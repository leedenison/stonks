//go:build dbtest

package rebuildtickervalidity

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// day is the nth day of March 2026.
func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

type stack struct {
	q   *gen.Queries
	tx  pgx.Tx
	svc *Service
}

// newStack returns a Service over a test transaction that rebuilds two
// tickers per batch.
func newStack(t *testing.T) *stack {
	t.Helper()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	return &stack{q: q, tx: tx, svc: New(db.New[Queries](tx), runtest.Runner{Q: q}, 2)}
}

func (s *stack) instr(t *testing.T) uuid.UUID {
	t.Helper()
	instr, err := s.q.CreateInstr(context.Background(), gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	return instr.ID
}

// ticker returns the row of a ticker at US unique to the test.
func (s *stack) ticker(t *testing.T) gen.Ticker {
	t.Helper()
	return dbtest.Ticker(t, s.q, types.MarketTicker{Market: "US", Value: "ZZB" + uuid.NewString()[:6]})
}

// rebuild runs a rebuild to completion.
func (s *stack) rebuild(t *testing.T) {
	t.Helper()
	row, err := s.svc.Start(context.Background())
	require.NoError(t, err)
	got, err := s.q.GetRunByID(context.Background(), row.ID)
	require.NoError(t, err)
	require.Equal(t, gen.RunStateCompleted, got.State)
}

// named returns the instrument each ticker's open row names, by ticker id.
func (s *stack) named(t *testing.T, instrs ...uuid.UUID) map[uuid.UUID]uuid.UUID {
	t.Helper()
	rows, err := s.q.ListResolvedTickers(context.Background(), instrs)
	require.NoError(t, err)
	out := map[uuid.UUID]uuid.UUID{}
	for _, r := range rows {
		if r.ValidBefore == nil {
			out[r.TickerID] = r.InstrID
		}
	}
	return out
}

// TestRebuildBatches checks that a run names the instrument of every ticker
// across more than one batch.
func TestRebuildBatches(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	admin, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleAdmin})
	require.NoError(t, err)
	attest, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	want := map[uuid.UUID]uuid.UUID{}
	var instrs []uuid.UUID
	for range 5 {
		tk, instr := s.ticker(t), s.instr(t)
		require.NoError(t, s.q.CreateIdentifierAttestation(ctx, gen.CreateIdentifierAttestationParams{
			ID: db.NewID(), RunID: attest.ID, AttestedBy: admin.ID, TickerID: tk.ID, Market: tk.Market, InstrID: &instr, AssertedFrom: day(1), AssertedBefore: day(1).AddDate(0, 0, 1),
		}))
		want[tk.ID] = instr
		instrs = append(instrs, instr)
	}
	s.rebuild(t)
	got := s.named(t, instrs...)
	for tk, instr := range want {
		if got[tk] != instr {
			t.Errorf("ticker %s names %s, want %s", tk, got[tk], instr)
		}
	}
}

// TestRebuildFollowsPrecedence checks that a reorder of the datasources
// leaves the stored ticker validity as it was, and that a run after it names
// the instrument that the datasource now first states.
func TestRebuildFollowsPrecedence(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	user, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	parent, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindResolveStmtKeys, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	tk := s.ticker(t)
	a, b := s.instr(t), s.instr(t)
	alpha, beta := "alpha-"+uuid.NewString(), "beta-"+uuid.NewString()
	asserts := func(name string, precedence int32, instr uuid.UUID) {
		_, err := s.q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: precedence})
		require.NoError(t, err)
		fetch, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
		require.NoError(t, err)
		_, err = s.q.CreateFetch(ctx, gen.CreateFetchParams{ID: fetch.ID, UserID: &user.ID, Datasource: name, Endpoint: "https://example.com", Kind: gen.FetchKindIdentity})
		require.NoError(t, err)
		key := db.NewID()
		sent := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: tk.Value}
		require.NoError(t, s.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: key, FetchID: fetch.ID, UserID: &user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
			SentType: &sent.Type, SentDomain: sent.Domain, SentValue: &sent.Value, AnsweredAt: ptr.To(day(1)),
		}))
		require.NoError(t, s.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), FetchKeyID: &key, TickerID: tk.ID, Market: tk.Market, InstrID: &instr, AssertedFrom: day(1), AssertedBefore: day(1).AddDate(0, 0, 1),
		}))
	}
	asserts(alpha, 1000, a)
	asserts(beta, 1001, b)
	on := func() uuid.UUID {
		t.Helper()
		stored, err := tickervalidity.Load(ctx, s.q, []types.MarketTicker{{Market: tk.Market, Value: tk.Value}})
		require.NoError(t, err)
		iv, ok := stored[types.MarketTicker{Market: tk.Market, Value: tk.Value}].On(day(1))
		require.True(t, ok, "the ticker has no stored ticker validity on day 1")
		return iv.Instr
	}
	s.rebuild(t)
	if got := s.named(t, a, b)[tk.ID]; got != a {
		t.Fatalf("before the reorder the ticker names %s, want %s", got, a)
	}
	if got := on(); got != a {
		t.Fatalf("before the reorder the stored ticker validity names %s, want %s", got, a)
	}
	require.NoError(t, s.q.SetDatasourcePrecedence(ctx, []string{beta, alpha}))
	if got := on(); got != a {
		t.Errorf("before the run the stored ticker validity names %s, want %s", got, a)
	}
	s.rebuild(t)
	if got := s.named(t, a, b)[tk.ID]; got != b {
		t.Errorf("after the reorder the ticker names %s, want %s", got, b)
	}
	if got := on(); got != b {
		t.Errorf("after the reorder the stored ticker validity names %s, want %s", got, b)
	}
}

// TestRebuildFollowsEventSource checks that a change of a market's
// identifier event source leaves the stored ticker validity as it was, and
// that a run after it counts only the new source's assertion coverage. The
// market is XLON, which no other test names as its own.
func TestRebuildFollowsEventSource(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	user, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	parent, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindResolveStmtKeys, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	alpha, beta := "alpha-"+uuid.NewString(), "beta-"+uuid.NewString()
	for i, name := range []string{alpha, beta} {
		_, err := s.q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: int32(1000 + i)})
		require.NoError(t, err)
	}
	dbtest.IdentifierEventSource(t, s.tx, "XLON", alpha)
	tk := dbtest.Ticker(t, s.q, types.MarketTicker{Market: "XLON", Value: "ZZL" + uuid.NewString()[:6]})
	instr := s.instr(t)
	fetch, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	require.NoError(t, err)
	_, err = s.q.CreateFetch(ctx, gen.CreateFetchParams{ID: fetch.ID, UserID: &user.ID, Datasource: alpha, Endpoint: "https://example.com", Kind: gen.FetchKindIdentifierFacts})
	require.NoError(t, err)
	key := db.NewID()
	sent := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: tk.Value}
	require.NoError(t, s.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: fetch.ID, UserID: &user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &sent.Type, SentDomain: sent.Domain, SentValue: &sent.Value, AnsweredAt: ptr.To(day(20)),
	}))
	require.NoError(t, s.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, TickerID: tk.ID, Market: tk.Market, InstrID: &instr, AssertedFrom: day(1), AssertedBefore: day(2),
	}))
	require.NoError(t, s.q.CreateIdentifierAssertionCoverage(ctx, gen.CreateIdentifierAssertionCoverageParams{
		ID: db.NewID(), FetchKeyID: key, Market: tk.Market, TickerID: tk.ID, CoveredFrom: day(1), CoveredBefore: day(20),
	}))
	confirmedOn := func(n int) bool {
		t.Helper()
		mt := types.MarketTicker{Market: tk.Market, Value: tk.Value}
		stored, err := tickervalidity.Load(ctx, s.q, []types.MarketTicker{mt})
		require.NoError(t, err)
		iv, ok := stored[mt].On(day(n))
		return ok && iv.Validity == tickervalidity.Confirmed
	}
	s.rebuild(t)
	if !confirmedOn(10) {
		t.Fatalf("day 10 is unconfirmed, want alpha's assertion coverage to confirm it")
	}
	dbtest.IdentifierEventSource(t, s.tx, "XLON", beta)
	if !confirmedOn(10) {
		t.Errorf("before the run day 10 is unconfirmed, want the stored ticker validity unchanged")
	}
	s.rebuild(t)
	if confirmedOn(10) || !confirmedOn(1) {
		t.Errorf("after the run day 10 confirmed = %v and day 1 confirmed = %v, want only the asserted day", confirmedOn(10), confirmedOn(1))
	}
}
