//go:build dbtest

package statement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

type stack struct {
	q    *gen.Queries
	tx   pgx.Tx
	svc  *Service
	user gen.User
	// alpha is the one datasource, enabled, whose answers a test scripts.
	alpha *scripted.Identity
	// alphaName is alpha's name, unique to the test.
	alphaName string
}

// newStack returns a statement service with one scripted datasource, in a
// rolled-back transaction.
func newStack(t *testing.T) stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	var err error
	q := gen.New(tx)
	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: fmt.Sprintf("%s@example.com", uuid.NewString()), Role: gen.UserRoleUser})
	require.NoError(t, err)
	name := "alpha-" + uuid.NewString()
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: 10})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	alpha := scripted.New()
	factories := map[string]market.Factory{
		name: market.Factory{New: func(market.Config) (market.Integration, error) { return alpha, nil }},
	}
	sources, err := market.New(ctx, q, factories, log)
	require.NoError(t, err)
	runs := runtest.Runner{Q: q}
	mics, err := mic.Load(ctx, q)
	require.NoError(t, err)
	families, err := currency.Load(ctx, q)
	require.NoError(t, err)
	resolver := resolve.New(db.New[resolve.Queries](tx), market.NewFetcher(db.New[market.Queries](tx), runs, log), sources, mics, families, clock, log)
	return stack{q: q, tx: tx, svc: New(db.New[Queries](tx), runs, resolver, families, clock), user: user, alpha: alpha, alphaName: name}
}

func (s stack) ingest(t *testing.T, msg *statementv1.Statement) gen.Run {
	t.Helper()
	ctx := context.Background()
	row, err := s.svc.Create(ctx, s.user.ID, msg)
	require.NoError(t, err)
	got, err := s.q.GetRun(ctx, gen.GetRunParams{ID: row.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	if got.State != gen.RunStateCompleted {
		t.Fatalf("statement run = %+v, want completed", got)
	}
	return got
}

func (s stack) transactions(t *testing.T) []string {
	t.Helper()
	ctx := context.Background()
	rows, err := s.q.ListTransactions(ctx, s.user.ID)
	require.NoError(t, err)
	// A transaction is denominated in its key's currency.
	currency := map[uuid.UUID]string{}
	listed := map[uuid.UUID]bool{}
	for _, r := range rows {
		if listed[r.StatementID] {
			continue
		}
		listed[r.StatementID] = true
		keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: r.StatementID, UserID: s.user.ID})
		require.NoError(t, err)
		for _, k := range keys {
			currency[k.ID] = "-"
			if k.Currency != nil {
				currency[k.ID] = *k.Currency
			}
		}
	}
	var out []string
	for _, r := range rows {
		out = append(out, fmt.Sprintf("%s %s %s %s", r.OrderDate.Format(time.DateOnly), r.Broker, r.Quantity, currency[r.StatedKeyID]))
	}
	return out
}

// TestIngest ingests one statement of the shape an IBKR export takes and
// reads back everything it leaves.
func TestIngest(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usd, eur := "USD", "EUR"
	isin := ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0000000001", "")
	currency := ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, "USD", "")
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, isin)
	transfer := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, nil, isin)
	cashInEur := &typev1.StatedKey{Identifiers: []*typev1.Identifier{currency}, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Currency: &eur}
	equityAsCash := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, currency)
	msg := &statementv1.Statement{
		Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{
			rowMsg(acme, "2026-03-05", "10"),
			rowMsg(cashKey("USD"), "2026-03-05", "-1000"),
			rowMsg(transfer, "2026-03-02", "3"),
			rowMsg(acme, "2026-04-20", "1"),
			rowMsg(cashInEur, "2026-03-06", "1"),
			rowMsg(equityAsCash, "2026-03-06", "1"),
		},
		Splits: []*statementv1.StatedSplit{{Key: securityKey("SPLIT CO", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), EffectiveDate: "2026-03-10", Quantity: "9", Ratio: &statementv1.SplitRatio{From: "1", To: "10"}}},
	}
	parent := s.ingest(t, msg)

	st, err := s.q.GetStatement(ctx, gen.GetStatementParams{ID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if st.Statement.Broker != gen.BrokerIbkr || !st.Statement.OrderFrom.Equal(from) || !st.Statement.OrderBefore.Equal(until) || st.Statement.RowCount != 6 || st.Rejected != 3 {
		t.Errorf("GetStatement = %+v, want ibkr over March with 6 rows and 3 rejected", st)
	}
	keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(keys) != 6 {
		t.Errorf("ListStatedKeys = %d keys, want 6: the trade, cash, the transfer, cash in EUR, the equity stating a currency and the split", len(keys))
	}
	items, err := s.q.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	var gotItems []string
	for _, it := range items {
		gotItems = append(gotItems, fmt.Sprintf("%d %s", it.Ordinal, it.Reason))
	}
	wantItems := []string{
		"3 order date outside the claimed period",
		"4 no listing of USD in EUR",
		"5 asset class equity contradicts the instrument's cash",
	}
	if diff := cmp.Diff(wantItems, gotItems); diff != "" {
		t.Errorf("items mismatch (-want +got):\n%s", diff)
	}
	want := []string{"2026-03-02 ibkr 3 -", "2026-03-05 ibkr 10 USD", "2026-03-05 ibkr -1000 USD"}
	if diff := cmp.Diff(want, s.transactions(t)); diff != "" {
		t.Errorf("transactions mismatch (-want +got):\n%s", diff)
	}

	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "USD"})
	require.NoError(t, err)
	line, err := s.q.GetListing(ctx, gen.GetListingParams{InstrumentID: found.Instrument.ID, Currency: "USD"})
	require.NoError(t, err)
	var associated []gen.StatedKey
	for _, k := range keys {
		if k.InstrumentID != nil {
			associated = append(associated, k)
		}
	}
	if len(associated) != 1 {
		t.Fatalf("%d keys carry an association, want only the cash key", len(associated))
	}
	got := associated[0]
	if *got.InstrumentID != found.Instrument.ID || got.ListingID == nil || *got.ListingID != line.ID {
		t.Errorf("the cash key names instrument %s listing %v, want %s and %s", got.InstrumentID, got.ListingID, found.Instrument.ID, line.ID)
	}
	if got.ViaID == nil || *got.ViaID != found.Identifier.ID {
		t.Errorf("the cash key associates through %v, want %s", got.ViaID, found.Identifier.ID)
	}

	var instruments, families int
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT count(*) FROM instruments").Scan(&instruments))
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT count(*) FROM currencies WHERE code = family").Scan(&families))
	if instruments != families {
		t.Errorf("%d instruments, want the %d the migration seeds", instruments, families)
	}

	children, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &parent.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	if len(children) != 1 || children[0].Kind != gen.RunKindResolution || children[0].State != gen.RunStateCompleted {
		t.Fatalf("ListChildRuns = %+v, want one completed resolution", children)
	}
	resolved, err := s.q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: children[0].ID, UserID: s.user.ID})
	require.NoError(t, err)
	outcomes := map[gen.ResolutionOutcome]int{}
	for _, r := range resolved {
		outcomes[r.Outcome]++
	}
	wantOutcomes := map[gen.ResolutionOutcome]int{gen.ResolutionOutcomeMatched: 1, gen.ResolutionOutcomeRejected: 2, gen.ResolutionOutcomeUnrecognised: 3}
	if diff := cmp.Diff(wantOutcomes, outcomes); diff != "" {
		t.Errorf("resolution outcomes mismatch (-want +got):\n%s", diff)
	}
}

// TestFamily checks that when a cash key states pence, it resolves to the
// pound listing, since a listing is keyed by the currency family, and keeps
// the code it stated.
func TestFamily(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	parent := s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(cashKey("GBX"), "2026-03-05", "250")}})
	keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	gbp, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBP"})
	require.NoError(t, err)
	line, err := s.q.GetListing(ctx, gen.GetListingParams{InstrumentID: gbp.Instrument.ID, Currency: "GBP"})
	require.NoError(t, err)
	k := keys[0]
	if k.InstrumentID == nil || *k.InstrumentID != gbp.Instrument.ID || k.ListingID == nil || *k.ListingID != line.ID {
		t.Errorf("the GBX key names instrument %v listing %v, want GBP's %s and %s", k.InstrumentID, k.ListingID, gbp.Instrument.ID, line.ID)
	}
	if k.Currency == nil || *k.Currency != "GBX" {
		t.Errorf("the key states currency %v, want GBX as stated", k.Currency)
	}
}

// TestReplace checks that a statement replaces the period it claims and
// nothing else, and that a claim with no rows deletes.
func TestReplace(t *testing.T) {
	s := newStack(t)
	usd := "USD"
	// Each broker states the line in its own domain.
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_SECURITY, &usd)
	schwab := &typev1.StatedKey{AssetClass: acme.AssetClass, Currency: acme.Currency, Identifiers: []*typev1.Identifier{
		{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "schwab", Value: "ACME CORP"},
	}}
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(schwab, "2026-03-05", "1"), rowMsg(schwab, "2026-03-20", "2")}})
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(acme, "2026-03-20", "7")}})
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-15", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(schwab, "2026-03-20", "3")}})
	want := []string{"2026-03-05 schwab 1 USD", "2026-03-20 ibkr 7 USD", "2026-03-20 schwab 3 USD"}
	if diff := cmp.Diff(want, s.transactions(t)); diff != "" {
		t.Errorf("after the overlapping statement (-want +got):\n%s", diff)
	}
	s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-15", OrderBefore: "2026-04-01"})
	want = []string{"2026-03-05 schwab 1 USD", "2026-03-20 ibkr 7 USD"}
	if diff := cmp.Diff(want, s.transactions(t)); diff != "" {
		t.Errorf("after the empty statement (-want +got):\n%s", diff)
	}
}

// TestConfirmCandidate checks a confirmation across statements: the
// confirmed key takes the user's pick, its group mate stays unresolved in a
// group of its own, and a later statement stating the confirmed key links to
// it and resolves nothing for it.
func TestConfirmCandidate(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usd := "USD"
	intc := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "INTC"}
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US4581401001"}
	figi := types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S5SM32"}
	xnas := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "INTC"}
	s.alpha.Responses[intc] = market.IdentityResult{Filtered: []types.Identifier{intc}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, isin, xnas}},
	}}
	key := &typev1.StatedKey{
		Identifiers: []*typev1.Identifier{
			ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "INTC", ""),
			ident(typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, "INTEL CORP", "schwab"),
		},
		AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK, Currency: &usd,
	}
	// bare states the same line with no currency, so it is a key of its own
	// that the description joins to key's group.
	bare := &typev1.StatedKey{Identifiers: key.Identifiers, AssetClass: key.AssetClass}
	month := func(k *typev1.StatedKey, from, before, order, quantity string) gen.Run {
		return s.ingest(t, &statementv1.Statement{
			Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: from, OrderBefore: before,
			Rows: []*statementv1.Row{rowMsg(k, order, quantity)},
		})
	}
	keyOf := func(statement gen.Run) gen.StatedKey {
		t.Helper()
		keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement.ID, UserID: s.user.ID})
		require.NoError(t, err)
		if len(keys) != 1 {
			t.Fatalf("statement %s has %d keys, want 1", statement.ID, len(keys))
		}
		return keys[0]
	}
	// syncRuns returns the user's synchronous resolution runs, oldest first.
	syncRuns := func() []uuid.UUID {
		t.Helper()
		rows, err := s.tx.Query(ctx, "SELECT id FROM runs WHERE user_id = $1 AND kind = 'resolution' AND trigger = 'user' ORDER BY id", s.user.ID)
		require.NoError(t, err)
		defer rows.Close()
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			require.NoError(t, rows.Scan(&id))
			ids = append(ids, id)
		}
		return ids
	}

	first := keyOf(month(key, "2026-03-01", "2026-04-01", "2026-03-05", "10"))
	second := keyOf(month(bare, "2026-04-01", "2026-05-01", "2026-04-05", "5"))
	if first.ID == second.ID {
		t.Fatalf("the key with a currency and the key without are one row %s, want two", first.ID)
	}
	groups, err := s.q.ListGroupHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	if len(groups) != 1 || groups[0].Quantity.String() != "15" {
		t.Fatalf("group holdings = %+v, want one of 15", groups)
	}

	offer, err := s.svc.Candidates(ctx, s.user.ID, first.ID)
	require.NoError(t, err)
	if len(offer.Candidates) != 1 || offer.Candidates[0].Datasource != s.alphaName || offer.Candidates[0].Strongest != figi || len(offer.Reasons) != 0 {
		t.Fatalf("Candidates() = %+v, want alpha's one group named by its FIGI", offer)
	}
	runs := syncRuns()
	if len(runs) != 1 {
		t.Fatalf("%d synchronous runs after listing, want 1", len(runs))
	}
	listing, err := s.q.GetRun(ctx, gen.GetRunParams{ID: runs[0], UserID: &s.user.ID})
	require.NoError(t, err)
	children, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &listing.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	if listing.State != gen.RunStateCompleted || len(children) != 1 || children[0].Kind != gen.RunKindFetch || children[0].State != gen.RunStateCompleted {
		t.Fatalf("listing run = %+v with children %+v, want completed with one completed fetch", listing, children)
	}
	items, err := s.q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: children[0].ID})
	require.NoError(t, err)
	if len(items) != 1 || *items[0].FetchKey.StatedKeyID != first.ID || items[0].FetchKey.Outcome != gen.FetchOutcomeServed {
		t.Errorf("fetch items = %+v, want the key served", items)
	}

	rk, err := s.svc.Confirm(ctx, s.user.ID, first.ID, resolve.Pick{Datasource: s.alphaName, Identifier: isin})
	require.NoError(t, err)
	if rk.Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("Confirm() = %+v, want matched", rk)
	}
	got, err := s.q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: first.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if got.InstrumentID == nil || got.Arbiter == nil || *got.Arbiter != gen.ArbiterUser || got.GroupID != nil {
		t.Errorf("the confirmed key = %+v, want associated by the user in no group", got)
	}
	mate, err := s.q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: second.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if mate.InstrumentID != nil || mate.Arbiter != nil {
		t.Errorf("the mate = %+v, want it unresolved: a choice associates the one key", mate)
	}
	groups, err = s.q.ListGroupHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	instruments, err := s.q.ListInstrumentHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	if len(groups) != 1 || groups[0].Quantity.String() != "5" || len(instruments) != 1 || instruments[0].Quantity.String() != "10" {
		t.Errorf("holdings = groups %+v, instruments %+v, want the mate's group of 5 and one instrument holding of 10", groups, instruments)
	}
	if _, err := s.svc.Confirm(ctx, s.user.ID, first.ID, resolve.Pick{Datasource: s.alphaName, Identifier: isin}); !errors.Is(err, ErrAssociated) {
		t.Errorf("a second Confirm() error = %v, want ErrAssociated", err)
	}

	// A third statement restates the confirmed key. It links to the row,
	// resolves nothing for it, and its regroup leaves the mate in a group of
	// its own.
	third := month(key, "2026-02-01", "2026-03-01", "2026-02-05", "1")
	if k := keyOf(third); k.ID != first.ID || k.InstrumentID == nil || *k.InstrumentID != instruments[0].InstrumentID || k.Arbiter == nil || *k.Arbiter != gen.ArbiterUser {
		t.Errorf("the third statement's key = %+v, want the confirmed key %s", k, first.ID)
	}
	resolution, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &third.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	fetches, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &resolution[0].ID, UserID: &s.user.ID})
	require.NoError(t, err)
	if len(fetches) != 0 {
		t.Errorf("the third statement's resolution started %d fetches, want none: a key the user associated is not resolved again", len(fetches))
	}
	resolved, err := s.q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: resolution[0].ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(resolved) != 0 {
		t.Errorf("the third statement's resolution recorded %+v, want no outcome", resolved)
	}
	instruments, err = s.q.ListInstrumentHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	if len(instruments) != 1 || instruments[0].Quantity.String() != "11" {
		t.Errorf("instrument holdings = %+v, want one of 11", instruments)
	}
	mate, err = s.q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: second.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if mate.GroupID == nil || *mate.GroupID != second.ID {
		t.Errorf("the mate after the regroup = %+v, want a group of its own", mate)
	}
}

// TestSweep checks that a write sweeps a key of the user that lacks both a
// transaction and a statement link, after the regroup, so the members of a
// swept root survive it. Nothing deletes a statement today, so the test
// deletes the first statement's row by SQL, which stands in for that
// deletion.
func TestSweep(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usd := "USD"
	// Both keys state the one description, which joins them, and differ in
	// their currency. The key with the currency is interned first and is the
	// group's root.
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_SECURITY, &usd)
	bare := &typev1.StatedKey{Identifiers: acme.Identifiers, AssetClass: acme.AssetClass}
	first := s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(acme, "2026-03-05", "1"), rowMsg(bare, "2026-03-06", "2")}})
	keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: first.ID, UserID: s.user.ID})
	require.NoError(t, err)
	require.Len(t, keys, 2)
	root, member := keys[0], keys[1]
	if member.GroupID == nil || *member.GroupID != root.ID {
		t.Fatalf("member = %+v, want it in the root's group %s", member, root.ID)
	}

	_, err = s.tx.Exec(ctx, "DELETE FROM statements WHERE id = $1", first.ID)
	require.NoError(t, err)
	second := s.ingest(t, &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-04-01", OrderBefore: "2026-05-01",
		Rows: []*statementv1.Row{rowMsg(bare, "2026-04-05", "3")}})
	if _, err := s.q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: root.ID, UserID: s.user.ID}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("the root after the write: err = %v, want ErrNotFound", err)
	}
	kept, err := s.q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: member.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if kept.GroupID == nil || *kept.GroupID != member.ID {
		t.Errorf("the member after the write = %+v, want it kept in a group of its own", kept)
	}
	keys, err = s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: second.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(keys) != 1 || keys[0].ID != member.ID {
		t.Errorf("the second statement's keys = %+v, want the member alone", keys)
	}
	if diff := cmp.Diff([]string{"2026-04-05 ibkr 3 -"}, s.transactions(t)); diff != "" {
		t.Errorf("transactions mismatch (-want +got):\n%s", diff)
	}
}

// TestRestate checks that a statement restating a matched key reuses the
// earlier match, without a fetch or an outcome of its own, and lists the key
// with the outcome the earlier run recorded.
func TestRestate(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usd := "USD"
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000002"}
	figi := types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S5SM33"}
	xnas := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "ACME"}
	s.alpha.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, isin, xnas}},
	}}
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_STOCK, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0000000002", ""))
	month := func(from, before, order, quantity string) gen.Run {
		return s.ingest(t, &statementv1.Statement{
			Broker: typev1.Broker_BROKER_IBKR, OrderFrom: from, OrderBefore: before,
			Rows: []*statementv1.Row{rowMsg(acme, order, quantity)},
		})
	}
	keyOf := func(statement gen.Run) gen.StatedKey {
		t.Helper()
		keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement.ID, UserID: s.user.ID})
		require.NoError(t, err)
		if len(keys) != 1 {
			t.Fatalf("statement %s has %d keys, want 1", statement.ID, len(keys))
		}
		return keys[0]
	}
	resolutionOf := func(statement gen.Run) gen.Run {
		t.Helper()
		children, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &statement.ID, UserID: &s.user.ID})
		require.NoError(t, err)
		if len(children) != 1 || children[0].Kind != gen.RunKindResolution {
			t.Fatalf("statement %s has children %+v, want one resolution", statement.ID, children)
		}
		return children[0]
	}

	first := month("2026-03-01", "2026-04-01", "2026-03-05", "10")
	second := month("2026-04-01", "2026-05-01", "2026-04-05", "5")
	a, b := keyOf(first), keyOf(second)
	if a.ID != b.ID || b.InstrumentID == nil || b.Arbiter == nil || *b.Arbiter != gen.ArbiterDatasource {
		t.Errorf("the second statement's key = %+v, want the first's %s with the datasource's association", b, a.ID)
	}
	resolution := resolutionOf(second)
	fetches, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &resolution.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	if len(fetches) != 0 {
		t.Errorf("the second statement's resolution started %d fetches, want none", len(fetches))
	}
	resolved, err := s.q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: resolution.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(resolved) != 0 {
		t.Errorf("the second statement's resolution recorded %+v, want no outcome", resolved)
	}
	latest, err := s.q.ListLatestResolutions(ctx, gen.ListLatestResolutionsParams{Ids: []uuid.UUID{a.ID}, UserID: s.user.ID})
	require.NoError(t, err)
	if len(latest) != 1 || latest[0].RunID != resolutionOf(first).ID || latest[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Errorf("latest resolution = %+v, want the first statement's, matched", latest)
	}
	instruments, err := s.q.ListInstrumentHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	if len(instruments) != 1 || instruments[0].Quantity.String() != "15" {
		t.Errorf("instrument holdings = %+v, want one of 15", instruments)
	}
}
