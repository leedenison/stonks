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
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
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
	run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &f.user.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser})
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

// TestQueries checks that a venue's tickers list under its operating MIC, that
// a key without a transaction and a ticker without a venue are left out, and
// that a ticker's transactions come from every venue of its domain.
func TestQueries(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	value := "ACME" + uuid.NewString()[:8]
	segment := f.key(t, "XNGS", value, 4)
	nasdaq := f.key(t, "XNAS", value, 6, 6)
	f.key(t, "XLON", value, 8)
	f.key(t, "XNAS", "BETA"+uuid.NewString()[:8])
	f.bareKey(t, value)

	domains, err := f.q.ListTickerDomains(ctx)
	require.NoError(t, err)
	wantDomains := []gen.ListTickerDomainsRow{{Domain: "XLON", Tickers: 1}, {Domain: "XNAS", Tickers: 1}}
	if diff := cmp.Diff(wantDomains, domains); diff != "" {
		t.Errorf("ListTickerDomains mismatch (-want +got):\n%s", diff)
	}

	values, err := f.q.ListDomainTickers(ctx, "XNAS")
	require.NoError(t, err)
	if diff := cmp.Diff([]string{value}, values); diff != "" {
		t.Errorf("ListDomainTickers mismatch (-want +got):\n%s", diff)
	}

	txs, err := f.q.ListTickerTransactions(ctx, gen.ListTickerTransactionsParams{Domain: "XNAS", Value: value})
	require.NoError(t, err)
	wantTxs := []gen.ListTickerTransactionsRow{
		{StatedKeyID: segment, OrderDate: day(4)},
		{StatedKeyID: nasdaq, OrderDate: day(6)},
		{StatedKeyID: nasdaq, OrderDate: day(6)},
	}
	if diff := cmp.Diff(wantTxs, txs); diff != "" {
		t.Errorf("ListTickerTransactions mismatch (-want +got):\n%s", diff)
	}
}
