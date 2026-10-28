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
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
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

// TestOverlappingAttestations checks that each identifier attestation of a
// range is one assertion, that a later attestation replaces an earlier one on
// the days they share, and that withdrawing the later gives those days back to
// the earlier.
func TestOverlappingAttestations(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	to := day(10)
	_, err := s.svc.Start(ctx, s.admin, s.v, &s.a, day(1), &to)
	require.NoError(t, err)
	to = day(8)
	later, err := s.svc.Start(ctx, s.admin, s.v, &s.b, day(5), &to)
	require.NoError(t, err)

	rows, err := s.q.ListIdentifierAssertions(ctx, gen.ListIdentifierAssertionsParams{Markets: []string{s.v.Market}, Values: []string{s.v.Value}})
	require.NoError(t, err)
	var got []types.Period
	for _, r := range rows {
		got = append(got, types.Period{From: r.IdentifierAssertion.AssertedFrom, Before: r.IdentifierAssertion.AssertedBefore})
	}
	if diff := cmp.Diff([]types.Period{{From: day(1), Before: day(11)}, {From: day(5), Before: day(9)}}, got); diff != "" {
		t.Errorf("assertions mismatch (-want +got):\n%s", diff)
	}
	names := func() []uuid.UUID {
		ivs, err := tickervalidity.Of(ctx, s.q, s.v)
		require.NoError(t, err)
		var out []uuid.UUID
		for _, d := range []int{3, 6, 9} {
			for _, iv := range ivs {
				if iv.Contains(day(d)) && iv.Validity == tickervalidity.Confirmed {
					out = append(out, iv.Instr)
				}
			}
		}
		return out
	}
	if diff := cmp.Diff([]uuid.UUID{s.a, s.b, s.a}, names()); diff != "" {
		t.Errorf("confirmed instruments on days 3, 6 and 9 mismatch (-want +got):\n%s", diff)
	}

	var id uuid.UUID
	require.NoError(t, s.tx.QueryRow(ctx, `SELECT id FROM identifier_assertions WHERE run_id = $1`, later.ID).Scan(&id))
	require.NoError(t, s.svc.Withdraw(ctx, id))
	if diff := cmp.Diff([]uuid.UUID{s.a, s.a, s.a}, names()); diff != "" {
		t.Errorf("confirmed instruments after the withdrawal mismatch (-want +got):\n%s", diff)
	}
}
