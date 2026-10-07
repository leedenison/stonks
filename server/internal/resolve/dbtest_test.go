//go:build dbtest

package resolve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// TestTree holds the class tree equal to the asset_class_tree table.
func TestTree(t *testing.T) {
	rows, err := gen.New(pool).ListAssetClassTree(context.Background())
	require.NoError(t, err)
	got := map[gen.AssetClass]gen.AssetClass{}
	for _, r := range rows {
		var parent gen.AssetClass
		if r.Parent != nil {
			parent = *r.Parent
		}
		got[r.Class] = parent
	}
	if diff := cmp.Diff(parents, got); diff != "" {
		t.Errorf("asset class tree mismatch (-code +table):\n%s", diff)
	}
}

// stack is a resolver over a rolled-back transaction. It scripts two
// datasources: alpha, which is enabled, and beta, which a test enables with
// enableBeta.
type stack struct {
	q          *gen.Queries
	tx         pgx.Tx
	user       gen.User
	statement  gen.Run
	statements []gen.Run
	resolution gen.Run
	script     *scripted.Identity
	beta       *scripted.Identity
	sources    *market.Registry
	resolver   *Resolver
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	var err error
	q := gen.New(tx)
	s := &stack{q: q, tx: tx, script: scripted.New(), beta: scripted.New()}
	s.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.statement = s.newStatement(t, gen.BrokerIbkr)
	s.resolution, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &s.statement.ID})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: "alpha", Enabled: true, Precedence: 10})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	factories := map[string]market.Factory{
		"alpha": func(market.Config) (market.Integration, error) { return s.script, nil },
		"beta":  func(market.Config) (market.Integration, error) { return s.beta, nil },
	}
	s.sources, err = market.New(ctx, q, factories, log)
	require.NoError(t, err)
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log)
	s.resolver = New(db.New[Queries](tx), fetcher, s.sources, log)
	return s
}

// newStatement records a statement run of broker and returns the run.
func (s *stack) newStatement(t *testing.T, broker gen.Broker) gen.Run {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: s.user.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = s.q.CreateStatement(ctx, gen.CreateStatementParams{ID: run.ID, UserID: s.user.ID, Broker: broker, OrderFrom: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), OrderBefore: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	s.statements = append(s.statements, run)
	return run
}

// enableBeta enables beta with a lower precedence than alpha.
func (s *stack) enableBeta(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := s.q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: "beta", Enabled: true, Precedence: 20})
	require.NoError(t, err)
	require.NoError(t, s.sources.Reload(ctx))
}

// state records a stated key under the stack's statement.
func (s *stack) state(t *testing.T, class gen.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	t.Helper()
	return s.stateUnder(t, s.statement, class, currency, ids...)
}

// stateUnder records a stated key under statement.
func (s *stack) stateUnder(t *testing.T, statement gen.Run, class gen.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	t.Helper()
	arg := gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: statement.ID, UserID: s.user.ID, Identifiers: ids}
	if class != "" {
		arg.AssetClass = &class
	}
	if currency != "" {
		arg.Currency = &currency
	}
	row, err := s.q.CreateStatedKey(context.Background(), arg)
	require.NoError(t, err)
	return row
}

func (s *stack) resolve(t *testing.T, keys ...gen.StatedKey) []gen.ResolutionKey {
	t.Helper()
	out, err := s.resolver.Resolve(context.Background(), s.resolution, keys)
	require.NoError(t, err)
	return out
}

func (s *stack) key(t *testing.T, id uuid.UUID) gen.StatedKey {
	t.Helper()
	for _, st := range s.statements {
		keys, err := s.q.ListStatedKeys(context.Background(), gen.ListStatedKeysParams{StatementID: st.ID, UserID: s.user.ID})
		require.NoError(t, err)
		for _, k := range keys {
			if k.ID == id {
				return k
			}
		}
	}
	t.Fatalf("no stated key %s", id)
	return gen.StatedKey{}
}

// fetches returns the fetch runs of the stack's resolution.
func (s *stack) fetches(t *testing.T) []gen.Run {
	t.Helper()
	children, err := s.q.ListChildRuns(context.Background(), gen.ListChildRunsParams{ParentID: &s.resolution.ID, UserID: s.user.ID})
	require.NoError(t, err)
	return children
}

// fetchKey returns the fetch key datasource wrote for key.
func (s *stack) fetchKey(t *testing.T, key uuid.UUID, datasource string) gen.FetchKey {
	t.Helper()
	ctx := context.Background()
	for _, child := range s.fetches(t) {
		fetch, err := s.q.GetFetch(ctx, gen.GetFetchParams{ID: child.ID, UserID: s.user.ID})
		require.NoError(t, err)
		if fetch.Datasource != datasource {
			continue
		}
		items, err := s.q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: child.ID})
		require.NoError(t, err)
		for _, it := range items {
			if it.StatedKey.ID == key {
				return it.FetchKey
			}
		}
	}
	t.Fatalf("no fetch key for %s from %s", key, datasource)
	return gen.FetchKey{}
}

// findings returns the kinds of the findings written against key.
func (s *stack) findings(t *testing.T, key uuid.UUID) []gen.FindingKind {
	t.Helper()
	rows, err := s.q.ListRunFindings(context.Background(), []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	var kinds []gen.FindingKind
	for _, f := range rows {
		if f.Finding.StatedKeyID != nil && *f.Finding.StatedKeyID == key {
			kinds = append(kinds, f.Finding.Kind)
		}
	}
	return kinds
}

// exists reports whether the instrument id exists.
func (s *stack) exists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(), "SELECT count(*) FROM instruments WHERE id = $1", id).Scan(&n))
	return n == 1
}

// written writes a resolution key as its outcome and reason.
func written(rk gen.ResolutionKey) string {
	out := string(rk.Outcome)
	if rk.Reason != nil {
		out += ": " + *rk.Reason
	}
	return out
}

// TestResolveWrites checks what one resolution writes: a created instrument,
// a second key attaching to it, a cash key on the seed and a dropped group.
func TestResolveWrites(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBX", Identifiers: []types.Identifier{figi, isin, comp, xlon}},
	}}
	trade := s.state(t, gen.AssetClassStock, "GBX", isin, xlon)
	transfer := s.state(t, gen.AssetClassEquity, "", isin)
	cash := s.state(t, gen.AssetClassCash, "GBX", id(types.IdentifierTypeCurrency, "", "GBX"))
	wrong := s.state(t, gen.AssetClassStock, "USD", cusip)
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi2, cusip, xetr}},
	}}

	got := s.resolve(t, trade, transfer, cash, wrong)
	outcomes := make([]string, len(got))
	for i, r := range got {
		outcomes[i] = string(r.Outcome)
		if r.Reason != nil {
			outcomes[i] += ": " + *r.Reason
		}
	}
	want := []string{"matched", "matched", "matched", "unrecognised: alpha: 1 candidate in 1 group, 1 group dropped"}
	if diff := cmp.Diff(want, outcomes); diff != "" {
		t.Errorf("outcomes mismatch (-want +got):\n%s", diff)
	}

	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeIsin, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument
	if inst.AssetClass != gen.AssetClassStock || inst.FetchKeyID == nil {
		t.Errorf("instrument = %+v, want a stock with provenance", inst)
	}
	listings, err := s.q.ListListings(ctx, inst.ID)
	require.NoError(t, err)
	if len(listings) != 1 || listings[0].Currency != "GBP" || listings[0].FetchKeyID == nil {
		t.Fatalf("listings = %+v, want one GBP listing with provenance", listings)
	}
	ids, err := s.q.ListIdentifiers(ctx, inst.ID)
	require.NoError(t, err)
	var identifiers []string
	for _, row := range ids {
		where := "instrument"
		if row.ListingID != nil {
			where = "listing"
		}
		identifiers = append(identifiers, where+" "+name(to.Identifier(row)))
		if row.FetchKeyID == nil || *row.FetchKeyID != *inst.FetchKeyID {
			t.Errorf("identifier %s from fetch key %v, want the instrument's %s", row.Value, row.FetchKeyID, *inst.FetchKeyID)
		}
	}
	wantIdentifiers := []string{"instrument isin GB00BH4HKS39", "instrument openfigi_share_class BBG001S5XDT5", "listing openfigi_composite BBG000C6K6G9", "listing mic_ticker XLON:VOD"}
	if diff := cmp.Diff(wantIdentifiers, identifiers); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	asserted, err := s.q.ListFetchIdentifiers(ctx, *inst.FetchKeyID)
	require.NoError(t, err)
	if len(asserted) != 4 {
		t.Errorf("%d fetch identifiers, want the 4 the response named", len(asserted))
	}

	tradeKey, transferKey := s.key(t, trade.ID), s.key(t, transfer.ID)
	via := map[uuid.UUID]string{}
	for _, row := range ids {
		via[row.ID] = name(to.Identifier(row))
	}
	if tradeKey.InstrumentID == nil || *tradeKey.InstrumentID != inst.ID || tradeKey.ListingID == nil || *tradeKey.ListingID != listings[0].ID || via[*tradeKey.ViaID] != "isin GB00BH4HKS39" || *tradeKey.Validity != gen.ValidityConfirmed {
		t.Errorf("trade = %+v, want the instrument's GBP listing via its ISIN, confirmed", tradeKey)
	}
	if transferKey.InstrumentID == nil || *transferKey.InstrumentID != inst.ID || transferKey.ListingID != nil || via[*transferKey.ViaID] != "isin GB00BH4HKS39" || *transferKey.Validity != gen.ValidityConfirmed {
		t.Errorf("transfer = %+v, want the instrument via its ISIN, confirmed, with no listing", transferKey)
	}

	gbx, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBX"})
	require.NoError(t, err)
	cashKey := s.key(t, cash.ID)
	if cashKey.InstrumentID == nil || *cashKey.InstrumentID != gbx.Instrument.ID || cashKey.ViaID == nil || *cashKey.ViaID != gbx.Identifier.ID || cashKey.ListingID == nil {
		t.Errorf("cash = %+v, want the GBP seed via the GBX identifier", cashKey)
	}

	// One fetch served every key but cash, since every lookup precedes the
	// writes: the transfer's response attached to the instrument the trade
	// created, and the coverage row carries its fetch key, the later.
	children, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &s.resolution.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(children) != 1 || children[0].Kind != gen.RunKindFetch || children[0].State != gen.RunStateCompleted {
		t.Fatalf("ListChildRuns = %+v, want one completed fetch", children)
	}
	items, err := s.q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: children[0].ID})
	require.NoError(t, err)
	sent := map[uuid.UUID]gen.FetchKey{}
	for _, it := range items {
		sent[it.StatedKey.ID] = it.FetchKey
	}
	attached := func(id uuid.UUID) bool { return sent[id].InstrumentID != nil && *sent[id].InstrumentID == inst.ID }
	if len(sent) != 3 || !attached(trade.ID) || !attached(transfer.ID) || sent[wrong.ID].InstrumentID != nil {
		t.Errorf("fetch items = %+v, want the trade and the transfer attached to the instrument and the mis-stated key attached to nothing", sent)
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{inst.ID})
	require.NoError(t, err)
	if len(coverage) != 1 || coverage[0].Datasource != "alpha" || coverage[0].FetchKeyID != sent[transfer.ID].ID {
		t.Errorf("coverage = %+v, want alpha through the transfer's fetch key", coverage)
	}

	findings, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	if len(findings) != 1 || findings[0].Finding.Kind != gen.FindingKindDropped || *findings[0].Finding.Step != gen.DropStepStated || *findings[0].Finding.StatedKeyID != wrong.ID || *findings[0].Finding.Detail != "cusip 92857W308: stated USD has no listing among GBP (alpha)" {
		t.Errorf("findings = %+v, want one stated drop against the mis-stated key", findings)
	}
	if _, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCusip, Value: cusip.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("FindIdentifier(cusip) error = %v, want not found: a dropped group is not stored", err)
	}
}

// TestMergeRows checks that a response identifying two instruments folds
// the later into the earlier, with every row that pointed at the loser
// relinked or moved.
func TestMergeRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{cusip, xnas}},
	}}
	first := s.state(t, gen.AssetClassStock, "GBP", isin)
	second := s.state(t, gen.AssetClassStock, "USD", cusip)
	s.resolve(t, first, second)
	a, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	b, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: cusip.Type, Value: cusip.Value})
	require.NoError(t, err)
	if a.Instrument.ID == b.Instrument.ID {
		t.Fatal("the two keys resolved to one instrument before the merge")
	}

	// The third key's response names both instruments, which is what brings
	// them together.
	sedol := id(types.IdentifierTypeSedol, "", "BH4HKS3")
	s.script.Responses[sedol] = market.IdentityResult{Filtered: []types.Identifier{sedol}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, cusip, sedol, xnas}},
	}}
	third := s.state(t, gen.AssetClassStock, "USD", sedol)
	got := s.resolve(t, third)
	if got[0].Outcome != gen.ResolutionOutcomeMatched {
		reason := ""
		if got[0].Reason != nil {
			reason = *got[0].Reason
		}
		findings, _ := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
		var details []string
		for _, f := range findings {
			details = append(details, string(f.Finding.Kind)+": "+*f.Finding.Detail)
		}
		t.Fatalf("Resolve = %s (%s), want matched; findings %v", got[0].Outcome, reason, details)
	}
	_, err = s.tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	require.NoError(t, err, "the deferred constraints hold after the merge")

	survivor := a.Instrument.ID
	var instruments int
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT count(*) FROM instruments WHERE id = $1", b.Instrument.ID).Scan(&instruments))
	if instruments != 0 {
		t.Errorf("the later instrument %s still exists", b.Instrument.ID)
	}
	listings, err := s.q.ListListings(ctx, survivor)
	require.NoError(t, err)
	families := map[string]uuid.UUID{}
	for _, l := range listings {
		families[l.Currency] = l.ID
	}
	if len(families) != 2 || families["GBP"] == uuid.Nil || families["USD"] == uuid.Nil {
		t.Fatalf("survivor listings = %+v, want GBP and USD", listings)
	}
	ids, err := s.q.ListIdentifiers(ctx, survivor)
	require.NoError(t, err)
	where := map[string]string{}
	for _, row := range ids {
		listing := "instrument"
		if row.ListingID != nil {
			for fam, id := range families {
				if id == *row.ListingID {
					listing = fam
				}
			}
		}
		where[name(to.Identifier(row))] = listing
	}
	wantWhere := map[string]string{
		"isin GB00BH4HKS39": "instrument", "openfigi_share_class BBG001S5XDT5": "instrument",
		"cusip 92857W308":     "instrument",
		"mic_ticker XLON:VOD": "GBP", "mic_ticker XNAS:VOD": "USD", "sedol BH4HKS3": "USD",
	}
	if diff := cmp.Diff(wantWhere, where); diff != "" {
		t.Errorf("survivor identifiers mismatch (-want +got):\n%s", diff)
	}
	for _, id := range []uuid.UUID{second.ID, third.ID} {
		k := s.key(t, id)
		if k.InstrumentID == nil || *k.InstrumentID != survivor || k.ListingID == nil || *k.ListingID != families["USD"] {
			t.Errorf("key %s = instrument %v listing %v, want the survivor's USD listing", id, k.InstrumentID, k.ListingID)
		}
	}
	var relinked int
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT count(*) FROM fetch_keys WHERE stated_key_id = $1 AND instrument_id = $2", second.ID, survivor).Scan(&relinked))
	if relinked != 1 {
		t.Errorf("%d fetch keys of the second key name the survivor, want 1", relinked)
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{survivor, b.Instrument.ID})
	require.NoError(t, err)
	if len(coverage) != 1 || coverage[0].InstrumentID != survivor {
		t.Errorf("coverage = %+v, want one row on the survivor", coverage)
	}
	findings, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	if len(findings) != 1 || findings[0].Finding.Kind != gen.FindingKindMerged || *findings[0].Finding.StatedKeyID != third.ID || findings[0].Finding.FetchKeyID == nil {
		t.Errorf("findings = %+v, want one merged finding on the third key with its fetch key", findings)
	}
}

// TestDescribedRows checks resolution through a system owned broker
// description against a real database.
func TestDescribedRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	descr := id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORP")
	// alpha creates the instrument the description names, and a second
	// instrument that the description must not match.
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi2, cusip, xnas}},
	}}
	s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin), s.state(t, gen.AssetClassStock, "USD", cusip))
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument
	listings, err := s.q.ListListings(ctx, inst.ID)
	require.NoError(t, err)
	require.Len(t, listings, 1)
	row, err := s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: inst.ID, Type: descr.Type, Domain: descr.Domain, Value: descr.Value})
	require.NoError(t, err)
	s.enableBeta(t)

	// via checks that key matched inst through row, on the GBP listing,
	// provisionally.
	via := func(t *testing.T, key gen.StatedKey) {
		t.Helper()
		if key.InstrumentID == nil || *key.InstrumentID != inst.ID || key.ViaID == nil || *key.ViaID != row.ID || key.ListingID == nil || *key.ListingID != listings[0].ID || *key.Validity != gen.ValidityProvisional {
			t.Errorf("key = %+v, want the instrument's GBP listing via the description, provisional", key)
		}
	}

	t.Run("a description alone matches, and beta is asked and serves nothing", func(t *testing.T) {
		before := len(s.fetches(t))
		k := s.state(t, gen.AssetClassStock, "GBP", descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
		if len(s.fetches(t)) != before+1 {
			t.Errorf("%d fetches, want one more: alpha covers the instrument and beta does not", len(s.fetches(t))-before)
		}
		if fk := s.fetchKey(t, k.ID, "beta"); fk.Outcome != gen.FetchOutcomeNotServed {
			t.Errorf("beta's fetch key = %+v, want not served", fk)
		}
		ids, err := s.q.ListIdentifiers(ctx, inst.ID)
		require.NoError(t, err)
		var descriptions []uuid.UUID
		for _, i := range ids {
			if i.Type == types.IdentifierTypeBrokerDescription {
				descriptions = append(descriptions, i.ID)
			}
		}
		if len(descriptions) != 1 || descriptions[0] != row.ID {
			t.Errorf("description rows = %v, want the one inserted", descriptions)
		}
	})

	t.Run("a datasource answering an unknown ISIN with another instrument is a contradiction", func(t *testing.T) {
		other := id(types.IdentifierTypeIsin, "", "GB00B03MLX29")
		s.beta.Responses[other] = market.IdentityResult{Filtered: []types.Identifier{other}, Candidates: []market.Candidate{
			{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT7"), other, id(types.IdentifierTypeMicTicker, "XLON", "SHEL")}},
		}}
		k := s.state(t, gen.AssetClassStock, "GBP", other, descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, k.ID)); diff != "" {
			t.Errorf("findings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a description and an identifier naming different instruments", func(t *testing.T) {
		k := s.state(t, gen.AssetClassStock, "USD", cusip, descr)
		got := s.resolve(t, k)
		want := "unrecognised: cusip 92857W308 and broker_description ibkr:ACME CORP name different instruments"
		if written(got[0]) != want {
			t.Errorf("outcome = %s, want %s", written(got[0]), want)
		}
		if key := s.key(t, k.ID); key.InstrumentID != nil {
			t.Errorf("key = %+v, want no association", key)
		}
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, k.ID)); diff != "" {
			t.Errorf("findings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a bare ticker beside the description is sent and the description associates", func(t *testing.T) {
		// beta served the instrument above and so covers it. Its coverage is
		// cleared so that it is asked again, and alpha's kept, since the
		// stack's one connection cannot carry two fetches at once.
		_, err := s.tx.Exec(ctx, "DELETE FROM identity_coverage WHERE instrument_id = $1 AND datasource = 'beta'", inst.ID)
		require.NoError(t, err)
		s.beta.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Candidates: []market.Candidate{
			{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, xlon}},
		}}
		k := s.state(t, gen.AssetClassStock, "GBP", ticker, descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
		if fk := s.fetchKey(t, k.ID, "beta"); fk.Outcome != gen.FetchOutcomeServed || fk.SentType == nil || *fk.SentType != types.IdentifierTypeMicTicker {
			t.Errorf("beta's fetch key = %+v, want served under the ticker", fk)
		}
	})

	t.Run("another broker's description of the same text names nothing", func(t *testing.T) {
		schwab := s.newStatement(t, gen.BrokerSchwab)
		k := s.stateUnder(t, schwab, gen.AssetClassStock, "USD", id(types.IdentifierTypeBrokerDescription, "schwab", "ACME CORP"))
		got := s.resolve(t, k)
		want := "unrecognised: no instrument is identified by broker_description schwab:ACME CORP"
		if written(got[0]) != want {
			t.Errorf("outcome = %s, want %s", written(got[0]), want)
		}
	})

	t.Run("two descriptions, one known", func(t *testing.T) {
		k := s.state(t, gen.AssetClassStock, "GBP", descr, id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORPORATION"))
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
	})

	// beta serves the ISIN, so it covers the instrument. The subtests above
	// need it uncovered, so this runs last.
	t.Run("an identifier the instrument carries is stronger than the description", func(t *testing.T) {
		k := s.state(t, gen.AssetClassStock, "GBP", isin, descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		key := s.key(t, k.ID)
		if key.ViaID == nil || *key.ViaID != found.Identifier.ID || *key.Validity != gen.ValidityConfirmed {
			t.Errorf("key = %+v, want the instrument via its ISIN, confirmed", key)
		}
	})

	t.Run("reference data is asked of no datasource", func(t *testing.T) {
		ref, err := s.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: gen.AssetClassEquity})
		require.NoError(t, err)
		listing, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: ref.ID, Currency: "GBP"})
		require.NoError(t, err)
		fund := id(types.IdentifierTypeBrokerDescription, "fidelity_uk", "ZZ FUND ACC")
		seeded, err := s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: ref.ID, Type: fund.Type, Domain: fund.Domain, Value: fund.Value})
		require.NoError(t, err)
		fidelity := s.newStatement(t, gen.BrokerFidelityUk)
		before := len(s.fetches(t))
		k := s.stateUnder(t, fidelity, gen.AssetClassSecurity, "GBP", fund)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		key := s.key(t, k.ID)
		if key.InstrumentID == nil || *key.InstrumentID != ref.ID || *key.ViaID != seeded.ID || *key.ListingID != listing.ID || *key.Validity != gen.ValidityProvisional {
			t.Errorf("key = %+v, want the reference instrument via its description, provisional", key)
		}
		if len(s.fetches(t)) != before {
			t.Errorf("%d new fetches, want none: reference data is covered by every datasource", len(s.fetches(t))-before)
		}
	})
}

// TestCurrencyRows checks the cash keys the seed rejects.
func TestCurrencyRows(t *testing.T) {
	s := newStack(t)
	usd := id(types.IdentifierTypeCurrency, "", "USD")
	tests := []struct {
		name  string
		class gen.AssetClass
		code  string
		id    types.Identifier
		want  string
	}{
		{name: "no such currency", class: gen.AssetClassCash, code: "ZZZ", id: id(types.IdentifierTypeCurrency, "", "ZZZ"), want: "rejected: no currency ZZZ"},
		{name: "a class the seed contradicts", class: gen.AssetClassEquity, code: "USD", id: usd, want: "rejected: asset class equity contradicts the instrument's cash"},
		{name: "no listing in the stated family", class: gen.AssetClassCash, code: "EUR", id: usd, want: "rejected: no listing of USD in EUR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := s.state(t, tc.class, tc.code, tc.id)
			got := s.resolve(t, k)
			if written(got[0]) != tc.want {
				t.Errorf("outcome = %s, want %s", written(got[0]), tc.want)
			}
			if key := s.key(t, k.ID); key.InstrumentID != nil {
				t.Errorf("key = %+v, want no association", key)
			}
		})
	}
}

// TestLimitedDatasource checks that a datasource listing only some of an
// instrument's listings never contradicts a stated currency, and that one
// listing every listing does. Alpha covers the instrument first, so only
// beta is asked the second key.
func TestLimitedDatasource(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprintf("limited %t", limited), func(t *testing.T) {
			s := newStack(t)
			ctx := context.Background()
			s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
				{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
			}}
			s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin))
			s.enableBeta(t)
			s.beta.Limited = limited
			s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
				{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, xnas}},
			}}
			k := s.stateUnder(t, s.newStatement(t, gen.BrokerIbkr), gen.AssetClassStock, "GBP", isin)
			if got := s.resolve(t, k); written(got[0]) != "matched" {
				t.Fatalf("outcome = %s, want matched", written(got[0]))
			}
			var want []gen.FindingKind
			if !limited {
				want = []gen.FindingKind{gen.FindingKindDropped}
			}
			if diff := cmp.Diff(want, s.findings(t, k.ID)); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
			key := s.key(t, k.ID)
			listings, err := s.q.ListListings(ctx, *key.InstrumentID)
			require.NoError(t, err)
			if n := 1 + btoi(limited); len(listings) != n {
				t.Errorf("listings = %+v, want %d", listings, n)
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestCoveredRows checks that a key the database names is requested only
// from the datasources that have not covered its instrument, and that their
// response fills what the instrument lacks without replacing what it has.
func TestCoveredRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin))
	before, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	inst := before.Instrument
	s.enableBeta(t)
	s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, isin, xnas}},
	}}
	fetched := len(s.fetches(t))

	k := s.state(t, gen.AssetClassStock, "USD", isin)
	got := s.resolve(t, k)
	if written(got[0]) != "matched" {
		t.Fatalf("outcome = %s, want matched", written(got[0]))
	}
	if n := len(s.fetches(t)) - fetched; n != 1 {
		t.Errorf("%d new fetches, want 1: alpha covers the instrument", n)
	}
	fk := s.fetchKey(t, k.ID, "beta")
	listings, err := s.q.ListListings(ctx, inst.ID)
	require.NoError(t, err)
	byFamily := map[string]gen.Listing{}
	for _, l := range listings {
		byFamily[l.Currency] = l
	}
	usd, ok := byFamily["USD"]
	if len(listings) != 2 || !ok || usd.FetchKeyID == nil || *usd.FetchKeyID != fk.ID {
		t.Fatalf("listings = %+v, want GBP and a USD listing from beta's fetch key", listings)
	}
	ids, err := s.q.ListIdentifiers(ctx, inst.ID)
	require.NoError(t, err)
	var names []string
	for _, row := range ids {
		names = append(names, name(to.Identifier(row)))
		if row.Type == isin.Type && row.ID != before.Identifier.ID {
			t.Errorf("the ISIN row is %s, want the one alpha wrote, %s", row.ID, before.Identifier.ID)
		}
	}
	want := []string{"isin GB00BH4HKS39", "openfigi_share_class BBG001S5XDT5", "mic_ticker XLON:VOD", "mic_ticker XNAS:VOD"}
	if diff := cmp.Diff(want, names); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	key := s.key(t, k.ID)
	if key.ListingID == nil || *key.ListingID != usd.ID || *key.ViaID != before.Identifier.ID || *key.Validity != gen.ValidityConfirmed {
		t.Errorf("key = %+v, want the USD listing via the ISIN, confirmed", key)
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{inst.ID})
	require.NoError(t, err)
	var sources []string
	for _, c := range coverage {
		sources = append(sources, c.Datasource)
	}
	slices.Sort(sources)
	if diff := cmp.Diff([]string{"alpha", "beta"}, sources); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
}

// TestDroppedUncovered checks that when the resolver drops a datasource's
// answer, the datasource does not cover the instrument and is asked again,
// and that an answer with no candidate covers it.
func TestDroppedUncovered(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin))
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument.ID
	covered := func() []string {
		rows, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{inst})
		require.NoError(t, err)
		var out []string
		for _, c := range rows {
			out = append(out, c.Datasource)
		}
		return out
	}
	s.enableBeta(t)
	// Each key needs a new statement, because a statement states a key once.
	state := func() gen.StatedKey {
		return s.stateUnder(t, s.newStatement(t, gen.BrokerIbkr), gen.AssetClassStock, "GBP", isin)
	}

	// beta's share class contradicts the instrument's, so its group is
	// dropped and it is asked again for each key.
	s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi2, isin, xlon}},
	}}
	for n := range 2 {
		fetched := len(s.fetches(t))
		k := state()
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		if d := len(s.fetches(t)) - fetched; d != 1 {
			t.Errorf("key %d: %d new fetches, want 1: a dropped answer does not cover the instrument", n, d)
		}
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, k.ID)); diff != "" {
			t.Errorf("key %d: findings mismatch (-want +got):\n%s", n, diff)
		}
		if diff := cmp.Diff([]string{"alpha"}, covered()); diff != "" {
			t.Errorf("key %d: coverage mismatch (-want +got):\n%s", n, diff)
		}
	}

	// An answer with no candidate covers the instrument.
	s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}}
	fetched := len(s.fetches(t))
	s.resolve(t, state())
	if d := len(s.fetches(t)) - fetched; d != 1 {
		t.Errorf("%d new fetches, want 1: beta is asked once more", d)
	}
	if diff := cmp.Diff([]string{"alpha", "beta"}, covered()); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
	fetched = len(s.fetches(t))
	s.resolve(t, state())
	if d := len(s.fetches(t)) - fetched; d != 0 {
		t.Errorf("%d new fetches, want none: an empty answer covers the instrument", d)
	}
}

// TestLookupRows checks the keys the lookup decides against what the
// database names: two stated identifiers naming different instruments, and
// a stated class disjoint from the instrument's.
func TestLookupRows(t *testing.T) {
	s := newStack(t)
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi2, cusip, xnas}},
	}}
	s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin), s.state(t, gen.AssetClassStock, "USD", cusip))
	fetched := len(s.fetches(t))

	contradicted := s.state(t, gen.AssetClassStock, "USD", isin, cusip)
	disjoint := s.state(t, gen.AssetClassEtf, "GBP", isin)
	got := s.resolve(t, contradicted, disjoint)
	want := []string{
		"unrecognised: isin GB00BH4HKS39 and cusip 92857W308 name different instruments",
		"unrecognised: asset class etf contradicts the instrument's stock",
	}
	if diff := cmp.Diff(want, []string{written(got[0]), written(got[1])}); diff != "" {
		t.Errorf("outcomes mismatch (-want +got):\n%s", diff)
	}
	if n := len(s.fetches(t)) - fetched; n != 0 {
		t.Errorf("%d new fetches, want none: the lookup decided both keys", n)
	}
	if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, contradicted.ID)); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
}

// TestMergeAcrossCurrencies checks that two instruments carrying a ticker at
// one venue, on listings of different currency families, do not disagree,
// so a response naming both merges them.
func TestMergeAcrossCurrencies(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usdLine := id(types.IdentifierTypeMicTicker, "XLON", "VODUSD")
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{cusip, usdLine}},
	}}
	s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin), s.state(t, gen.AssetClassStock, "USD", cusip))
	a, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	b, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: cusip.Type, Value: cusip.Value})
	require.NoError(t, err)

	sedol := id(types.IdentifierTypeSedol, "", "BH4HKS3")
	s.script.Responses[sedol] = market.IdentityResult{Filtered: []types.Identifier{sedol}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, cusip, sedol}},
	}}
	k := s.state(t, gen.AssetClassStock, "USD", sedol)
	got := s.resolve(t, k)
	if written(got[0]) != "matched" {
		t.Fatalf("outcome = %s, want matched", written(got[0]))
	}
	if s.exists(t, b.Instrument.ID) {
		t.Errorf("the later instrument %s still exists", b.Instrument.ID)
	}
	ids, err := s.q.ListIdentifiers(ctx, a.Instrument.ID)
	require.NoError(t, err)
	var tickers []string
	for _, row := range ids {
		if row.Type == types.IdentifierTypeMicTicker {
			tickers = append(tickers, name(to.Identifier(row)))
		}
	}
	if diff := cmp.Diff([]string{"mic_ticker XLON:VOD", "mic_ticker XLON:VODUSD"}, tickers); diff != "" {
		t.Errorf("survivor tickers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindMerged}, s.findings(t, k.ID)); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
}

// TestMergeRefused checks that where two instruments a response names
// disagree on an identifier, both survive, the key associates with the
// instrument its stated identifier names, and a contradiction is recorded.
func TestMergeRefused(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	other := id(types.IdentifierTypeIsin, "", "GB00BH4HKS40")
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{cusip, other, xnas}},
	}}
	s.resolve(t, s.state(t, gen.AssetClassStock, "GBP", isin), s.state(t, gen.AssetClassStock, "USD", cusip))
	a, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	b, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: cusip.Type, Value: cusip.Value})
	require.NoError(t, err)

	// alpha covers both instruments, so beta carries the response that names
	// both.
	s.enableBeta(t)
	s.beta.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, cusip, xnas}},
	}}
	k := s.stateUnder(t, s.newStatement(t, gen.BrokerIbkr), gen.AssetClassStock, "USD", cusip)
	got := s.resolve(t, k)
	if written(got[0]) != "matched" {
		t.Fatalf("outcome = %s, want matched", written(got[0]))
	}
	for _, inst := range []uuid.UUID{a.Instrument.ID, b.Instrument.ID} {
		if !s.exists(t, inst) {
			t.Errorf("instrument %s is gone, want both kept", inst)
		}
	}
	key := s.key(t, k.ID)
	if key.InstrumentID == nil || *key.InstrumentID != b.Instrument.ID || *key.ViaID != b.Identifier.ID {
		t.Errorf("key = %+v, want the instrument the stated CUSIP identifies, via it", key)
	}
	rows, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	want := fmt.Sprintf("isin GB00BH4HKS39 identifies instrument %s and isin GB00BH4HKS40 identifies %s; not merged", a.Instrument.ID, b.Instrument.ID)
	var details []string
	for _, f := range rows {
		if *f.Finding.StatedKeyID == k.ID {
			details = append(details, string(f.Finding.Kind)+": "+*f.Finding.Detail)
		}
	}
	if diff := cmp.Diff([]string{"contradiction: " + want}, details); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
	ids, err := s.q.ListIdentifiers(ctx, b.Instrument.ID)
	require.NoError(t, err)
	for _, row := range ids {
		if row.Type == figi.Type {
			t.Errorf("wrote %s onto the CUSIP's instrument, which the other instrument carries", row.Value)
		}
	}
}
