//go:build dbtest

package coverage

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

// fixture holds a user with one statement, so a key can take transactions.
type fixture struct {
	tx   pgx.Tx
	q    *gen.Queries
	user gen.User
	stmt uuid.UUID
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	f := &fixture{tx: tx, q: q}
	var err error
	f.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &f.user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	f.stmt = run.ID
	f.exec(t, "INSERT INTO stmts (id, user_id, broker, order_from, order_before, row_count) VALUES ($1, $2, 'ibkr', $3, $4, 1)", run.ID, f.user.ID, day(1), day(31))
	return f
}

func (f *fixture) exec(t *testing.T, sql string, args ...any) {
	t.Helper()
	_, err := f.tx.Exec(context.Background(), sql, args...)
	require.NoError(t, err)
}

// key states one ticker and takes a transaction on each of days.
func (f *fixture) key(t *testing.T, domain, value string, days ...int) uuid.UUID {
	t.Helper()
	id := db.NewID()
	f.exec(t, `INSERT INTO stated_keys (id, user_id, identifiers) VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'domain', $3::text, 'value', $4::text)))`, id, f.user.ID, domain, value)
	for _, n := range days {
		f.exec(t, "INSERT INTO txns (id, user_id, broker, stmt_id, stated_key_id, order_date, settlement_date, as_at, quantity) VALUES ($1, $2, 'ibkr', $3, $4, $5, $5, $5, 1)", db.NewID(), f.user.ID, f.stmt, id, day(n))
	}
	return id
}

// bareKey states one ticker without a venue and takes a transaction.
func (f *fixture) bareKey(t *testing.T, value string) {
	t.Helper()
	id := db.NewID()
	f.exec(t, `INSERT INTO stated_keys (id, user_id, identifiers) VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'value', $3::text)))`, id, f.user.ID, value)
	f.exec(t, "INSERT INTO txns (id, user_id, broker, stmt_id, stated_key_id, order_date, settlement_date, as_at, quantity) VALUES ($1, $2, 'ibkr', $3, $4, $5, $5, $5, 1)", db.NewID(), f.user.ID, f.stmt, id, day(9))
}

// venuesKey states one ticker at each of venues and takes a transaction on
// each of days.
func (f *fixture) venuesKey(t *testing.T, venues []string, value string, days ...int) uuid.UUID {
	t.Helper()
	id := db.NewID()
	f.exec(t, `INSERT INTO stated_keys (id, user_id, identifiers)
		SELECT $1, $2, jsonb_agg(jsonb_build_object('type', 'mic_ticker', 'domain', venue, 'value', $4::text) ORDER BY venue)
		FROM unnest($3::text[]) AS venue`, id, f.user.ID, venues, value)
	for _, n := range days {
		f.exec(t, "INSERT INTO txns (id, user_id, broker, stmt_id, stated_key_id, order_date, settlement_date, as_at, quantity) VALUES ($1, $2, 'ibkr', $3, $4, $5, $5, $5, 1)", db.NewID(), f.user.ID, f.stmt, id, day(n))
	}
	return id
}

// validity gives the ticker a confirmed interval naming instr, from day from
// up to day before.
func (f *fixture) validity(t *testing.T, instr uuid.UUID, market, value string, from, before int) {
	t.Helper()
	row := dbtest.Ticker(t, f.q, types.MarketTicker{Market: market, Value: value})
	err := f.q.CreateTickerValidity(context.Background(), gen.CreateTickerValidityParams{
		TickerIds: []uuid.UUID{row.ID}, Markets: []string{market}, InstrIds: []uuid.UUID{instr},
		ValidFroms: []string{day(from).Format(time.DateOnly)}, ValidBefores: []string{day(before).Format(time.DateOnly)},
		Validities: []string{string(gen.ValidityConfirmed)},
	})
	require.NoError(t, err)
}

// instr returns an instrument with a USD listing.
func (f *fixture) instr(t *testing.T) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	instr, err := f.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	_, err = f.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: "USD"})
	require.NoError(t, err)
	return instr.ID
}

// attest asserts that the ticker value at market names instrument on a day,
// as an administrator.
func (f *fixture) attest(t *testing.T, instr uuid.UUID, market, value string) {
	t.Helper()
	ctx := context.Background()
	run, err := f.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	err = f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), RunID: &run.ID, AttestedBy: &f.user.ID,
		TickerID: dbtest.Ticker(t, f.q, types.MarketTicker{Market: market, Value: value}).ID, Market: market, InstrID: &instr, AssertedFrom: day(1), AssertedBefore: day(1).AddDate(0, 0, 1),
	})
	require.NoError(t, err)
}

// names writes a row of resolved_tickers that names instr by value at
// market and venue, open where open is set.
func (f *fixture) names(t *testing.T, instr uuid.UUID, market, value, venue string, open bool) {
	t.Helper()
	row := dbtest.Ticker(t, f.q, types.MarketTicker{Market: market, Value: value})
	var before *time.Time
	if !open {
		before = ptr.To(day(5))
	}
	f.exec(t, `INSERT INTO resolved_tickers (ticker_id, market, instr_id, venue, valid_before, currency)
		VALUES ($1, $2, $3, $4, $5, 'USD')`, row.ID, market, instr, venue, before)
}

// TestQueries checks that an instrument's tickers are those of its names and
// those named in its assertions, each with its relation, and that each
// ticker's transactions come from every venue of its market.
func TestQueries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	value := "ACME" + uuid.NewString()[:8]
	former := "OLD" + uuid.NewString()[:8]
	defunct := "GONE" + uuid.NewString()[:8]
	segment := f.key(t, "XNGS", value, 4)
	nasdaq := f.key(t, "XNAS", value, 6, 6)
	nyse := f.key(t, "XNYS", value, 7)
	f.key(t, "XLON", value, 8)
	f.key(t, "XNAS", "BETA"+uuid.NewString()[:8])
	f.bareKey(t, value)
	instr := f.instr(t)
	f.attest(t, instr, "US", value)
	f.attest(t, instr, "US", former)
	f.names(t, instr, "US", value, "XNAS", true)
	f.names(t, instr, "US", value, "XNYS", true)
	f.names(t, instr, "XLON", defunct, "XLON", false)
	f.names(t, f.instr(t), "XLON", defunct, "XLON", true)

	mics, err := mic.Load(ctx, f.q)
	require.NoError(t, err)
	tickers, err := New(f.q, mics).InstrTickers(ctx, instr)
	require.NoError(t, err)
	wantTickers := []InstrTicker{
		{Ticker: types.MarketTicker{Market: "US", Value: value}, Relation: gen.TickerRelationCurrent, Txns: 4, Venues: []string{"XNAS", "XNYS"}},
		{Ticker: types.MarketTicker{Market: "XLON", Value: defunct}, Relation: gen.TickerRelationDefunct, Txns: 0, Venues: []string{"XLON"}},
		{Ticker: types.MarketTicker{Market: "US", Value: former}, Relation: gen.TickerRelationFormer, Txns: 0, Venues: []string{}},
	}
	if diff := cmp.Diff(wantTickers, tickers); diff != "" {
		t.Errorf("InstrTickers mismatch (-want +got):\n%s", diff)
	}

	probes, err := mics.TickerProbes([]types.MarketTicker{{Market: "US", Value: value}})
	require.NoError(t, err)
	txns, err := f.q.ListTickerTxns(ctx, probes)
	require.NoError(t, err)
	wantTxns := []gen.ListTickerTxnsRow{
		{StatedKeyID: segment, AsAt: day(4)},
		{StatedKeyID: nasdaq, AsAt: day(6)},
		{StatedKeyID: nasdaq, AsAt: day(6)},
		{StatedKeyID: nyse, AsAt: day(7)},
	}
	if diff := cmp.Diff(wantTxns, txns); diff != "" {
		t.Errorf("ListTickerTxns mismatch (-want +got):\n%s", diff)
	}
}

// TestRelations checks that resolved_relations gives each row of
// resolved_tickers the relation that instr_tickers gives its ticker and
// instrument: current, defunct where another instrument holds the ticker
// open, and former otherwise.
func TestRelations(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	current, defunct, former := "CUR"+suffix, "GONE"+suffix, "OLD"+suffix
	instr, other := f.instr(t), f.instr(t)
	f.attest(t, instr, "US", former)
	f.names(t, instr, "US", current, "XNAS", true)
	f.names(t, instr, "US", current, "XNYS", true)
	f.names(t, instr, "XLON", defunct, "XLON", false)
	f.names(t, other, "XLON", defunct, "XLON", true)
	f.names(t, instr, "XTSE", former, "XTSE", false)

	rows, err := f.tx.Query(ctx, `
		SELECT tickers.value, resolved_relations.venue, resolved_relations.instr_id,
		       resolved_relations.relation::text, instr_tickers.relation::text
		FROM resolved_relations
		JOIN tickers ON tickers.id = resolved_relations.ticker_id
		JOIN instr_tickers ON instr_tickers.ticker_id = resolved_relations.ticker_id
		    AND instr_tickers.instr_id = resolved_relations.instr_id
		WHERE resolved_relations.instr_id = ANY($1)
		ORDER BY tickers.value, resolved_relations.venue, resolved_relations.instr_id`, []uuid.UUID{instr, other})
	require.NoError(t, err)
	type relation struct {
		Value, Venue, Relation string
		Instr                  uuid.UUID
	}
	var got []relation
	for rows.Next() {
		var r relation
		var view string
		require.NoError(t, rows.Scan(&r.Value, &r.Venue, &r.Instr, &r.Relation, &view))
		if r.Relation != view {
			t.Errorf("%s at %s: resolved_relations says %s, instr_tickers says %s", r.Value, r.Venue, r.Relation, view)
		}
		got = append(got, r)
	}
	require.NoError(t, rows.Err())
	want := []relation{
		{Value: current, Venue: "XNAS", Instr: instr, Relation: "current"},
		{Value: current, Venue: "XNYS", Instr: instr, Relation: "current"},
		{Value: defunct, Venue: "XLON", Instr: instr, Relation: "defunct"},
		{Value: defunct, Venue: "XLON", Instr: other, Relation: "current"},
		{Value: former, Venue: "XTSE", Instr: instr, Relation: "former"},
	}
	if instr.String() > other.String() {
		want[2], want[3] = want[3], want[2]
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("resolved_relations mismatch (-want +got):\n%s", diff)
	}
}

// TestStatedTickerTxns checks that every stated ticker's transactions are
// gathered under its market, a segment MIC included. A key at two venues
// counts once per transaction and a bare ticker is left out, so the count
// agrees with ListTickerTxns. Against the same rows, Unconfirmed reads a
// ticker without a validity row as naming nothing and a ticker with a
// confirmed interval by its gaps.
func TestStatedTickerTxns(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	suffix := uuid.NewString()[:8]
	value, named := "ACME"+suffix, "NAMED"+suffix
	segment := f.key(t, "XNGS", value, 4)
	nasdaq := f.key(t, "XNAS", value, 6, 6)
	nyse := f.key(t, "XNYS", value, 7)
	both := f.venuesKey(t, []string{"XNAS", "XNYS"}, value, 8)
	f.bareKey(t, value)
	london := f.key(t, "XLON", named, 2, 5)
	instr := f.instr(t)
	f.validity(t, instr, "XLON", named, 1, 3)

	rows, err := f.q.ListStatedTickerTxns(ctx)
	require.NoError(t, err)
	want := []gen.ListStatedTickerTxnsRow{
		{Market: "US", Value: value, StatedKeyID: segment, AsAt: day(4), Txns: 1},
		{Market: "US", Value: value, StatedKeyID: nasdaq, AsAt: day(6), Txns: 2},
		{Market: "US", Value: value, StatedKeyID: nyse, AsAt: day(7), Txns: 1},
		{Market: "US", Value: value, StatedKeyID: both, AsAt: day(8), Txns: 1},
		{Market: "XLON", Value: named, StatedKeyID: london, AsAt: day(2), Txns: 1},
		{Market: "XLON", Value: named, StatedKeyID: london, AsAt: day(5), Txns: 1},
	}
	if diff := cmp.Diff(want, rows); diff != "" {
		t.Errorf("ListStatedTickerTxns mismatch (-want +got):\n%s", diff)
	}
	mics, err := mic.Load(ctx, f.q)
	require.NoError(t, err)
	probes, err := mics.TickerProbes([]types.MarketTicker{{Market: "US", Value: value}})
	require.NoError(t, err)
	txns, err := f.q.ListTickerTxns(ctx, probes)
	require.NoError(t, err)
	var sum int32
	for _, r := range rows[:4] {
		sum += r.Txns
	}
	if int(sum) != len(txns) {
		t.Errorf("ListStatedTickerTxns counts %d transactions of US:%s, ListTickerTxns %d", sum, value, len(txns))
	}

	nothing, gapped, err := New(f.q, mics).Unconfirmed(ctx, day(30))
	require.NoError(t, err)
	wantNothing := []NamesNothing{{Ticker: types.MarketTicker{Market: "US", Value: value}, Keys: 4, Txns: 5, First: day(4)}}
	if diff := cmp.Diff(wantNothing, nothing); diff != "" {
		t.Errorf("Unconfirmed names nothing mismatch (-want +got):\n%s", diff)
	}
	wantGaps := []TickerGap{{
		Ticker: types.MarketTicker{Market: "XLON", Value: named}, Instr: instr,
		Gap: Gap{Period: types.Period{From: day(3), Before: day(31)}, Keys: 1, Txns: 1},
	}}
	if diff := cmp.Diff(wantGaps, gapped); diff != "" {
		t.Errorf("Unconfirmed gaps mismatch (-want +got):\n%s", diff)
	}
}
