//go:build dbtest

package tickervalidity

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// fixture holds a user, an administrator, two instruments and a fetch for each
// of two datasources, alpha with the lower precedence number. Its helpers
// assert the ticker FOO at the market US, which a key states at XNYS.
type fixture struct {
	tx         pgx.Tx
	q          *gen.Queries
	user       gen.User
	run        gen.Run
	fetches    map[string]uuid.UUID
	foo        types.MarketTicker
	at         types.Identifier
	a, b       uuid.UUID
	alpha      string
	beta       string
	attestedBy gen.User
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	f := &fixture{tx: tx, q: q, fetches: map[string]uuid.UUID{}, alpha: "alpha-" + uuid.NewString(), beta: "beta-" + uuid.NewString()}
	f.foo = types.MarketTicker{Market: "US", Value: "FOO" + uuid.NewString()[:8]}
	f.at = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: f.foo.Value}
	var err error
	f.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	f.attestedBy, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleAdmin})
	require.NoError(t, err)
	f.run, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &f.user.ID, Kind: gen.RunKindResolveStmtKeys, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	for i, name := range []string{f.alpha, f.beta} {
		_, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: int32(1000 + i)})
		require.NoError(t, err)
		fetch, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &f.user.ID, Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, ParentID: &f.run.ID})
		require.NoError(t, err)
		_, err = q.CreateFetch(ctx, gen.CreateFetchParams{ID: fetch.ID, UserID: &f.user.ID, Datasource: name, Endpoint: "https://example.com", Kind: gen.FetchKindIdentity})
		require.NoError(t, err)
		f.fetches[name] = fetch.ID
	}
	for _, id := range []*uuid.UUID{&f.a, &f.b} {
		instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
		require.NoError(t, err)
		*id = instr.ID
	}
	return f
}

// day is the nth day of March 2026.
func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

// asserts records datasource asserting that foo names instrument on day n,
// through a fetch key of its own.
func (f *fixture) asserts(t *testing.T, datasource string, n int, instr *uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	key := db.NewID()
	require.NoError(t, f.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: f.fetches[datasource], UserID: &f.user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &f.at.Type, SentDomain: f.at.Domain, SentValue: &f.at.Value, AnsweredAt: ptr.To(day(n)),
	}))
	require.NoError(t, f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, TickerID: dbtest.Ticker(t, f.q, f.foo).ID, Market: f.foo.Market, InstrID: instr, AssertedOn: day(n),
	}))
}

// exec runs sql with args in the fixture's transaction.
func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := f.tx.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

func (f *fixture) of(t *testing.T) []Interval {
	t.Helper()
	ivs, err := Of(context.Background(), f.q, f.foo)
	require.NoError(t, err)
	return ivs
}

func TestOfGreyZone(t *testing.T) {
	f := newFixture(t)
	f.asserts(t, f.alpha, 1, &f.a)
	f.asserts(t, f.beta, 5, &f.b)
	want := []Interval{
		provisional(nil, at(1), f.a), confirmed(at(1), at(2), f.a), provisional(at(2), at(5), f.a),
		confirmed(at(5), at(6), f.b), provisional(at(6), nil, f.b),
	}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestOfNegativeOfAnotherDatasource checks that a datasource that never
// asserted the ticker cannot retire it.
func TestOfNegativeOfAnotherDatasource(t *testing.T) {
	f := newFixture(t)
	f.asserts(t, f.alpha, 1, &f.a)
	f.asserts(t, f.beta, 5, nil)
	want := []Interval{provisional(nil, at(1), f.a), confirmed(at(1), at(2), f.a), provisional(at(2), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestOfPrecedence checks that, on one day, alpha's assertion wins over
// beta's.
func TestOfPrecedence(t *testing.T) {
	f := newFixture(t)
	f.asserts(t, f.beta, 1, &f.b)
	f.asserts(t, f.alpha, 1, &f.a)
	want := []Interval{provisional(nil, at(1), f.a), confirmed(at(1), at(2), f.a), provisional(at(2), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestOfEdgesAndAssertionCoverage checks that a period of assertion coverage of
// the market confirms the ticker, and that a first edge of the ticker bounds
// it.
func TestOfEdgesAndAssertionCoverage(t *testing.T) {
	f := newFixture(t)
	f.asserts(t, f.alpha, 5, &f.a)
	key := db.NewID()
	require.NoError(t, f.q.CreateFetchKey(context.Background(), gen.CreateFetchKeyParams{
		ID: key, FetchID: f.fetches[f.alpha], UserID: &f.user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &f.at.Type, SentDomain: "XNYS", AnsweredAt: ptr.To(day(20)),
	}))
	f.exec(t, "INSERT INTO identifier_assertion_coverage (id, fetch_key_id, market, covered_from, covered_before) VALUES ($1, $2, 'US', $3, $4)", db.NewID(), key, day(1), day(20))
	require.NoError(t, f.q.CreateIdentifierAssertion(context.Background(), gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, TickerID: dbtest.Ticker(t, f.q, f.foo).ID, Market: f.foo.Market, InstrID: &f.a,
		AssertedOn: day(3), Edge: ptr.To(gen.IdentifierEdgeFirst),
	}))
	want := []Interval{confirmed(at(3), at(20), f.a), provisional(at(20), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestOfAttested checks that an administrator's assertion on an earlier day
// brackets a datasource's.
func TestOfAttested(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.asserts(t, f.alpha, 20, &f.a)
	run, err := f.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	require.NoError(t, f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), RunID: &run.ID, AttestedBy: &f.attestedBy.ID, TickerID: dbtest.Ticker(t, f.q, f.foo).ID, Market: f.foo.Market,
		InstrID: &f.a, AssertedOn: day(2),
	}))
	want := []Interval{provisional(nil, at(2), f.a), confirmed(at(2), at(21), f.a), provisional(at(21), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestReadRowsAssertionCoverage checks that each period of a ticker's assertion
// coverage names the datasource that wrote it and its precedence, and that a
// row of the whole market has no value.
func TestReadRowsAssertionCoverage(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	key := db.NewID()
	require.NoError(t, f.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: f.fetches[f.alpha], UserID: &f.user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &f.at.Type, SentDomain: f.at.Domain, AnsweredAt: ptr.To(day(20)),
	}))
	f.exec(t, "INSERT INTO identifier_assertion_coverage (id, fetch_key_id, market, ticker_id, covered_from, covered_before) VALUES ($1, $2, $3, $4, $5, $6)", db.NewID(), key, f.foo.Market, dbtest.Ticker(t, f.q, f.foo).ID, day(5), day(20))
	f.exec(t, "INSERT INTO identifier_assertion_coverage (id, fetch_key_id, market, covered_from, covered_before) VALUES ($1, $2, $3, $4, $5)", db.NewID(), key, f.foo.Market, day(1), day(10))
	rows, err := ReadRows(ctx, f.q, f.foo)
	require.NoError(t, err)
	want := []gen.ListIdentifierAssertionCoverageRow{
		{Market: f.foo.Market, CoveredFrom: day(1), CoveredBefore: day(10), FetchKeyID: key, Datasource: f.alpha, Precedence: 1000},
		{Market: f.foo.Market, Value: &f.foo.Value, CoveredFrom: day(5), CoveredBefore: day(20), FetchKeyID: key, Datasource: f.alpha, Precedence: 1000},
	}
	if diff := cmp.Diff(want, rows.AssertionCoverage); diff != "" {
		t.Errorf("ReadRows assertion coverage mismatch (-want +got):\n%s", diff)
	}
}

// TestReadRowsOf checks that one read of several tickers keeps each ticker's
// facts apart. A ticker that shares a value or a market with an asked one is
// left out. A ticker that was never created still gets the assertion coverage
// of its market.
func TestReadRowsOf(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	key := db.NewID()
	require.NoError(t, f.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: f.fetches[f.alpha], UserID: &f.user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &f.at.Type, SentDomain: f.at.Domain, AnsweredAt: ptr.To(day(20)),
	}))
	suffix := uuid.NewString()[:8]
	usA := types.MarketTicker{Market: "US", Value: "A" + suffix}
	lonB := types.MarketTicker{Market: "XLON", Value: "B" + suffix}
	usB := types.MarketTicker{Market: "US", Value: "B" + suffix}
	usOld := types.MarketTicker{Market: "US", Value: "OLD" + suffix}
	usNew := types.MarketTicker{Market: "US", Value: "NEW" + suffix}
	unrowed := types.MarketTicker{Market: "US", Value: "NONE" + suffix}
	for _, mt := range []types.MarketTicker{usA, lonB, usB} {
		row := dbtest.Ticker(t, f.q, mt)
		require.NoError(t, f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), FetchKeyID: &key, TickerID: row.ID, Market: row.Market, InstrID: &f.a, AssertedOn: day(20),
		}))
	}
	for mt, e := range map[types.MarketTicker]gen.IdentifierEdge{usOld: gen.IdentifierEdgeLast, usNew: gen.IdentifierEdgeFirst} {
		row := dbtest.Ticker(t, f.q, mt)
		require.NoError(t, f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), FetchKeyID: &key, TickerID: row.ID, Market: row.Market, InstrID: &f.a, AssertedOn: day(3), Edge: &e,
		}))
	}
	f.exec(t, "INSERT INTO identifier_assertion_coverage (id, fetch_key_id, market, covered_from, covered_before) VALUES ($1, $2, 'US', $3, $4)", db.NewID(), key, day(1), day(10))

	rows, err := ReadRowsOf(ctx, f.q, []types.MarketTicker{usA, lonB, usOld, usNew, unrowed})
	require.NoError(t, err)
	got := map[types.MarketTicker][]int{}
	for mt, r := range rows {
		got[mt] = []int{len(r.Assertions), len(r.AssertionCoverage)}
	}
	want := map[types.MarketTicker][]int{
		usA: {1, 1}, lonB: {1, 0}, usOld: {1, 1}, usNew: {1, 1}, unrowed: {0, 1},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ReadRowsOf assertions and assertion coverage per ticker (-want +got):\n%s", diff)
	}
}

// TestReadTicker checks that one read of a ticker's facts answers whether it
// has ticker validity, which days hold an assertion, and the earliest date its
// validity leaves unconfirmed.
func TestReadTicker(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	empty, err := ReadTicker(ctx, f.q, f.foo)
	require.NoError(t, err)
	if empty.HasTickerValidity() {
		t.Errorf("a ticker with no facts has ticker validity %+v, want none", empty.Derived.Intervals)
	}
	f.asserts(t, f.alpha, 1, &f.a)
	f.asserts(t, f.alpha, 5, &f.a)
	tk, err := ReadTicker(ctx, f.q, f.foo)
	require.NoError(t, err)
	if !tk.HasTickerValidity() || tk.Ticker != f.foo {
		t.Errorf("ticker = %+v, want ticker validity for %s", tk, f.foo.Name())
	}
	if !tk.Asserted(day(5)) || tk.Asserted(day(3)) {
		t.Errorf("Asserted(5) = %v, Asserted(3) = %v, want only the asserted day", tk.Asserted(day(5)), tk.Asserted(day(3)))
	}
	if got, ok := tk.FirstUnconfirmed([]time.Time{day(3), day(9), day(7)}); !ok || !got.Equal(day(7)) {
		t.Errorf("FirstUnconfirmed = %s, %v, want day 7: days 1 to 5 are bracketed", got, ok)
	}
	if _, ok := tk.FirstUnconfirmed([]time.Time{day(2), day(4)}); ok {
		t.Errorf("FirstUnconfirmed of bracketed days reported one, want none")
	}
}

// TestReadTickersAcrossVenues checks that a ticker stated at two venues of
// one market reads one set of facts: the assertions of the ticker and the
// assertion coverage of the market.
func TestReadTickersAcrossVenues(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	f.asserts(t, f.alpha, 5, &f.a)
	key := db.NewID()
	require.NoError(t, f.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: f.fetches[f.alpha], UserID: &f.user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &f.at.Type, SentDomain: f.at.Domain, AnsweredAt: ptr.To(day(20)),
	}))
	f.exec(t, "INSERT INTO identifier_assertion_coverage (id, fetch_key_id, market, covered_from, covered_before) VALUES ($1, $2, 'US', $3, $4)", db.NewID(), key, day(1), day(20))
	f.exec(t, "INSERT INTO identifier_assertions (id, fetch_key_id, market, ticker_id, instr_id, asserted_on, edge) VALUES ($1, $2, 'US', $3, $4, $5, 'first')", db.NewID(), key, dbtest.Ticker(t, f.q, f.foo).ID, f.a, day(3))
	mics, err := mic.Load(ctx, f.q)
	require.NoError(t, err)
	var keys []types.MarketTicker
	for _, venue := range []string{"XNYS", "XNAS"} {
		mt, ok := mics.MarketTicker(types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: f.foo.Value})
		require.True(t, ok)
		keys = append(keys, mt)
	}
	read, err := ReadTickers(ctx, f.q, keys)
	require.NoError(t, err)
	if len(read) != 1 || read[f.foo] == nil {
		t.Fatalf("ReadTickers = %v, want one ticker, %s", read, f.foo.Name())
	}
	rows, err := ReadRows(ctx, f.q, f.foo)
	require.NoError(t, err)
	if len(rows.Assertions) != 2 || len(rows.AssertionCoverage) != 1 {
		t.Errorf("rows = %d assertions and %d rows of assertion coverage, want 2 and 1", len(rows.Assertions), len(rows.AssertionCoverage))
	}
}
