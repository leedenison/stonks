//go:build dbtest

package resolve

import (
	"context"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/validity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// stack is a resolver over a rolled-back transaction. It scripts two
// datasources: alpha, which is enabled, and beta, which a test enables with
// enableBeta.
type stack struct {
	q          *gen.Queries
	tx         pgx.Tx
	user       gen.User
	statement  gen.Run
	statements []gen.Run
	resolution gen.Run
	script     *scripted.Identity
	beta       *scripted.Identity
	// alphaFacts and betaFacts serve each datasource's identifier events.
	alphaFacts *scripted.IdentifierEvents
	betaFacts  *scripted.IdentifierEvents
	// alphaName and betaName are unique to the test.
	alphaName string
	betaName  string
	sources   *market.Registry
	mics      mic.MICMap
	resolver  *Resolver
	// extender is a resolver whose fetches skip the cache, as an extend
	// validity run's do.
	extender *Resolver
	// now is the moment every datasource of the stack answers. It starts at
	// answered.
	now time.Time
	// identityOnly names the datasources built without identifier events,
	// as of the next reload.
	identityOnly map[string]bool
	cache        *memCache
}

// memCache is a fetch cache in memory. It passes every read through to a
// live fetch until on is set, so a test that changes a datasource's answers
// between runs keeps live answers.
type memCache struct {
	on      bool
	entries map[string][]byte
}

func (c *memCache) Get(_ context.Context, keys []string) ([][]byte, error) {
	out := make([][]byte, len(keys))
	if c.on {
		for i, k := range keys {
			out[i] = c.entries[k]
		}
	}
	return out, nil
}

func (c *memCache) Set(_ context.Context, key string, value []byte) error {
	if c.on {
		c.entries[key] = value
	}
	return nil
}

func (c *memCache) Drop(_ context.Context, datasource string) error {
	for k := range c.entries {
		if strings.Contains(k, datasource) {
			delete(c.entries, k)
		}
	}
	return nil
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	var err error
	q := gen.New(tx)
	s := &stack{
		q: q, tx: tx, script: scripted.New(), beta: scripted.New(), alphaFacts: scripted.NewIdentifierEvents(), betaFacts: scripted.NewIdentifierEvents(),
		alphaName: "alpha-" + uuid.NewString(), betaName: "beta-" + uuid.NewString(), now: answered, identityOnly: map[string]bool{},
		cache: &memCache{entries: map[string][]byte{}},
	}
	s.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.statement = s.newStatement(t, gen.BrokerIbkr)
	s.resolution, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &s.statement.ID})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: s.alphaName, Enabled: true, Precedence: 10})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	factories := map[string]market.Factory{
		s.alphaName: market.Factory{New: func(market.Config) (market.Integration, error) {
			if s.identityOnly[s.alphaName] {
				return s.script, nil
			}
			return &scripted.Datasource{Identity: s.script, Facts: s.alphaFacts, Since: since}, nil
		}},
		s.betaName: market.Factory{New: func(market.Config) (market.Integration, error) {
			if s.identityOnly[s.betaName] {
				return s.beta, nil
			}
			return &scripted.Datasource{Identity: s.beta, Facts: s.betaFacts, Since: since}, nil
		}},
	}
	s.sources, err = market.New(ctx, q, factories, log)
	require.NoError(t, err)
	clock := func() time.Time { return s.now }
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log, market.WithClock(clock, sleep), market.WithCache(s.cache))
	s.mics, err = mic.Load(ctx, q)
	require.NoError(t, err)
	families, _, err := currency.Load(ctx, q)
	require.NoError(t, err)
	s.resolver = New(db.New[Queries](tx), &serial{Fetcher: fetcher}, s.sources, s.mics, families, clock, log)
	s.extender = New(db.New[Queries](tx), &serial{Fetcher: fetcher.Fresh()}, s.sources, s.mics, families, clock, log)
	return s
}

// confirmedOn reports whether ivs confirm instrument on day d.
// mt is the market ticker of v: a ticker at a US venue is in the market
// US, and one at any other venue tested is in the market of its venue.
func mt(v types.Identifier) types.MarketTicker {
	m := v.Domain
	if slices.Contains([]string{"XNAS", "XNGS", "XNYS", "ARCX", "BATS"}, m) {
		m = "US"
	}
	return types.MarketTicker{Market: m, Value: v.Value}
}

func confirmedOn(ivs []validity.Interval, instrument uuid.UUID, d time.Time) bool {
	for _, iv := range ivs {
		if iv.Instrument == instrument && iv.Validity == validity.Confirmed &&
			iv.From != nil && !d.Before(*iv.From) && iv.Before != nil && d.Before(*iv.Before) {
			return true
		}
	}
	return false
}

// answered is the moment the stack's datasources first answer, so the day an
// assertion holds is known.
var answered = time.Date(2026, time.March, 2, 15, 30, 0, 0, time.UTC)

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// serial runs the fetches of one request one at a time. The stack's queries
// share one transaction, whose connection serves one query at a time, so the
// datasources a request asks together cannot record their fetches at once.
type serial struct {
	Fetcher
	mu sync.Mutex
}

func (s *serial) Identity(ctx context.Context, parent gen.Run, e *market.Entry, keys []gen.StatedKey) ([]fetchKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Fetcher.Identity(ctx, parent, e, keys)
}

func (s *serial) TickerIdentity(ctx context.Context, parent gen.Run, e *market.Entry, keys []gen.StatedKey) ([]fetchKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Fetcher.TickerIdentity(ctx, parent, e, keys)
}

func (s *serial) IdentifierEvents(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.IdentifierEventRequest) ([]eventKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Fetcher.IdentifierEvents(ctx, parent, e, reqs)
}

// since is the first day the stack's datasources answer identifier events
// for.
var since = time.Date(2003, time.September, 10, 0, 0, 0, 0, time.UTC)

// newStatement records a statement run of broker and returns the run.
func (s *stack) newStatement(t *testing.T, broker gen.Broker) gen.Run {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = s.q.CreateStatement(ctx, gen.CreateStatementParams{ID: run.ID, UserID: s.user.ID, Broker: broker, OrderFrom: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC), OrderBefore: time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)})
	require.NoError(t, err)
	s.statements = append(s.statements, run)
	return run
}

// rerun starts a new resolution run under the stack's statement and makes
// it the stack's, so a key the stack resolved resolves again, as a replay
// resolves it.
func (s *stack) rerun(t *testing.T) {
	t.Helper()
	var err error
	s.resolution, err = s.q.CreateRun(context.Background(), gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &s.statement.ID})
	require.NoError(t, err)
}

// enableBeta enables beta with a lower precedence than alpha.
func (s *stack) enableBeta(t *testing.T) {
	t.Helper()
	ctx := context.Background()
	_, err := s.q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: s.betaName, Enabled: true, Precedence: 20})
	require.NoError(t, err)
	require.NoError(t, s.sources.Reload(ctx))
}

// state records a stated key under the stack's statement.
func (s *stack) state(t *testing.T, class types.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	t.Helper()
	return s.stateUnder(t, s.statement, class, currency, ids...)
}

// stateUnder records a stated key of the user, linked to statement. If the
// user already has the key, it returns that row.
func (s *stack) stateUnder(t *testing.T, statement gen.Run, class types.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	t.Helper()
	ctx := context.Background()
	arg := gen.UpsertStatedKeyParams{ID: db.NewID(), UserID: s.user.ID, Identifiers: ids}
	if class != "" {
		arg.AssetClass = &class
	}
	if currency != "" {
		arg.Currency = &currency
	}
	row, err := s.q.UpsertStatedKey(ctx, arg)
	require.NoError(t, err)
	_, err = s.q.CreateStatementKeys(ctx, []gen.CreateStatementKeysParams{{StatementID: statement.ID, UserID: s.user.ID, StatedKeyID: row.ID}})
	require.NoError(t, err)
	return row
}

func (s *stack) resolve(t *testing.T, keys ...gen.StatedKey) []gen.ResolutionKey {
	t.Helper()
	out, _, err := s.resolver.Resolve(context.Background(), s.resolution, undated(keys...))
	require.NoError(t, err)
	return out
}

func (s *stack) key(t *testing.T, id uuid.UUID) gen.StatedKey {
	t.Helper()
	for _, st := range s.statements {
		keys, err := s.q.ListStatedKeys(context.Background(), gen.ListStatedKeysParams{StatementID: st.ID, UserID: s.user.ID})
		require.NoError(t, err)
		for _, k := range keys {
			if k.ID == id {
				return k
			}
		}
	}
	t.Fatalf("no stated key %s", id)
	return gen.StatedKey{}
}

// fetches returns the fetch runs of the stack's resolution.
func (s *stack) fetches(t *testing.T) []gen.Run {
	t.Helper()
	children, err := s.q.ListChildRuns(context.Background(), gen.ListChildRunsParams{ParentID: &s.resolution.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	return children
}

// fetchKey returns the fetch key datasource wrote for key.
func (s *stack) fetchKey(t *testing.T, key uuid.UUID, datasource string) gen.FetchKey {
	t.Helper()
	ctx := context.Background()
	for _, child := range s.fetches(t) {
		fetch, err := s.q.GetFetch(ctx, gen.GetFetchParams{ID: child.ID, UserID: &s.user.ID})
		require.NoError(t, err)
		if fetch.Datasource != datasource {
			continue
		}
		items, err := s.q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: child.ID})
		require.NoError(t, err)
		for _, it := range items {
			if it.FetchKey.StatedKeyID != nil && *it.FetchKey.StatedKeyID == key {
				return it.FetchKey
			}
		}
	}
	t.Fatalf("no fetch key for %s from %s", key, datasource)
	return gen.FetchKey{}
}

// findings returns the kinds of the findings written against key.
func (s *stack) findings(t *testing.T, key uuid.UUID) []gen.FindingKind {
	t.Helper()
	rows, err := s.q.ListRunFindings(context.Background(), []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	var kinds []gen.FindingKind
	for _, f := range rows {
		if f.Finding.StatedKeyID != nil && *f.Finding.StatedKeyID == key {
			kinds = append(kinds, f.Finding.Kind)
		}
	}
	return kinds
}

// count runs a query that counts rows.
func (s *stack) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// exists reports whether the instrument id exists.
func (s *stack) exists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	return s.count(t, "SELECT count(*) FROM instruments WHERE id = $1", id) == 1
}

// written writes a resolution key as its outcome and reason.
func written(rk gen.ResolutionKey) string {
	out := string(rk.Outcome)
	if len(rk.Reasons) > 0 {
		out += ": " + strings.Join(rk.Reasons, "; ")
	}
	return out
}

// on is the UTC midnight of a day.
func on(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// heldOnXLON returns a key held through v, a ticker on XLON that alpha
// declines and beta resolves to l, with both datasources serving identity
// alone.
func (s *stack) heldOnXLON(t *testing.T, v types.Identifier, l market.Listing) gen.StatedKey {
	t.Helper()
	s.enableBeta(t)
	s.withoutEvents(t, s.alphaName, s.betaName)
	s.script.Declines = []string{"XLON"}
	s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{l}}
	key := s.state(t, types.AssetClassStock, "GBP", v)
	s.trade(t, key, on(2026, 1, 5))
	got := s.resolveOn(t, []time.Time{on(2026, 1, 5)}, key)
	require.Equal(t, gen.ResolutionOutcomeMatched, got[0].Outcome)
	require.True(t, datedThrough(s.key(t, key.ID), v, gen.ArbiterDatasource))
	return key
}
