//go:build dbtest

package replay

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	stmt "github.com/leedenison/stonks/server/internal/statement"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// stack is the replay and statement services over one rolled-back
// transaction. Datasource alpha is enabled and beta is not.
type stack struct {
	q     *gen.Queries
	user  gen.User
	admin gen.User
	alpha *scripted.Identity
	beta  *scripted.Identity
	// alphaName and betaName are unique to the test.
	alphaName  string
	betaName   string
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
	s := &stack{q: q, alpha: scripted.New(), beta: scripted.New(), alphaName: "alpha-" + uuid.NewString(), betaName: "beta-" + uuid.NewString()}
	s.user, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.admin, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleAdmin})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: s.alphaName, Enabled: true, Precedence: 10})
	require.NoError(t, err)
	_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: s.betaName, Enabled: false, Precedence: 20})
	require.NoError(t, err)
	log := slog.New(slog.DiscardHandler)
	factories := map[string]market.Factory{
		s.alphaName: market.Factory{New: func(market.Config) (market.Integration, error) {
			return &scripted.Datasource{Identity: s.alpha, Facts: scripted.NewIdentifierEvents(), Since: time.Date(2003, time.September, 10, 0, 0, 0, 0, time.UTC)}, nil
		}},
		s.betaName: market.Factory{New: func(market.Config) (market.Integration, error) { return s.beta, nil }},
	}
	s.sources, err = market.New(ctx, q, factories, log)
	require.NoError(t, err)
	runs := runtest.Runner{Q: q}
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runs, log)
	clock := func() time.Time { return time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC) }
	mics, err := mic.Load(ctx, q)
	require.NoError(t, err)
	families, _, err := currency.Load(ctx, q)
	require.NoError(t, err)
	resolver := resolve.New(db.New[resolve.Queries](tx), fetcher, s.sources, mics, families, clock, log)
	s.svc = New(db.New[Queries](tx), runs, resolver, nopCorpActs{}, s.sources, mics, log)
	s.statements = stmt.New(db.New[stmt.Queries](tx), runs, resolver, s.svc, nopCorpActs{}, families, clock, log)
	return s
}

// answer is a response naming the ISIN's instrument: a stock in USD.
var answer = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
	{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{isin}},
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
	got, err := s.q.GetRun(context.Background(), gen.GetRunParams{ID: id, UserID: &s.user.ID})
	require.NoError(t, err)
	return got
}

func (s *stack) children(t *testing.T, parent gen.Run) []gen.Run {
	t.Helper()
	rows, err := s.q.ListChildRuns(context.Background(), gen.ListChildRunsParams{ParentID: &parent.ID, UserID: &s.user.ID})
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
	_, groups := s.holdings(t)
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
	holdings, groups := s.holdings(t)
	var held bool
	for _, h := range holdings {
		held = held || (h.InstrumentID == *key.InstrumentID && h.Quantity.Equal(decimal.NewFromInt(10)))
	}
	if !held {
		t.Errorf("instrument holdings after the replay = %+v, want ten of the stock", holdings)
	}
	if len(groups) != 0 {
		t.Errorf("group holdings after the replay = %+v, want none", groups)
	}
	if _, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{}); !errors.Is(err, ErrEmpty) {
		t.Errorf("second replay of the unavailable keys: err = %v, want %v", err, ErrEmpty)
	}

	_, err = s.q.UpdateDatasource(ctx, gen.UpdateDatasourceParams{Name: s.betaName, Enabled: true})
	require.NoError(t, err)
	require.NoError(t, s.sources.Reload(ctx))
	s.beta.Responses[isin] = answer
	fetches := s.children(t, resolution)
	if _, err := s.svc.Start(ctx, s.admin.ID, fetches[0], Scope{Datasource: s.betaName}); !errors.Is(err, ErrKind) {
		t.Errorf("replay of a fetch run: err = %v, want %v", err, ErrKind)
	}
	if _, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{Datasource: "gamma"}); !errors.Is(err, ErrDisabled) {
		t.Errorf("replay over gamma: err = %v, want %v", err, ErrDisabled)
	}
	row, err = s.svc.Start(ctx, s.admin.ID, resolution, Scope{Datasource: s.betaName})
	require.NoError(t, err)
	second := s.run(t, row.ID)
	require.Equal(t, gen.RunStateCompleted, second.State)
	rep, err = s.q.GetReplay(ctx, row.ID)
	require.NoError(t, err)
	if rep.Replay.SourceID != resolution.ID || rep.Replay.Datasource == nil || *rep.Replay.Datasource != s.betaName {
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
	if _, err := s.svc.Start(ctx, s.admin.ID, statement, Scope{Datasource: s.betaName}); !errors.Is(err, ErrEmpty) {
		t.Errorf("second replay over beta: err = %v, want %v", err, ErrEmpty)
	}
}

// stateTicker ingests a statement of user over month of 2026, with one stock
// row whose key states ticker alone, and returns the statement run and the
// key. A statement replaces the user's transactions over its period, so each
// of a user's statements takes a month of its own.
func (s *stack) stateTicker(t *testing.T, user gen.User, month time.Month, ticker types.Identifier) (gen.Run, gen.StatedKey) {
	t.Helper()
	ctx := context.Background()
	usd := "USD"
	key := &typev1.StatedKey{
		Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: ticker.Domain, Value: ticker.Value}},
		AssetClass:  typev1.AssetClass_ASSET_CLASS_STOCK, Currency: &usd,
	}
	from := time.Date(2026, month, 1, 0, 0, 0, 0, time.UTC)
	on := from.AddDate(0, 0, 4).Format(time.DateOnly)
	msg := &statementv1.Statement{
		Broker: typev1.Broker_BROKER_IBKR, OrderFrom: from.Format(time.DateOnly), OrderBefore: from.AddDate(0, 1, 0).Format(time.DateOnly),
		Rows: []*statementv1.Row{{Key: key, OrderDate: on, SettlementDate: on, AsAt: on, Quantity: "10"}},
	}
	row, err := s.statements.Create(ctx, user.ID, msg)
	require.NoError(t, err)
	keys, err := s.q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: row.ID, UserID: user.ID})
	require.NoError(t, err)
	require.Len(t, keys, 1)
	return row, keys[0]
}

// replays returns the replay runs below parent, each with the keys its
// resolution resolved.
func (s *stack) replays(t *testing.T, parent gen.Run) map[uuid.UUID][]uuid.UUID {
	t.Helper()
	ctx := context.Background()
	rows, err := s.q.ListRunDescendants(ctx, &parent.ID)
	require.NoError(t, err)
	out := map[uuid.UUID][]uuid.UUID{}
	for _, r := range rows {
		if r.Run.Kind != gen.RunKindResolution {
			continue
		}
		items, err := s.q.ListResolutionItems(ctx, gen.ListResolutionItemsParams{RunID: r.Run.ID})
		require.NoError(t, err)
		for _, it := range items {
			out[*r.Run.ParentID] = append(out[*r.Run.ParentID], it.StatedKey.ID)
		}
	}
	return out
}

// TestAfterIdentifierFacts checks that a fact written beneath a user's
// fetch run replays that user's unresolved key beneath the fetch run, and
// another user's beneath one system child of it, whatever venue of the
// ticker's market the key states. A fact of a market written beneath a
// system run replays each user's keys stating a ticker of the market, at any
// of its venues, directly beneath that run. A key stating a ticker of
// another market and the keys the excepted run resolved are left out.
func TestAfterIdentifierFacts(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	other, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "ZZ" + uuid.NewString()[:6]}
	stated, mine := s.stateTicker(t, s.user, time.January, ticker)
	_, theirs := s.stateTicker(t, other, time.January, ticker)
	_, segment := s.stateTicker(t, other, time.February, types.Identifier{Type: ticker.Type, Domain: "XNGS", Value: "ZY" + uuid.NewString()[:6]})
	_, across := s.stateTicker(t, other, time.March, types.Identifier{Type: ticker.Type, Domain: "XNYS", Value: ticker.Value})
	_, elsewhere := s.stateTicker(t, other, time.April, types.Identifier{Type: ticker.Type, Domain: "XLON", Value: ticker.Value})

	parent, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	fetch, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	require.NoError(t, err)
	written := []resolve.WrittenIdentifierFacts{{Run: fetch, Tickers: []types.MarketTicker{{Market: "US", Value: ticker.Value}}}}
	s.svc.AfterIdentifierFacts(ctx, gen.Run{ID: db.NewID()}, written)

	tree, err := s.q.ListRunDescendants(ctx, &fetch.ID)
	require.NoError(t, err)
	var own, system *gen.Run
	for _, r := range tree {
		switch {
		case r.Run.Kind != gen.RunKindReplay || *r.Run.ParentID != fetch.ID:
		case r.Run.UserID == nil:
			system = &r.Run
		case *r.Run.UserID == s.user.ID:
			own = &r.Run
		}
	}
	if own == nil || system == nil {
		t.Fatalf("runs below the fetch = %+v, want the user's replay and a system run", tree)
	}
	replayed := s.replays(t, fetch)
	if got := replayed[own.ID]; len(got) != 1 || got[0] != mine.ID {
		t.Errorf("the user's replay resolved %v, want %s", got, mine.ID)
	}
	var theirsRun uuid.UUID
	for id, keys := range replayed {
		if id != own.ID {
			theirsRun = id
			if !sameKeys(keys, []uuid.UUID{theirs.ID, across.ID}) {
				t.Errorf("the other user's replay resolved %v, want %s and %s", keys, theirs.ID, across.ID)
			}
		}
	}
	other2, err := s.q.GetRun(ctx, gen.GetRunParams{ID: theirsRun, UserID: &other.ID})
	require.NoError(t, err)
	if other2.ParentID == nil || *other2.ParentID != system.ID {
		t.Errorf("the other user's replay = %+v, want it beneath the system run %s", other2, system.ID)
	}

	attestation, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestation, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	except, _ := s.resolution(t, stated)
	written = []resolve.WrittenIdentifierFacts{{Run: attestation, Markets: []string{"US"}}}
	s.svc.AfterIdentifierFacts(ctx, except, written)
	replayed = s.replays(t, attestation)
	var got []uuid.UUID
	for parent, keys := range replayed {
		run, err := s.q.GetRun(ctx, gen.GetRunParams{ID: parent, UserID: &other.ID})
		require.NoError(t, err, "a replay beneath the system run belongs to the other user")
		if run.ParentID == nil || *run.ParentID != attestation.ID {
			t.Errorf("replay %+v, want it directly beneath the system run", run)
		}
		got = append(got, keys...)
	}
	want := []uuid.UUID{theirs.ID, segment.ID, across.ID}
	if !sameKeys(got, want) {
		t.Errorf("keys replayed for the US market = %v, want %v: not %s of XLON, nor %s the excepted run resolved", got, want, elsewhere.ID, mine.ID)
	}
}

// sameKeys reports whether a and b hold the same keys in any order.
func sameKeys(a, b []uuid.UUID) bool {
	cmp := func(x, y uuid.UUID) int { return strings.Compare(x.String(), y.String()) }
	a, b = slices.Clone(a), slices.Clone(b)
	slices.SortFunc(a, cmp)
	slices.SortFunc(b, cmp)
	return slices.Equal(a, b)
}

// TestReplayChain checks that a replay whose resolution writes new facts
// replays in turn the keys that state their identifiers. The first user's replay asserts the
// ticker through its identity fetch, which replays the second user's key
// beneath that fetch. Both keys end dated, and the chain stops.
func TestReplayChain(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	other, err := s.q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "ZR" + uuid.NewString()[:6]}
	_, mine := s.stateTicker(t, s.user, time.January, ticker)
	_, theirs := s.stateTicker(t, other, time.January, ticker)
	share := types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG" + uuid.NewString()[:9]}
	s.alpha.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{{Type: ticker.Type, Value: ticker.Value}}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{share, ticker}},
	}}

	parent, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	fetch, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	require.NoError(t, err)
	written := []resolve.WrittenIdentifierFacts{{Run: fetch, Tickers: []types.MarketTicker{{Market: "US", Value: ticker.Value}}}}
	s.svc.AfterIdentifierFacts(ctx, gen.Run{ID: db.NewID()}, written)

	for _, k := range []struct {
		user gen.User
		key  gen.StatedKey
	}{{s.user, mine}, {other, theirs}} {
		got, err := s.q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: k.key.ID, UserID: k.user.ID})
		require.NoError(t, err)
		if got.ViaValue == nil || *got.ViaValue != ticker.Value {
			t.Errorf("key %s = %+v, want dated through %s", k.key.ID, got, ticker.Name())
		}
	}
	tree, err := s.q.ListRunDescendants(ctx, &fetch.ID)
	require.NoError(t, err)
	var own *gen.Run
	for _, r := range tree {
		if r.Run.Kind == gen.RunKindReplay && r.Run.UserID != nil && *r.Run.UserID == s.user.ID {
			own = &r.Run
		}
	}
	require.NotNil(t, own, "the user's replay beneath the fetch")
	below, err := s.q.ListRunDescendants(ctx, &own.ID)
	require.NoError(t, err)
	chained := false
	for _, r := range below {
		chained = chained || (r.Run.Kind == gen.RunKindReplay && r.Run.UserID != nil && *r.Run.UserID == other.ID)
	}
	if !chained {
		t.Errorf("runs beneath the user's replay = %+v, want the other user's replay that its facts started", below)
	}
}

// holding is one instrument's or one group's sum of the user's fixed and
// unresolved keys.
type holding struct {
	InstrumentID, GroupID uuid.UUID
	Quantity              decimal.Decimal
}

// holdings sums the user's keys by instrument and by group, as the holdings
// service does for keys with no dated association, leaving out zero sums.
func (s *stack) holdings(t *testing.T) (instruments, groups []holding) {
	t.Helper()
	keys, err := s.q.ListHoldingKeys(context.Background(), s.user.ID)
	require.NoError(t, err)
	byInstrument, byGroup := map[uuid.UUID]decimal.Decimal{}, map[uuid.UUID]decimal.Decimal{}
	for _, k := range keys {
		switch {
		case k.StatedKey.InstrumentID != nil:
			byInstrument[*k.StatedKey.InstrumentID] = byInstrument[*k.StatedKey.InstrumentID].Add(k.Quantity)
		case k.StatedKey.GroupID != nil && k.StatedKey.ViaType == nil:
			byGroup[*k.StatedKey.GroupID] = byGroup[*k.StatedKey.GroupID].Add(k.Quantity)
		}
	}
	for id, q := range byInstrument {
		if !q.IsZero() {
			instruments = append(instruments, holding{InstrumentID: id, Quantity: q})
		}
	}
	for id, q := range byGroup {
		if !q.IsZero() {
			groups = append(groups, holding{GroupID: id, Quantity: q})
		}
	}
	return instruments, groups
}

// nopCorpActs stands in for the corporate actions fetch. The
// refreshcorpacts package tests that fetch.
type nopCorpActs struct{}

func (nopCorpActs) FetchAfterIdentifierFacts(context.Context, gen.Run, []resolve.WrittenIdentifierFacts) {
}

func (nopCorpActs) AfterStatement(context.Context, gen.Run) {}
