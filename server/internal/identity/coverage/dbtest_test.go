//go:build dbtest

package coverage

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// fixture holds a user with one statement, so a key can take transactions.
type fixture struct {
	tx        pgx.Tx
	q         *gen.Queries
	user      gen.User
	statement uuid.UUID
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
	f.statement = run.ID
	f.exec(t, "INSERT INTO statements (id, user_id, broker, order_from, order_before, row_count) VALUES ($1, $2, 'ibkr', $3, $4, 1)", run.ID, f.user.ID, day(1), day(31))
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
		f.exec(t, "INSERT INTO transactions (id, user_id, broker, statement_id, stated_key_id, order_date, settlement_date, as_at, quantity) VALUES ($1, $2, 'ibkr', $3, $4, $5, $5, $5, 1)", db.NewID(), f.user.ID, f.statement, id, day(n))
	}
	return id
}

// bareKey states one ticker without a venue and takes a transaction.
func (f *fixture) bareKey(t *testing.T, value string) {
	t.Helper()
	id := db.NewID()
	f.exec(t, `INSERT INTO stated_keys (id, user_id, identifiers) VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'value', $3::text)))`, id, f.user.ID, value)
	f.exec(t, "INSERT INTO transactions (id, user_id, broker, statement_id, stated_key_id, order_date, settlement_date, as_at, quantity) VALUES ($1, $2, 'ibkr', $3, $4, $5, $5, $5, 1)", db.NewID(), f.user.ID, f.statement, id, day(9))
}

// instrument returns an instrument whose listing carries value as a
// MIC_TICKER at each of domains.
func (f *fixture) instrument(t *testing.T, value string, domains ...string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	inst, err := f.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	listing, err := f.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: inst.ID, Currency: "USD"})
	require.NoError(t, err)
	for _, domain := range domains {
		_, err = f.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: inst.ID, ListingID: &listing.ID, Type: types.IdentifierTypeMicTicker, Domain: domain, Value: value})
		require.NoError(t, err)
	}
	return inst.ID
}

// attest asserts that the ticker value at market names instrument on a day,
// as an administrator.
func (f *fixture) attest(t *testing.T, instrument uuid.UUID, market, value string) {
	t.Helper()
	ctx := context.Background()
	run, err := f.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	err = f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), RunID: &run.ID, AttestedBy: &f.user.ID,
		Type: types.IdentifierTypeMicTicker, Domain: market, Value: value, InstrumentID: &instrument, AssertedOn: day(1),
	})
	require.NoError(t, err)
}

// TestQueries checks that an instrument's tickers are those on its listings
// and those named in its assertions, one per market, and that each ticker's
// transactions come from every venue of its market.
func TestQueries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	value := "ACME" + uuid.NewString()[:8]
	former := "OLD" + uuid.NewString()[:8]
	segment := f.key(t, "XNGS", value, 4)
	nasdaq := f.key(t, "XNAS", value, 6, 6)
	nyse := f.key(t, "XNYS", value, 7)
	f.key(t, "XLON", value, 8)
	f.key(t, "XNAS", "BETA"+uuid.NewString()[:8])
	f.bareKey(t, value)
	inst := f.instrument(t, value, "XNAS", "XNYS")
	f.attest(t, inst, "US", value)
	f.attest(t, inst, "US", former)

	mics, err := mic.Load(ctx, f.q)
	require.NoError(t, err)
	tickers, err := New(f.q, mics).InstrumentTickers(ctx, inst)
	require.NoError(t, err)
	wantTickers := []InstrumentTicker{
		{Ticker: types.MarketTicker{Market: "US", Value: value}, Current: true, Transactions: 4, Venues: []string{"XNAS", "XNYS"}},
		{Ticker: types.MarketTicker{Market: "US", Value: former}, Current: false, Transactions: 0, Venues: []string{}},
	}
	if diff := cmp.Diff(wantTickers, tickers); diff != "" {
		t.Errorf("InstrumentTickers mismatch (-want +got):\n%s", diff)
	}

	probes, err := mics.TickerProbes([]types.MarketTicker{{Market: "US", Value: value}})
	require.NoError(t, err)
	txs, err := f.q.ListTickerTransactions(ctx, probes)
	require.NoError(t, err)
	wantTxs := []gen.ListTickerTransactionsRow{
		{StatedKeyID: segment, AsAt: day(4)},
		{StatedKeyID: nasdaq, AsAt: day(6)},
		{StatedKeyID: nasdaq, AsAt: day(6)},
		{StatedKeyID: nyse, AsAt: day(7)},
	}
	if diff := cmp.Diff(wantTxs, txs); diff != "" {
		t.Errorf("ListTickerTransactions mismatch (-want +got):\n%s", diff)
	}
}
