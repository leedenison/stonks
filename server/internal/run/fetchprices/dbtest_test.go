//go:build dbtest

package fetchprices

import (
	"context"
	"log/slog"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/price"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// start is the moment a stack's clock first reads: a Tuesday. The last
// completed day is the day before it.
var start = time.Date(2026, time.March, 10, 15, 30, 0, 0, time.UTC)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func span(from, before time.Time) types.Period { return types.Period{From: from, Before: before} }

// at is an as traded price on day d.
func at(d time.Time, p string) market.DayPrice {
	return market.DayPrice{Day: d, Price: decimal.RequireFromString(p), AsAt: d}
}

// history is a provider's whole history of one identifier in currency, over
// p.
func history(currency string, p types.Period, prices ...market.DayPrice) market.PriceResult {
	return market.PriceResult{Prices: prices, Currency: currency, Coverage: &p}
}

// year is a history that spans every day a fund of these tests demands.
var year = span(day(2026, 1, 1), day(2027, 1, 1))

// stack is a Service and a Starter over a rolled-back transaction, with two
// scripted datasources: alpha at precedence 10 and beta at 20.
type stack struct {
	q           *gen.Queries
	tx          pgx.Tx
	alpha, beta string
	// alphaPrices and betaPrices script what the two datasources serve.
	alphaPrices, betaPrices *scripted.Prices
	demand                  *price.Demand
	svc                     *Service
	starter                 *Starter
	// now is the moment the clock reads. A test moves it on to add a day.
	now time.Time
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	s := &stack{
		q: q, tx: tx, alpha: "alpha-" + uuid.NewString(), beta: "beta-" + uuid.NewString(),
		alphaPrices: scripted.NewPrices(), betaPrices: scripted.NewPrices(), now: start,
	}
	for name, precedence := range map[string]int32{s.alpha: 10, s.beta: 20} {
		_, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: precedence})
		require.NoError(t, err)
	}
	families, scales, err := currency.Load(ctx, q)
	require.NoError(t, err)
	factory := func(i market.Integration) market.Factory {
		return market.Factory{New: func(market.Config) (market.Integration, error) { return i, nil }}
	}
	log := slog.New(slog.DiscardHandler)
	sources, err := market.New(ctx, q, map[string]market.Factory{s.alpha: factory(s.alphaPrices), s.beta: factory(s.betaPrices)}, log)
	require.NoError(t, err)
	clock := func() time.Time { return s.now }
	sleep := func(context.Context, time.Duration) error { return nil }
	runs := runtest.Runner{Q: q}
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runs, log, market.WithClock(clock, sleep), market.WithRetry(time.Millisecond, 1))
	recorder := price.New(db.New[price.RecordQueries](tx), fetcher, families, scales, log)
	s.demand = price.NewDemand(q, families, clock)
	s.svc = New(s.demand, runs, recorder, sources, log)
	s.starter = NewStarter(s.demand, runs, recorder, sources, log)
	return s
}

// holder is a user with the ingest run of a statement that holds the user's
// transactions.
type holder struct {
	user uuid.UUID
	stmt gen.Run
}

func (s *stack) holder(t *testing.T) holder {
	t.Helper()
	ctx := context.Background()
	user, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = s.q.CreateStmt(ctx, gen.CreateStmtParams{ID: run.ID, UserID: user.ID, Broker: gen.BrokerIbkr, OrderFrom: day(2020, 1, 1), OrderBefore: day(2027, 1, 1)})
	require.NoError(t, err)
	return holder{user: user.ID, stmt: run}
}

// fund is a mutual fund with one listing that a current ticker reaches at
// XNAS.
type fund struct {
	instr, listing uuid.UUID
	// id is the identifier a fetch sends for the listing.
	id types.Identifier
}

func (s *stack) fund(t *testing.T, currency string) fund {
	t.Helper()
	ctx := context.Background()
	instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassMutualFund})
	require.NoError(t, err)
	l, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: currency})
	require.NoError(t, err)
	value := "T" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
	row := dbtest.Ticker(t, s.q, types.MarketTicker{Market: "US", Value: value})
	require.NoError(t, s.q.CreateResolvedTickers(ctx, gen.CreateResolvedTickersParams{
		TickerIds: []uuid.UUID{row.ID}, Markets: []string{"US"}, InstrIds: []uuid.UUID{instr.ID},
		Venues: []string{"XNAS"}, ValidFroms: []string{""}, ValidBefores: []string{""}, Currencies: []string{currency},
	}))
	return fund{instr: instr.ID, listing: l.ID, id: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: value}}
}

// buys records that h bought f as at d, through a key fixed to the listing.
func (s *stack) buys(t *testing.T, h holder, f fund, d time.Time) {
	t.Helper()
	ctx := context.Background()
	via, key := db.NewID(), db.NewID()
	_, err := s.tx.Exec(ctx, `INSERT INTO identifiers (id, instr_id, type, value) VALUES ($1, $2, 'isin', $3)`, via, f.instr, via.String())
	require.NoError(t, err)
	_, err = s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, instr_id, listing_id, via_identifier_id, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'isin', 'value', $6::text)), $3, $4, $5, 'datasource')`, key, h.user, f.instr, f.listing, via, via.String())
	require.NoError(t, err)
	_, err = s.q.CreateTxn(ctx, gen.CreateTxnParams{
		ID: db.NewID(), UserID: h.user, Broker: gen.BrokerIbkr, StmtID: h.stmt.ID, StatedKeyID: key,
		OrderDate: d, SettlementDate: d, AsAt: d, Quantity: decimal.NewFromInt(10),
	})
	require.NoError(t, err)
}

// held is a fund in currency that h holds from Monday 2 March 2026.
func (s *stack) held(t *testing.T, h holder, currency string) fund {
	t.Helper()
	f := s.fund(t, currency)
	s.buys(t, h, f, day(2026, 3, 2))
	return f
}

// run starts a fetch prices run and returns its row once it has ended.
func (s *stack) run(t *testing.T) gen.Run {
	t.Helper()
	row, err := s.starter.Start(context.Background())
	require.NoError(t, err)
	row, err = s.q.GetRunByID(context.Background(), row.ID)
	require.NoError(t, err)
	return row
}

// gaps returns the gaps of every user, by listing.
func (s *stack) gaps(t *testing.T) map[uuid.UUID][]types.Period {
	t.Helper()
	gaps, _, err := s.demand.Gaps(context.Background(), nil)
	require.NoError(t, err)
	out := map[uuid.UUID][]types.Period{}
	for _, g := range gaps {
		out[g.Listing] = g.Periods
	}
	return out
}

// sources returns the datasources that hold a price of listing, in order.
func (s *stack) sources(t *testing.T, listing uuid.UUID) []string {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `SELECT DISTINCT price_coverage.datasource
		FROM prices JOIN price_coverage ON price_coverage.id = prices.coverage_id
		WHERE price_coverage.listing_id = $1 ORDER BY 1`, listing)
	require.NoError(t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	return out
}

// outcomes returns the outcome of each fetch key that source was sent value
// for, oldest first.
func (s *stack) outcomes(t *testing.T, source, value string) []gen.FetchOutcome {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `SELECT fetch_keys.outcome FROM fetch_keys
		JOIN fetches ON fetches.id = fetch_keys.fetch_id
		WHERE fetches.datasource = $1 AND fetch_keys.sent_value = $2 ORDER BY fetches.created_at, fetches.id`, source, value)
	require.NoError(t, err)
	out, err := pgx.CollectRows(rows, pgx.RowTo[gen.FetchOutcome])
	require.NoError(t, err)
	return out
}

// sent returns the identifiers that p was sent, sorted.
func sent(p *scripted.Prices) []string {
	out := []string{}
	for _, r := range p.Sent {
		out = append(out, r.Identifier.Name())
	}
	sort.Strings(out)
	return out
}

// demanded is the gap of a fund held from 2 March 2026, before any fetch.
var demanded = []types.Period{span(day(2026, 2, 23), types.Day(start))}

// TestFailedKeyPassesDown checks that a key the first datasource fails is
// asked of the second in the same run, and that the first stores the prices
// of every other key.
func TestFailedKeyPassesDown(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	good, failed := s.held(t, h, "USD"), s.held(t, h, "USD")
	s.alphaPrices.Fails = map[types.Identifier]bool{failed.id: true}
	s.alphaPrices.Responses[good.id] = history("USD", year, at(day(2026, 3, 2), "10"))
	s.alphaPrices.Responses[failed.id] = history("USD", year, at(day(2026, 3, 2), "20"))
	s.betaPrices.Responses[good.id] = history("USD", year, at(day(2026, 3, 2), "11"))
	s.betaPrices.Responses[failed.id] = history("USD", year, at(day(2026, 3, 2), "21"))

	if row := s.run(t); row.State != gen.RunStateCompleted || row.UserID != nil {
		t.Errorf("run = %s for user %v, want a completed system run", row.State, row.UserID)
	}
	if diff := cmp.Diff([]string{s.alpha}, s.sources(t, good.listing)); diff != "" {
		t.Errorf("sources of the served listing mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{s.beta}, s.sources(t, failed.listing)); diff != "" {
		t.Errorf("sources of the failed listing mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{failed.id.Name()}, sent(s.betaPrices)); diff != "" {
		t.Errorf("requests to beta mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.FetchOutcome{gen.FetchOutcomeFailedTemporary}, s.outcomes(t, s.alpha, failed.id.Value)); diff != "" {
		t.Errorf("outcomes of the failed key at alpha mismatch (-want +got):\n%s", diff)
	}
	if got := s.gaps(t); len(got) != 0 {
		t.Errorf("gaps after the run = %v, want none", got)
	}
}

// TestGapRemainsWhenBothFail checks that a key both datasources fail leaves
// its gap as it was, that the run still completes, and that the next run asks
// both again.
func TestGapRemainsWhenBothFail(t *testing.T) {
	s := newStack(t)
	f := s.held(t, s.holder(t), "USD")
	s.alphaPrices.Fails = map[types.Identifier]bool{f.id: true}
	s.betaPrices.Fails = map[types.Identifier]bool{f.id: true}

	for i := 1; i <= 2; i++ {
		if row := s.run(t); row.State != gen.RunStateCompleted {
			t.Errorf("run %d = %s, want completed", i, row.State)
		}
		if diff := cmp.Diff(demanded, s.gaps(t)[f.listing]); diff != "" {
			t.Errorf("gap after run %d mismatch (-want +got):\n%s", i, diff)
		}
		if a, b := len(s.alphaPrices.Sent), len(s.betaPrices.Sent); a != i || b != i {
			t.Errorf("after run %d alpha was sent %d requests and beta %d, want %d each", i, a, b, i)
		}
	}
}

// TestPermanentFailureBlocks checks that a key the first datasource fails
// for good is served by the second, and that the next run records the key as
// blocked at the first without sending it.
func TestPermanentFailureBlocks(t *testing.T) {
	s := newStack(t)
	f := s.held(t, s.holder(t), "USD")
	s.alphaPrices.Fails = map[types.Identifier]bool{f.id: true}
	s.alphaPrices.Permanent = true
	s.betaPrices.Responses[f.id] = history("USD", year, at(day(2026, 3, 2), "21"))

	s.run(t)
	if diff := cmp.Diff([]string{s.beta}, s.sources(t, f.listing)); diff != "" {
		t.Errorf("sources mismatch (-want +got):\n%s", diff)
	}
	s.now = s.now.AddDate(0, 0, 1)
	s.run(t)
	want := []gen.FetchOutcome{gen.FetchOutcomeFailedPermanent, gen.FetchOutcomeBlocked}
	if diff := cmp.Diff(want, s.outcomes(t, s.alpha, f.id.Value)); diff != "" {
		t.Errorf("outcomes at alpha mismatch (-want +got):\n%s", diff)
	}
	if n := len(s.alphaPrices.Sent); n != 1 {
		t.Errorf("alpha was sent %d requests, want 1", n)
	}
}

// TestSurplusIsStored checks that a run stores the whole history a
// datasource serves, so a holding that later reaches back inside that
// history has no gap.
func TestSurplusIsStored(t *testing.T) {
	s := newStack(t)
	f := s.held(t, s.holder(t), "USD")
	decade := span(day(2016, 1, 1), day(2027, 1, 1))
	s.alphaPrices.Responses[f.id] = history("USD", decade, at(day(2023, 3, 1), "5"), at(day(2026, 3, 2), "10"))

	s.run(t)
	rows, err := s.q.ListPriceCoverage(context.Background(), []uuid.UUID{f.listing})
	require.NoError(t, err)
	var covered []types.Period
	for _, r := range rows {
		covered = append(covered, span(r.CoveredFrom, r.CoveredBefore))
	}
	if diff := cmp.Diff([]types.Period{span(day(2016, 1, 1), types.Day(start))}, covered); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
	var early int
	require.NoError(t, s.tx.QueryRow(context.Background(), `SELECT count(*) FROM prices JOIN price_coverage ON price_coverage.id = prices.coverage_id
		WHERE price_coverage.listing_id = $1 AND prices.day = $2`, f.listing, day(2023, 3, 1)).Scan(&early))
	if early != 1 {
		t.Errorf("%d prices stored for 2023-03-01, want 1", early)
	}

	s.buys(t, s.holder(t), f, day(2023, 3, 1))
	if got := s.gaps(t)[f.listing]; len(got) != 0 {
		t.Errorf("gap after an earlier holding = %v, want none", got)
	}
}

// TestHigherRankRecovers checks that a datasource that failed, and whose
// days the next one filled, stores its whole history when a later run asks
// it, so a read of the earlier days takes its prices.
func TestHigherRankRecovers(t *testing.T) {
	s := newStack(t)
	f := s.held(t, s.holder(t), "USD")
	s.alphaPrices.Down = true
	s.alphaPrices.Responses[f.id] = history("USD", year, at(day(2026, 3, 2), "10"))
	s.betaPrices.Responses[f.id] = history("USD", year, at(day(2026, 3, 2), "11"))

	s.run(t)
	read := func() string {
		t.Helper()
		got, err := price.Read(context.Background(), s.q, []uuid.UUID{f.listing}, span(day(2026, 3, 2), day(2026, 3, 3)), types.Day(s.now))
		require.NoError(t, err)
		require.Len(t, got[f.listing], 1)
		return got[f.listing][0].Datasource
	}
	if got := read(); got != s.beta {
		t.Errorf("datasource read after the first run = %s, want beta", got)
	}

	s.alphaPrices.Down = false
	s.now = s.now.AddDate(0, 0, 1)
	s.run(t)
	if got := read(); got != s.alpha {
		t.Errorf("datasource read after alpha recovered = %s, want alpha", got)
	}
}

// TestAfterStmtFetches checks the fetch after a statement: one system child of the
// ingest run, with a fetch beneath it for each datasource asked. It asks for
// the listings of the statement's user and for the rate of a currency the
// statement brought into use, and leaves a listing that only another user
// holds.
func TestAfterStmtFetches(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	h, other := s.holder(t), s.holder(t)
	usd, gbp := s.held(t, h, "USD"), s.held(t, h, "GBP")
	theirs := s.held(t, other, "USD")
	s.alphaPrices.Responses[usd.id] = history("USD", year, at(day(2026, 3, 2), "10"))
	rate := types.Identifier{Type: types.IdentifierTypeCurrency, Value: "GBP"}

	s.svc.AfterStmt(ctx, h.stmt)

	type child struct {
		Kind   gen.RunKind
		System bool
		State  gen.RunState
	}
	children := func(parent uuid.UUID) ([]child, []uuid.UUID) {
		t.Helper()
		rows, err := s.tx.Query(ctx, `SELECT id, kind, user_id IS NULL, state FROM runs WHERE parent_id = $1 ORDER BY created_at, id`, parent)
		require.NoError(t, err)
		defer rows.Close()
		var out []child
		var ids []uuid.UUID
		for rows.Next() {
			var id uuid.UUID
			var c child
			require.NoError(t, rows.Scan(&id, &c.Kind, &c.System, &c.State))
			out, ids = append(out, c), append(ids, id)
		}
		require.NoError(t, rows.Err())
		return out, ids
	}
	got, ids := children(h.stmt.ID)
	if diff := cmp.Diff([]child{{Kind: gen.RunKindFetchPrices, System: true, State: gen.RunStateCompleted}}, got); diff != "" {
		t.Fatalf("children of the statement mismatch (-want +got):\n%s", diff)
	}
	fetch := child{Kind: gen.RunKindDatasourceFetch, System: true, State: gen.RunStateCompleted}
	got, _ = children(ids[0])
	if diff := cmp.Diff([]child{fetch, fetch}, got); diff != "" {
		t.Errorf("children of the fetch prices run mismatch (-want +got):\n%s", diff)
	}
	want := []string{usd.id.Name(), gbp.id.Name(), rate.Name()}
	sort.Strings(want)
	if diff := cmp.Diff(want, sent(s.alphaPrices)); diff != "" {
		t.Errorf("requests to alpha mismatch (-want +got):\n%s", diff)
	}
	want = []string{gbp.id.Name(), rate.Name()}
	sort.Strings(want)
	if diff := cmp.Diff(want, sent(s.betaPrices)); diff != "" {
		t.Errorf("requests to beta mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(demanded, s.gaps(t)[theirs.listing]); diff != "" {
		t.Errorf("gap of the other user's listing mismatch (-want +got):\n%s", diff)
	}
}

// TestAfterStmtWithoutGap checks that AfterStmt returns without starting a
// run where the user of the statement lacks a gap.
func TestAfterStmtWithoutGap(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	s.svc.AfterStmt(context.Background(), h.stmt)
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(), `SELECT count(*) FROM runs WHERE parent_id = $1`, h.stmt.ID).Scan(&n))
	if n != 0 {
		t.Errorf("%d child runs, want none", n)
	}
}
