//go:build dbtest

package price

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// answered is the moment the stack's datasources answer: a Tuesday.
var answered = time.Date(2026, time.March, 10, 15, 30, 0, 0, time.UTC)

// stack is the queries over a rolled-back transaction, with two datasources:
// alpha at precedence 10 and beta at 20.
type stack struct {
	q      *gen.Queries
	tx     pgx.Tx
	parent gen.Run
	// alpha and beta are names unique to the test.
	alpha, beta string
	families    currency.Families
	scales      currency.Scales
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	s := &stack{q: q, tx: tx, alpha: "alpha-" + uuid.NewString(), beta: "beta-" + uuid.NewString()}
	for name, precedence := range map[string]int32{s.alpha: 10, s.beta: 20} {
		_, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: precedence})
		require.NoError(t, err)
	}
	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.parent, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	s.families, s.scales, err = currency.Load(ctx, q)
	require.NoError(t, err)
	return s
}

// listing creates a stock with one listing in currency.
func (s *stack) listing(t *testing.T, currency string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	l, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: currency})
	require.NoError(t, err)
	return l.ID
}

// key records a served fetch key of source, answered at the moment at.
func (s *stack) key(t *testing.T, source string, at time.Time) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: s.parent.UserID, Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, ParentID: &s.parent.ID})
	require.NoError(t, err)
	_, err = s.q.CreateFetch(ctx, gen.CreateFetchParams{ID: run.ID, UserID: s.parent.UserID, Datasource: source, Endpoint: "https://example.com", Kind: gen.FetchKindPrices})
	require.NoError(t, err)
	id := db.NewID()
	require.NoError(t, s.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: id, FetchID: run.ID, UserID: s.parent.UserID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: ptr.To(types.IdentifierTypeMicTicker), SentDomain: "XNAS", SentValue: ptr.To("ABC"), AnsweredAt: &at,
	}))
	return id
}

// answer is an answer of source for listing, given at answered, that covers
// p in currency.
func (s *stack) answer(t *testing.T, source string, listing uuid.UUID, currency string, p types.Period, prices ...market.DayPrice) Answer {
	t.Helper()
	return Answer{
		Listing: listing, Datasource: source, FetchKeyID: s.key(t, source, answered), AnsweredAt: answered,
		PriceResult: market.PriceResult{Prices: prices, Currency: currency, Coverage: &p},
	}
}

// record records a and fails the test on an error.
func (s *stack) record(t *testing.T, a Answer) bool {
	t.Helper()
	stored, err := Record(context.Background(), s.q, s.families, s.scales, a)
	require.NoError(t, err)
	return stored
}

// stored is a stored price with its datasource.
type stored struct {
	Datasource string
	Day        string
	Price      string
	AsAt       string
}

// prices returns every stored price of listing, by datasource and day.
func (s *stack) prices(t *testing.T, listing uuid.UUID) []stored {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `SELECT price_coverage.datasource, prices.day, prices.price, prices.as_at
		FROM prices JOIN price_coverage ON price_coverage.id = prices.coverage_id
		WHERE price_coverage.listing_id = $1 ORDER BY price_coverage.datasource, prices.day`, listing)
	require.NoError(t, err)
	defer rows.Close()
	var out []stored
	for rows.Next() {
		var source string
		var d, asAt time.Time
		var p decimal.Decimal
		require.NoError(t, rows.Scan(&source, &d, &p, &asAt))
		out = append(out, stored{Datasource: source, Day: d.Format(time.DateOnly), Price: p.String(), AsAt: asAt.Format(time.DateOnly)})
	}
	require.NoError(t, rows.Err())
	return out
}

// coverage returns the periods that source covers for listing, in order.
func (s *stack) coverage(t *testing.T, source string, listing uuid.UUID) []types.Period {
	t.Helper()
	rows, err := s.q.ListPriceCoverage(context.Background(), []uuid.UUID{listing})
	require.NoError(t, err)
	return periods(rows, source)
}

func span(from, before time.Time) types.Period { return types.Period{From: from, Before: before} }

// at is an as traded price on day d.
func at(d time.Time, price string) market.DayPrice {
	return market.DayPrice{Day: d, Price: decimal.RequireFromString(price), AsAt: d}
}

// holder is a user with a statement that holds the user's transactions.
type holder struct {
	user, stmt uuid.UUID
}

func (s *stack) holder(t *testing.T) holder {
	t.Helper()
	ctx := context.Background()
	user, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	stmt, err := s.q.CreateStmt(ctx, gen.CreateStmtParams{ID: run.ID, UserID: user.ID, Broker: gen.BrokerIbkr, OrderFrom: day(2020, 1, 1), OrderBefore: day(2027, 1, 1)})
	require.NoError(t, err)
	return holder{user: user.ID, stmt: stmt.ID}
}

// instr creates an instrument of class with a listing in each of
// currencies, and returns the listings by currency.
func (s *stack) instr(t *testing.T, class types.AssetClass, currencies ...string) (uuid.UUID, map[string]uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: class})
	require.NoError(t, err)
	listings := map[string]uuid.UUID{}
	for _, c := range currencies {
		l, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: c})
		require.NoError(t, err)
		listings[c] = l.ID
	}
	return instr.ID, listings
}

// fixedKey creates a key of h with a fixed association to instr, and to
// listing where one is given.
func (s *stack) fixedKey(t *testing.T, h holder, instr uuid.UUID, listing *uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	via, key := db.NewID(), db.NewID()
	_, err := s.tx.Exec(ctx, `INSERT INTO identifiers (id, instr_id, type, value) VALUES ($1, $2, 'isin', $3)`, via, instr, via.String())
	require.NoError(t, err)
	_, err = s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, instr_id, listing_id, via_identifier_id, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'isin', 'value', $6::text)), $3, $4, $5, 'datasource')`, key, h.user, instr, listing, via, via.String())
	require.NoError(t, err)
	return key
}

// looseKey creates a key of h that names nothing.
func (s *stack) looseKey(t *testing.T, h holder) uuid.UUID {
	t.Helper()
	key := db.NewID()
	_, err := s.tx.Exec(context.Background(), `INSERT INTO stated_keys (id, user_id, identifiers)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'isin', 'value', $3::text)))`, key, h.user, key.String())
	require.NoError(t, err)
	return key
}

// txn records that h bought quantity through key as at d.
func (s *stack) txn(t *testing.T, h holder, key uuid.UUID, d time.Time, quantity string) {
	t.Helper()
	_, err := s.q.CreateTxn(context.Background(), gen.CreateTxnParams{
		ID: db.NewID(), UserID: h.user, Broker: gen.BrokerIbkr, StmtID: h.stmt, StatedKeyID: key,
		OrderDate: d, SettlementDate: d, AsAt: d, Quantity: decimal.RequireFromString(quantity),
	})
	require.NoError(t, err)
}

// resolves records that the ticker value names instr at venue in currency,
// on every day, and returns the ticker row.
func (s *stack) resolves(t *testing.T, instr uuid.UUID, market, value, venue, currency string) gen.Ticker {
	t.Helper()
	row := dbtest.Ticker(t, s.q, types.MarketTicker{Market: market, Value: value})
	require.NoError(t, s.q.CreateResolvedTickers(context.Background(), gen.CreateResolvedTickersParams{
		TickerIds: []uuid.UUID{row.ID}, Markets: []string{market}, InstrIds: []uuid.UUID{instr},
		Venues: []string{venue}, ValidFroms: []string{""}, ValidBefores: []string{""}, Currencies: []string{currency},
	}))
	return row
}
