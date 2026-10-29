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
	"github.com/leedenison/stonks/server/internal/identity/resolve"
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
	// replays holds every replay the Service asked for, in order.
	replays []replay
}

// replay is one set of written facts that the Service passed to
// Facts.AfterIdentifierFacts.
type replay struct {
	// Except is the run the replay sits beneath.
	Except uuid.UUID
	// Run is the run that wrote the facts.
	Run     uuid.UUID
	Tickers []types.MarketTicker
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	mics, err := mic.Load(ctx, q)
	require.NoError(t, err)
	s := &stack{tx: tx, q: q, v: types.MarketTicker{Market: "US", Value: "ZZA" + uuid.NewString()[:4]}}
	facts := NewMockFacts(gomock.NewController(t))
	facts.EXPECT().AfterIdentifierFacts(gomock.Any(), gomock.Any(), gomock.Any()).AnyTimes().Do(func(_ context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts) {
		for _, w := range written {
			s.replays = append(s.replays, replay{Except: except.ID, Run: w.Run.ID, Tickers: w.Tickers})
		}
	})
	s.svc = New(db.New[Queries](tx), runtest.Runner{Q: q}, facts, mics, func() time.Time { return today })
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

// replayed checks that the Service asked to replay s.v once beneath each of
// runs, in order, and forgets what it asked.
func (s *stack) replayed(t *testing.T, runs ...gen.Run) {
	t.Helper()
	var want []replay
	for _, r := range runs {
		want = append(want, replay{Except: r.ID, Run: r.ID, Tickers: []types.MarketTicker{s.v}})
	}
	if diff := cmp.Diff(want, s.replays); diff != "" {
		t.Errorf("replays mismatch (-want +got):\n%s", diff)
	}
	s.replays = nil
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
	first := s.attest(t, s.a, 1)
	s.replayed(t, first)
	if diff := cmp.Diff([]string{"current", ""}, s.relations(t)); diff != "" {
		t.Errorf("relations after a mismatch (-want +got):\n%s", diff)
	}
	s.replayed(t, s.attest(t, s.b, 5))
	if diff := cmp.Diff([]string{"defunct", "current"}, s.relations(t)); diff != "" {
		t.Errorf("relations after b mismatch (-want +got):\n%s", diff)
	}
	s.replayed(t, s.attest(t, s.a, 10))
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
	s.replays = nil
	var id uuid.UUID
	require.NoError(t, s.tx.QueryRow(ctx, `SELECT id FROM identifier_assertions WHERE run_id = $1`, again.ID).Scan(&id))
	require.NoError(t, s.svc.Withdraw(ctx, id))
	s.replayed(t, again)
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
		stored, err := tickervalidity.Load(ctx, s.q, []types.MarketTicker{s.v})
		require.NoError(t, err)
		ivs := stored[s.v]
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

// TestStartNothing checks that attesting that the ticker names nothing on one
// day writes one assertion of that day with no instrument, attested by the
// administrator under the run.
func TestStartNothing(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	row, err := s.svc.Start(ctx, s.admin, s.v, nil, day(10), nil)
	require.NoError(t, err)
	s.replayed(t, row)
	rows, err := s.q.ListIdentifierAssertions(ctx, gen.ListIdentifierAssertionsParams{Markets: []string{s.v.Market}, Values: []string{s.v.Value}})
	require.NoError(t, err)
	if len(rows) != 1 {
		t.Fatalf("assertions = %+v, want one", rows)
	}
	got := rows[0].IdentifierAssertion
	if got.InstrID != nil || !got.AssertedFrom.Equal(day(10)) || !got.AssertedBefore.Equal(day(11)) || got.RunID == nil || *got.RunID != row.ID || got.AttestedBy == nil || *got.AttestedBy != s.admin {
		t.Errorf("assertion = %+v, want day 10 alone naming nothing, attested by %s under run %s", got, s.admin, row.ID)
	}
}
