//go:build dbtest

package resolve

import (
	"context"
	"errors"
	"log"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("STONKS_TEST_DATABASE_URL")
	if url == "" {
		log.Fatal("STONKS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	var err error
	pool, err = db.Open(ctx, url)
	if err != nil {
		log.Fatalf("open pool: %v", err)
	}
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

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

// script is an integration whose response to each identifier is scripted.
type script struct {
	responses map[types.Identifier]market.IdentityResult
}

func (s *script) Classify(error) market.Failure {
	return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
}
func (s *script) Limit() (rate.Limit, int) { return rate.Inf, 1 }
func (s *script) Batch() int               { return 10 }
func (s *script) Serves(k gen.StatedKey) (types.Identifier, error) {
	for _, id := range k.Identifiers {
		if market.IsGUID(id) {
			return id, nil
		}
	}
	return types.Identifier{}, errors.New("no global identifier")
}

func (s *script) Fetch(_ context.Context, reqs []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	out := make([]market.Response[market.IdentityResult], len(reqs))
	for i, r := range reqs {
		out[i] = market.Response[market.IdentityResult]{Value: s.responses[r.Sent]}
	}
	return out, nil
}

// syncRunner records each child run inline over the test's transaction.
type syncRunner struct {
	q *gen.Queries
}

func (r syncRunner) Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
	row, err := r.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: parent.UserID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	if err != nil {
		return row, err
	}
	if _, err := r.q.StartRun(ctx, row.ID); err != nil {
		return row, err
	}
	if err := work(ctx, row); err != nil {
		return row, errors.Join(err, r.q.FailRun(ctx, gen.FailRunParams{ID: row.ID, Error: err.Error()}))
	}
	return row, r.q.CompleteRun(ctx, row.ID)
}

// stack is a resolver over a transaction rolled back when the test ends,
// with one scripted datasource, and a user with a statement for stating
// keys.
type stack struct {
	q          *gen.Queries
	tx         pgx.Tx
	user       gen.User
	statement  gen.Run
	resolution gen.Run
	script     *script
	resolver   *Resolver
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	q := gen.New(tx)
	s := &stack{q: q, tx: tx, script: &script{responses: map[types.Identifier]market.IdentityResult{}}}
	s.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.statement, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: s.user.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = q.CreateStatement(ctx, gen.CreateStatementParams{ID: s.statement.ID, UserID: s.user.ID, Broker: gen.BrokerIbkr, OrderFrom: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), OrderBefore: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	s.resolution, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &s.statement.ID})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: "alpha", Enabled: true, Precedence: 10})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	factories := map[string]market.Factory{"alpha": func(market.Config) (market.Integration, error) { return s.script, nil }}
	sources, err := market.New(ctx, q, factories, log)
	require.NoError(t, err)
	fetcher := market.NewFetcher(q, syncRunner{q: q}, log)
	s.resolver = New(db.New[Queries](tx), market.IdentityFetcher{F: fetcher}, sources, log)
	return s
}

// state records a stated key under the statement.
func (s *stack) state(t *testing.T, class gen.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	t.Helper()
	arg := gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: s.statement.ID, UserID: s.user.ID, Identifiers: ids}
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
	keys, err := s.q.ListStatedKeys(context.Background(), gen.ListStatedKeysParams{StatementID: s.statement.ID, UserID: s.user.ID})
	require.NoError(t, err)
	for _, k := range keys {
		if k.ID == id {
			return k
		}
	}
	t.Fatalf("no stated key %s", id)
	return gen.StatedKey{}
}

// TestResolveWrites checks a resolution against real rows: the instrument a
// response creates with its listing, identifiers, provenance, fetch
// identifiers and coverage; a second key attaching to it without a fetch; a
// cash key resolving to the seed; and a key whose every group is dropped,
// with its finding.
func TestResolveWrites(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBX", Identifiers: []types.Identifier{figi, isin, comp, xlon}},
	}}
	trade := s.state(t, gen.AssetClassStock, "GBX", isin, xlon)
	transfer := s.state(t, gen.AssetClassEquity, "", isin)
	cash := s.state(t, gen.AssetClassCash, "GBX", id(types.IdentifierTypeCurrency, "", "GBX"))
	wrong := s.state(t, gen.AssetClassStock, "USD", cusip)
	s.script.responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
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

	// The trade created the instrument, from the fetch key that served it.
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

	// The trade associates through its ISIN, confirmed, on the GBP listing,
	// its ticker being the weaker; the transfer through the ISIN with no
	// listing.
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

	// The cash key names the GBP seed through the GBX identifier.
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
	items, err := s.q.ListFetchItems(ctx, children[0].ID)
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

	// Dropping the mis-stated key's group is a finding of the resolution.
	findings, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	if len(findings) != 1 || findings[0].Finding.Kind != gen.FindingKindDropped || *findings[0].Finding.Step != gen.DropStepStated || *findings[0].Finding.StatedKeyID != wrong.ID || *findings[0].Finding.Detail != "cusip 92857W308: stated USD has no listing among GBP (alpha)" {
		t.Errorf("findings = %+v, want one stated drop against the mis-stated key", findings)
	}
	if _, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCusip, Value: cusip.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("FindIdentifier(cusip) error = %v, want not found: a dropped group is not stored", err)
	}
}

// TestMergeRows checks the merge against real rows: when a response
// identifies two instruments, the merge folds the later into the earlier,
// moving the listing the survivor lacks with its identifiers, relinking the
// stated keys, fetch keys and coverage, and deleting the rest, under the
// deferred constraints.
func TestMergeRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
		{Class: gen.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Candidates: []market.Candidate{
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

	// A third key states a SEDOL the database lacks, in USD, and the
	// response names the FIGI of the first instrument and the CUSIP of the
	// second.
	sedol := id(types.IdentifierTypeSedol, "", "BH4HKS3")
	s.script.responses[sedol] = market.IdentityResult{Filtered: []types.Identifier{sedol}, Candidates: []market.Candidate{
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
