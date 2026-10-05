//go:build dbtest

package replay

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/resolve"
	stmt "github.com/leedenison/stonks/server/internal/statement"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// stack is the replay and statement services over one rolled-back
// transaction. Datasource alpha is enabled and beta is not.
type stack struct {
	q          *gen.Queries
	user       gen.User
	admin      gen.User
	alpha      *scripted.Identity
	beta       *scripted.Identity
	sources    *market.Registry
	statements *stmt.Service
	svc        *Service
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	var err error
	q := gen.New(tx)
	s := &stack{q: q, alpha: scripted.New(), beta: scripted.New()}
	s.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.admin, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleAdmin})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: "alpha", Enabled: true, Precedence: 10})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: "beta", Enabled: false, Precedence: 20})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	factories := map[string]market.Factory{
		"alpha": func(market.Config) (market.Integration, error) { return s.alpha, nil },
		"beta":  func(market.Config) (market.Integration, error) { return s.beta, nil },
	}
	s.sources, err = market.New(ctx, q, factories, log)
	require.NoError(t, err)
	runs := runtest.Runner{Q: q}
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runs, log)
	resolver := resolve.New(db.New[resolve.Queries](tx), fetcher, s.sources, log)
	clock := func() time.Time { return time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC) }
	s.statements = stmt.New(db.New[stmt.Queries](tx), runs, resolver, clock)
	s.svc = New(db.New[Queries](tx), runs, resolver, s.sources)
	return s
}

// answer is a response naming the ISIN's instrument: a stock in USD.
var answer = market.IdentityResult{Filtered: []types.Identifier{isin}, Candidates: []market.Candidate{
	{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{isin}},
}}

// ingest ingests one statement, a stock bought with USD cash, and returns
// its completed run.
func (s *stack) ingest(t *testing.T) gen.Run {
	t.Helper()
	ctx := context.Background()
	usd := "USD"
	acme := &typev1.StatedKey{
		Identifiers: []*typev1.Identifier{
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: isin.Value},
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ACME CORP"},
		},
		AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK, Currency: &usd,
	}
	cash := &typev1.StatedKey{
		Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: usd}},
		AssetClass:  typev1.AssetClass_ASSET_CLASS_CASH, Currency: &usd,
	}
	msg := &statementv1.Statement{
		Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{
			{Key: acme, OrderDate: "2026-03-05", SettlementDate: "2026-03-05", AsAt: "2026-03-05", Quantity: "10"},
			{Key: cash, OrderDate: "2026-03-05", SettlementDate: "2026-03-05", AsAt: "2026-03-05", Quantity: "-1000"},
		},
	}
	row, err := s.statements.Create(ctx, s.user.ID, msg)
	require.NoError(t, err)
	return s.run(t, row.ID)
}

func (s *stack) run(t *testing.T, id uuid.UUID) gen.Run {
	t.Helper()
	got, err := s.q.GetRun(context.Background(), gen.GetRunParams{ID: id, UserID: s.user.ID})
	require.NoError(t, err)
	return got
}

func (s *stack) children(t *testing.T, parent gen.Run) []gen.Run {
	t.Helper()
	rows, err := s.q.ListChildRuns(context.Background(), gen.ListChildRunsParams{ParentID: &parent.ID, UserID: s.user.ID})
	require.NoError(t, err)
	return rows
}

// resolution returns the one resolution child of parent and its items.
func (s *stack) resolution(t *testing.T, parent gen.Run) (gen.Run, []gen.ListResolutionItemsRow) {
	t.Helper()
	children := s.children(t, parent)
	if len(children) != 1 || children[0].Kind != gen.RunKindResolution {
		t.Fatalf("children of %s = %+v, want one resolution", parent.ID, children)
	}
	items, err := s.q.ListResolutionItems(context.Background(), gen.ListResolutionItemsParams{RunID: children[0].ID})
	require.NoError(t, err)
	return children[0], items
}

// acme returns the stated key of the stock under statement.
func (s *stack) acme(t *testing.T, statement gen.Run) gen.StatedKey {
	t.Helper()
	keys, err := s.q.ListStatedKeys(context.Background(), gen.ListStatedKeysParams{StatementID: statement.ID, UserID: s.user.ID})
	require.NoError(t, err)
	for _, k := range keys {
		if slices.Contains(k.Identifiers, types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME CORP"}) {
			return k
		}
	}
	t.Fatal("no ACME CORP key")
	return gen.StatedKey{}
}

func outcomeOf(items []gen.ListResolutionItemsRow, key gen.StatedKey) gen.ResolutionOutcome {
	for _, it := range items {
		if it.StatedKey.ID == key.ID {
			return it.ResolutionKey.Outcome
		}
	}
	return ""
}

// TestReplay checks a replay against real rows. A statement ingested while
// the datasource is down leaves its stock key unavailable and grouped; a
// replay of the statement over the keys left unavailable, once the
// datasource answers, resolves the key, drops its group and moves the
// holding onto the instrument. A datasource enabled afterwards is then
// replayed over the replay's resolution, covering the instrument, while the
// cash key on reference data is left out. Each scope is refused once it
// selects nothing, as is a datasource not enabled and a run without keys.
func TestReplay(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.alpha.Down = true
	statement := s.ingest(t)
	require.Equal(t, gen.RunStateCompleted, statement.State)
	_, items := s.resolution(t, statement)
	key := s.acme(t, statement)
	if got := outcomeOf(items, key); got != gen.ResolutionOutcomeUnavailable {
		t.Fatalf("outcome while down = %s, want unavailable", got)
	}
	if key.InstrumentID != nil || key.GroupID == nil {
		t.Fatalf("key while down = %+v, want unresolved and grouped", key)
	}
	groups, err := s.q.ListGroupHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	if len(groups) != 1 {
		t.Fatalf("group holdings while down = %+v, want one", groups)
	}

	s.alpha.Down = false
	s.alpha.Responses[isin] = answer
	row, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{})
	require.NoError(t, err)
	replayed := s.run(t, row.ID)
	if replayed.Kind != gen.RunKindReplay || replayed.Trigger != gen.RunTriggerAdministrator || replayed.State != gen.RunStateCompleted {
		t.Errorf("replay run = %+v, want a completed replay started by an administrator", replayed)
	}
	rep, err := s.q.GetReplay(ctx, row.ID)
	require.NoError(t, err)
	if rep.Replay.SourceID != statement.ID || rep.Replay.Datasource != nil || rep.StartedByEmail != s.admin.Email {
		t.Errorf("replays row = %+v, want the statement, no datasource and the administrator", rep)
	}
	resolution, items := s.resolution(t, replayed)
	if len(items) != 1 || outcomeOf(items, key) != gen.ResolutionOutcomeMatched {
		t.Errorf("replay items = %+v, want the stock key matched and nothing else", items)
	}
	if fetches := s.children(t, resolution); len(fetches) != 1 || fetches[0].Kind != gen.RunKindFetch {
		t.Errorf("fetches of the replay = %+v, want one", fetches)
	}
	key = s.acme(t, statement)
	if key.InstrumentID == nil || key.GroupID != nil {
		t.Fatalf("key after the replay = %+v, want associated and ungrouped", key)
	}
	holdings, err := s.q.ListInstrumentHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	var held bool
	for _, h := range holdings {
		held = held || (h.InstrumentID == *key.InstrumentID && h.Quantity.Equal(decimal.NewFromInt(10)))
	}
	if !held {
		t.Errorf("instrument holdings after the replay = %+v, want ten of the stock", holdings)
	}
	groups, err = s.q.ListGroupHoldings(ctx, s.user.ID)
	require.NoError(t, err)
	if len(groups) != 0 {
		t.Errorf("group holdings after the replay = %+v, want none", groups)
	}
	if _, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{}); !errors.Is(err, ErrEmpty) {
		t.Errorf("second replay of the unavailable keys: err = %v, want %v", err, ErrEmpty)
	}

	_, err = s.q.UpdateDatasource(ctx, gen.UpdateDatasourceParams{Name: "beta", Enabled: true})
	require.NoError(t, err)
	require.NoError(t, s.sources.Reload(ctx))
	s.beta.Responses[isin] = answer
	fetches := s.children(t, resolution)
	if _, err := s.svc.Start(ctx, s.admin.ID, fetches[0], Scope{Datasource: "beta"}); !errors.Is(err, ErrKind) {
		t.Errorf("replay of a fetch run: err = %v, want %v", err, ErrKind)
	}
	if _, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{Datasource: "gamma"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("replay over gamma: err = %v, want %v", err, ErrDisabled)
	}
	row, err = s.svc.Start(ctx, s.admin.ID, resolution, Scope{Datasource: "beta"})
	require.NoError(t, err)
	second := s.run(t, row.ID)
	require.Equal(t, gen.RunStateCompleted, second.State)
	rep, err = s.q.GetReplay(ctx, row.ID)
	require.NoError(t, err)
	if rep.Replay.SourceID != resolution.ID || rep.Replay.Datasource == nil || *rep.Replay.Datasource != "beta" {
		t.Errorf("replays row = %+v, want the first replay's resolution and beta", rep)
	}
	resolution, items = s.resolution(t, second)
	if len(items) != 1 || outcomeOf(items, key) != gen.ResolutionOutcomeMatched {
		t.Errorf("beta replay items = %+v, want the stock key matched and nothing else", items)
	}
	if fetches := s.children(t, resolution); len(fetches) != 1 {
		t.Errorf("fetches of the beta replay = %+v, want one, alpha having covered the instrument", fetches)
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{*key.InstrumentID})
	require.NoError(t, err)
	if len(coverage) != 2 {
		t.Errorf("coverage after the beta replay = %+v, want alpha and beta", coverage)
	}
	if _, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{Datasource: "beta"}); !errors.Is(err, ErrEmpty) {
		t.Errorf("second replay over beta: err = %v, want %v", err, ErrEmpty)
	}
}
