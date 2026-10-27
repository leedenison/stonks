//go:build dbtest

package attestidentifier

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// stack is a Service over a test transaction, with two instruments and an
// administrator.
type stack struct {
	tx    pgx.Tx
	q     *gen.Queries
	svc   *Service
	admin uuid.UUID
	a, b  uuid.UUID
	v     types.MarketTicker
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	mics, err := mic.Load(ctx, q)
	require.NoError(t, err)
	facts := NewMockFacts(gomock.NewController(t))
	facts.EXPECT().AfterIdentifierFacts(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes()
	s := &stack{tx: tx, q: q, v: types.MarketTicker{Market: "US", Value: "ZZA" + uuid.NewString()[:4]}}
	s.svc = New(db.New[Queries](tx), runtest.Runner{Q: q}, facts, Names{}, mics, func() time.Time { return today })
	admin, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleAdmin})
	require.NoError(t, err)
	s.admin = admin.ID
	for _, id := range []*uuid.UUID{&s.a, &s.b} {
		instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
		require.NoError(t, err)
		*id = instr.ID
	}
	return s
}

// attest attests that s.v names instr on day n, and returns the run.
func (s *stack) attest(t *testing.T, instr uuid.UUID, n int) gen.Run {
	t.Helper()
	row, err := s.svc.Start(context.Background(), s.admin, s.v, &instr, day(n), nil)
	require.NoError(t, err)
	return row
}

// relations returns the relation of s.v to each instrument, a then b. An
// instrument without a relation to the ticker reads empty.
func (s *stack) relations(t *testing.T) []string {
	t.Helper()
	out := make([]string, 2)
	for i, instr := range []uuid.UUID{s.a, s.b} {
		err := s.tx.QueryRow(context.Background(), `
			SELECT instr_tickers.relation::text FROM instr_tickers
			JOIN tickers ON tickers.id = instr_tickers.ticker_id
			WHERE tickers.market = $1 AND tickers.value = $2 AND instr_tickers.instr_id = $3`,
			s.v.Market, s.v.Value, instr).Scan(&out[i])
		if err != pgx.ErrNoRows {
			require.NoError(t, err)
		}
	}
	return out
}

// TestStartRebuilds checks that each identifier attestation rebuilds the
// names of its ticker. After b takes the ticker from a, an attestation that
// the ticker names a again ends b's row, and the ticker is defunct for b.
func TestStartRebuilds(t *testing.T) {
	s := newStack(t)
	s.attest(t, s.a, 1)
	if diff := cmp.Diff([]string{"current", ""}, s.relations(t)); diff != "" {
		t.Errorf("relations after a mismatch (-want +got):\n%s", diff)
	}
	s.attest(t, s.b, 5)
	if diff := cmp.Diff([]string{"defunct", "current"}, s.relations(t)); diff != "" {
		t.Errorf("relations after b mismatch (-want +got):\n%s", diff)
	}
	s.attest(t, s.a, 10)
	if diff := cmp.Diff([]string{"current", "defunct"}, s.relations(t)); diff != "" {
		t.Errorf("relations after a again mismatch (-want +got):\n%s", diff)
	}
}

// TestWithdrawRebuilds checks that withdrawing the attestation that gave the
// ticker back to a reopens b's row.
func TestWithdrawRebuilds(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.attest(t, s.a, 1)
	s.attest(t, s.b, 5)
	again := s.attest(t, s.a, 10)
	var id uuid.UUID
	require.NoError(t, s.tx.QueryRow(ctx, `SELECT id FROM identifier_assertions WHERE run_id = $1`, again.ID).Scan(&id))
	require.NoError(t, s.svc.Withdraw(ctx, id))
	if diff := cmp.Diff([]string{"defunct", "current"}, s.relations(t)); diff != "" {
		t.Errorf("relations after the withdrawal mismatch (-want +got):\n%s", diff)
	}
}
