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

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func span(from, before time.Time) types.Period { return types.Period{From: from, Before: before} }

// at is an as traded price on day d.
func at(d time.Time, price string) market.DayPrice {
	return market.DayPrice{Day: d, Price: decimal.RequireFromString(price), AsAt: d}
}
