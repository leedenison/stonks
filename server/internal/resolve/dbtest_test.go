//go:build dbtest

package resolve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
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
	mics      mic.Table
	resolver  *Resolver
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	var err error
	q := gen.New(tx)
	s := &stack{q: q, tx: tx, script: scripted.New(), beta: scripted.New(), alphaFacts: scripted.NewIdentifierEvents(), betaFacts: scripted.NewIdentifierEvents(), alphaName: "alpha-" + uuid.NewString(), betaName: "beta-" + uuid.NewString()}
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
			return &scripted.Datasource{Identity: s.script, Facts: s.alphaFacts, Since: since}, nil
		}},
		s.betaName: market.Factory{New: func(market.Config) (market.Integration, error) {
			return &scripted.Datasource{Identity: s.beta, Facts: s.betaFacts, Since: since}, nil
		}},
	}
	s.sources, err = market.New(ctx, q, factories, log)
	require.NoError(t, err)
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log, market.WithClock(func() time.Time { return answered }, sleep))
	s.mics, err = mic.Load(ctx, q)
	require.NoError(t, err)
	families, err := currency.Load(ctx, q)
	require.NoError(t, err)
	s.resolver = New(db.New[Queries](tx), &serial{Fetcher: fetcher}, s.sources, s.mics, families, func() time.Time { return answered }, log)
	return s
}

// confirmedOn reports whether ivs confirm instrument on day d.
func confirmedOn(ivs []validity.Interval, instrument uuid.UUID, d time.Time) bool {
	for _, iv := range ivs {
		if iv.Instrument == instrument && iv.Validity == gen.ValidityConfirmed &&
			iv.From != nil && !d.Before(*iv.From) && iv.Before != nil && d.Before(*iv.Before) {
			return true
		}
	}
	return false
}

// answered is the moment every datasource of the stack answers, so the day an
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

// exists reports whether the instrument id exists.
func (s *stack) exists(t *testing.T, id uuid.UUID) bool {
	t.Helper()
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(), "SELECT count(*) FROM instruments WHERE id = $1", id).Scan(&n))
	return n == 1
}

// written writes a resolution key as its outcome and reason.
func written(rk gen.ResolutionKey) string {
	out := string(rk.Outcome)
	if len(rk.Reasons) > 0 {
		out += ": " + strings.Join(rk.Reasons, "; ")
	}
	return out
}

// TestResolveWrites checks what one resolution writes: a created instrument, a
// second key attaching to it, a cash key on the seed and a dropped set of
// identifiers.
func TestResolveWrites(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBX", Identifiers: []types.Identifier{figi, isin, comp, xlon}},
	}}
	trade := s.state(t, types.AssetClassStock, "GBX", isin, xlon)
	transfer := s.state(t, types.AssetClassEquity, "", isin)
	cash := s.state(t, types.AssetClassCash, "GBX", id(types.IdentifierTypeCurrency, "", "GBX"))
	wrong := s.state(t, types.AssetClassStock, "USD", cusip)
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi2, cusip, xetr}},
	}}

	got := s.resolve(t, trade, transfer, cash, wrong)
	outcomes := make([]string, len(got))
	for i, r := range got {
		outcomes[i] = written(r)
	}
	want := []string{"matched", "matched", "matched", "unrecognised: " + s.alphaName + ": 1 listing in 1 set of identifiers, 1 set of identifiers dropped"}
	if diff := cmp.Diff(want, outcomes); diff != "" {
		t.Errorf("outcomes mismatch (-want +got):\n%s", diff)
	}

	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeIsin, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument
	if inst.AssetClass != types.AssetClassStock || inst.FetchKeyID == nil {
		t.Errorf("instrument = %+v, want a stock with provenance", inst)
	}
	listings, err := s.q.ListListings(ctx, inst.ID)
	require.NoError(t, err)
	if len(listings) != 1 || listings[0].Currency != "GBP" || listings[0].FetchKeyID == nil {
		t.Fatalf("listings = %+v, want one GBP listing with provenance", listings)
	}
	ids, err := s.q.ListIdentifiers(ctx, inst.ID)
	require.NoError(t, err)
	var identifiers []string
	for _, row := range ids {
		where := "instrument"
		if row.ListingID != nil {
			where = "listing"
		}
		identifiers = append(identifiers, where+" "+to.Identifier(row).Name())
		if row.FetchKeyID == nil || *row.FetchKeyID != *inst.FetchKeyID {
			t.Errorf("identifier %s from fetch key %v, want the instrument's %s", row.Value, row.FetchKeyID, *inst.FetchKeyID)
		}
	}
	wantIdentifiers := []string{"instrument isin GB00BH4HKS39", "instrument openfigi_share_class BBG001S5XDT5", "listing openfigi_composite BBG000C6K6G9", "listing mic_ticker XLON:VOD"}
	if diff := cmp.Diff(wantIdentifiers, identifiers); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	asserted, err := s.q.ListFetchKeyAssertions(ctx, inst.FetchKeyID)
	require.NoError(t, err)
	if len(asserted) != 4 {
		t.Errorf("%d assertions, want the 4 the response named", len(asserted))
	}
	day := time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)
	for _, a := range asserted {
		if a.InstrumentID == nil || *a.InstrumentID != inst.ID || !a.AssertedOn.Equal(day) {
			t.Errorf("assertion = %+v, want the instrument on %s, the day of the answer", a, day.Format(time.DateOnly))
		}
	}
	vod := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	ivs, err := validity.Of(ctx, s.q, vod)
	require.NoError(t, err)
	if !confirmedOn(ivs, inst.ID, day) {
		t.Errorf("validity of %s = %+v, want the instrument confirmed on %s", vod.Name(), ivs, day.Format(time.DateOnly))
	}

	tradeKey, transferKey := s.key(t, trade.ID), s.key(t, transfer.ID)
	via := map[uuid.UUID]string{}
	for _, row := range ids {
		via[row.ID] = to.Identifier(row).Name()
	}
	if tradeKey.InstrumentID == nil || *tradeKey.InstrumentID != inst.ID || tradeKey.ListingID == nil || *tradeKey.ListingID != listings[0].ID || via[*tradeKey.ViaID] != "isin GB00BH4HKS39" {
		t.Errorf("trade = %+v, want the instrument's GBP listing via its ISIN, confirmed", tradeKey)
	}
	if transferKey.InstrumentID == nil || *transferKey.InstrumentID != inst.ID || transferKey.ListingID != nil || via[*transferKey.ViaID] != "isin GB00BH4HKS39" {
		t.Errorf("transfer = %+v, want the instrument via its ISIN, confirmed, with no listing", transferKey)
	}

	gbx, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBX"})
	require.NoError(t, err)
	cashKey := s.key(t, cash.ID)
	if cashKey.InstrumentID == nil || *cashKey.InstrumentID != gbx.Instrument.ID || cashKey.ViaID == nil || *cashKey.ViaID != gbx.Identifier.ID || cashKey.ListingID == nil {
		t.Errorf("cash = %+v, want the GBP seed via the GBX identifier", cashKey)
	}

	// One fetch served every key but cash, since every lookup precedes the
	// writes: the transfer's response attached to the instrument the trade
	// created, and the coverage row carries its fetch key, the later.
	children, err := s.q.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &s.resolution.ID, UserID: &s.user.ID})
	require.NoError(t, err)
	if len(children) != 1 || children[0].Kind != gen.RunKindFetch || children[0].State != gen.RunStateCompleted {
		t.Fatalf("ListChildRuns = %+v, want one completed fetch", children)
	}
	items, err := s.q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: children[0].ID})
	require.NoError(t, err)
	sent := map[uuid.UUID]gen.FetchKey{}
	for _, it := range items {
		sent[*it.FetchKey.StatedKeyID] = it.FetchKey
	}
	attached := func(id uuid.UUID) bool { return sent[id].InstrumentID != nil && *sent[id].InstrumentID == inst.ID }
	if len(sent) != 3 || !attached(trade.ID) || !attached(transfer.ID) || sent[wrong.ID].InstrumentID != nil {
		t.Errorf("fetch items = %+v, want the trade and the transfer attached to the instrument and the mis-stated key attached to nothing", sent)
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{inst.ID})
	require.NoError(t, err)
	if len(coverage) != 1 || coverage[0].Datasource != s.alphaName || coverage[0].FetchKeyID != sent[transfer.ID].ID {
		t.Errorf("coverage = %+v, want alpha through the transfer's fetch key", coverage)
	}

	findings, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	if len(findings) != 1 || findings[0].Finding.Kind != gen.FindingKindDropped || *findings[0].Finding.Step != gen.DropStepStated || *findings[0].Finding.StatedKeyID != wrong.ID || *findings[0].Finding.Detail != "cusip 92857W308: stated USD has no listing among GBP ("+s.alphaName+")" {
		t.Errorf("findings = %+v, want one stated drop against the mis-stated key", findings)
	}
	if _, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCusip, Value: cusip.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("FindIdentifier(cusip) error = %v, want not found: a dropped set of identifiers is not stored", err)
	}
	droppedKey := sent[wrong.ID].ID
	dropped, err := s.q.ListFetchKeyAssertions(ctx, &droppedKey)
	require.NoError(t, err)
	if len(dropped) != 0 {
		t.Errorf("assertions of the dropped answer = %+v, want none", dropped)
	}
}

// TestUnknownRows checks that an answer stating the identifier sent is
// unknown writes a negative assertion of it, on the day of the answer.
func TestUnknownRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "ZZZQ"}
	s.script.Responses[ticker] = market.IdentityResult{Unknown: true}
	key := s.state(t, types.AssetClassStock, "USD", ticker)
	s.resolve(t, key)

	fk := s.fetchKey(t, key.ID, s.alphaName)
	asserted, err := s.q.ListFetchKeyAssertions(ctx, &fk.ID)
	require.NoError(t, err)
	want := []gen.IdentifierAssertion{{
		ID: asserted[0].ID, FetchKeyID: &fk.ID, Type: ticker.Type, Domain: ticker.Domain, Value: ticker.Value,
		AssertedOn: time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC),
	}}
	if diff := cmp.Diff(want, asserted); diff != "" {
		t.Errorf("assertions mismatch (-want +got):\n%s", diff)
	}
}

// TestPrimaryVenueRows checks that a listing takes the primary venue the
// first run to learn one states, and keeps it.
func TestPrimaryVenueRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, isin, xnas}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{figi, isin, cusip, xnas}},
	}}
	cusip2 := id(types.IdentifierTypeCusip, "", "594918104")
	s.script.Responses[cusip2] = market.IdentityResult{Filtered: []types.Identifier{cusip2}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNYS", Identifiers: []types.Identifier{figi, isin, cusip2, xnas}},
	}}
	found := func(t *testing.T) gen.Listing {
		t.Helper()
		row, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeIsin, Value: isin.Value})
		require.NoError(t, err)
		listings, err := s.q.ListListings(ctx, row.Instrument.ID)
		require.NoError(t, err)
		require.Len(t, listings, 1)
		return listings[0]
	}
	s.resolve(t, s.state(t, types.AssetClassStock, "USD", isin))
	if l := found(t); l.PrimaryMic != nil {
		t.Errorf("listing after an answer stating no primary venue = %+v, want none", l)
	}
	s.resolve(t, s.state(t, types.AssetClassStock, "USD", cusip))
	if l := found(t); l.PrimaryMic == nil || *l.PrimaryMic != "XNAS" {
		t.Errorf("listing after an answer stating XNAS = %+v, want XNAS", l)
	}
	s.resolve(t, s.state(t, types.AssetClassStock, "USD", cusip2))
	if l := found(t); l.PrimaryMic == nil || *l.PrimaryMic != "XNAS" {
		t.Errorf("listing after a later answer stating XNYS = %+v, want XNAS kept", l)
	}
}

// TestMergeRows checks that a response identifying two instruments folds
// the later into the earlier, with every row that pointed at the loser
// relinked or moved.
func TestMergeRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{cusip, xnas}},
	}}
	first := s.state(t, types.AssetClassStock, "GBP", isin)
	second := s.state(t, types.AssetClassStock, "USD", cusip)
	s.resolve(t, first, second)
	a, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	b, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: cusip.Type, Value: cusip.Value})
	require.NoError(t, err)
	if a.Instrument.ID == b.Instrument.ID {
		t.Fatal("the two keys resolved to one instrument before the merge")
	}

	// The third key's response names both instruments, which is what brings
	// them together.
	sedol := id(types.IdentifierTypeSedol, "", "BH4HKS3")
	s.script.Responses[sedol] = market.IdentityResult{Filtered: []types.Identifier{sedol}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, cusip, sedol, xnas}},
	}}
	third := s.state(t, types.AssetClassStock, "USD", sedol)
	got := s.resolve(t, third)
	if got[0].Outcome != gen.ResolutionOutcomeMatched {
		reason := strings.Join(got[0].Reasons, "; ")
		findings, _ := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
		var details []string
		for _, f := range findings {
			details = append(details, string(f.Finding.Kind)+": "+*f.Finding.Detail)
		}
		t.Fatalf("Resolve = %s (%s), want matched; findings %v", got[0].Outcome, reason, details)
	}
	_, err = s.tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
	require.NoError(t, err, "the deferred constraints hold after the merge")

	survivor := a.Instrument.ID
	var instruments int
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT count(*) FROM instruments WHERE id = $1", b.Instrument.ID).Scan(&instruments))
	if instruments != 0 {
		t.Errorf("the later instrument %s still exists", b.Instrument.ID)
	}
	listings, err := s.q.ListListings(ctx, survivor)
	require.NoError(t, err)
	families := map[string]uuid.UUID{}
	for _, l := range listings {
		families[l.Currency] = l.ID
	}
	if len(families) != 2 || families["GBP"] == uuid.Nil || families["USD"] == uuid.Nil {
		t.Fatalf("survivor listings = %+v, want GBP and USD", listings)
	}
	ids, err := s.q.ListIdentifiers(ctx, survivor)
	require.NoError(t, err)
	where := map[string]string{}
	for _, row := range ids {
		listing := "instrument"
		if row.ListingID != nil {
			for fam, id := range families {
				if id == *row.ListingID {
					listing = fam
				}
			}
		}
		where[to.Identifier(row).Name()] = listing
	}
	wantWhere := map[string]string{
		"isin GB00BH4HKS39": "instrument", "openfigi_share_class BBG001S5XDT5": "instrument",
		"cusip 92857W308":     "instrument",
		"mic_ticker XLON:VOD": "GBP", "mic_ticker XNAS:VOD": "USD", "sedol BH4HKS3": "USD",
	}
	if diff := cmp.Diff(wantWhere, where); diff != "" {
		t.Errorf("survivor identifiers mismatch (-want +got):\n%s", diff)
	}
	for _, id := range []uuid.UUID{second.ID, third.ID} {
		k := s.key(t, id)
		if k.InstrumentID == nil || *k.InstrumentID != survivor || k.ListingID == nil || *k.ListingID != families["USD"] {
			t.Errorf("key %s = instrument %v listing %v, want the survivor's USD listing", id, k.InstrumentID, k.ListingID)
		}
	}
	var relinked int
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT count(*) FROM fetch_keys WHERE stated_key_id = $1 AND instrument_id = $2", second.ID, survivor).Scan(&relinked))
	if relinked != 1 {
		t.Errorf("%d fetch keys of the second key name the survivor, want 1", relinked)
	}
	naming := map[bool]int{}
	rows, err := s.tx.Query(ctx, "SELECT instrument_id = $1, count(*) FROM identifier_assertions WHERE instrument_id IN ($1, $2) GROUP BY 1", survivor, b.Instrument.ID)
	require.NoError(t, err)
	for rows.Next() {
		var isSurvivor bool
		var n int
		require.NoError(t, rows.Scan(&isSurvivor, &n))
		naming[isSurvivor] = n
	}
	require.NoError(t, rows.Err())
	if naming[false] != 0 || naming[true] == 0 {
		t.Errorf("assertions naming the survivor %d, the merged instrument %d, want every one on the survivor", naming[true], naming[false])
	}
	nasdaq := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "VOD"}
	ivs, err := validity.Of(ctx, s.q, nasdaq)
	require.NoError(t, err)
	for _, iv := range ivs {
		if iv.Instrument != survivor {
			t.Errorf("validity of %s = %+v, want only the survivor", nasdaq.Name(), ivs)
			break
		}
	}
	if len(ivs) == 0 {
		t.Errorf("validity of %s is empty, want the survivor", nasdaq.Name())
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{survivor, b.Instrument.ID})
	require.NoError(t, err)
	if len(coverage) != 1 || coverage[0].InstrumentID != survivor {
		t.Errorf("coverage = %+v, want one row on the survivor", coverage)
	}
	findings, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	if len(findings) != 1 || findings[0].Finding.Kind != gen.FindingKindMerged || *findings[0].Finding.StatedKeyID != third.ID || findings[0].Finding.FetchKeyID == nil {
		t.Errorf("findings = %+v, want one merged finding on the third key with its fetch key", findings)
	}
}

// TestDescribedRows checks resolution through a system owned broker
// description against a real database.
func TestDescribedRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	descr := id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORP")
	// alpha creates the instrument the description names, and a second
	// instrument that the description must not match.
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi2, cusip, xnas}},
	}}
	s.resolve(t, s.state(t, types.AssetClassStock, "GBP", isin), s.state(t, types.AssetClassStock, "USD", cusip))
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument
	listings, err := s.q.ListListings(ctx, inst.ID)
	require.NoError(t, err)
	require.Len(t, listings, 1)
	row, err := s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: inst.ID, Type: descr.Type, Domain: descr.Domain, Value: descr.Value})
	require.NoError(t, err)
	s.enableBeta(t)

	// via checks that key matched inst through row, on the GBP listing,
	// provisionally.
	via := func(t *testing.T, key gen.StatedKey) {
		t.Helper()
		if key.InstrumentID == nil || *key.InstrumentID != inst.ID || key.ViaID == nil || *key.ViaID != row.ID || key.ListingID == nil || *key.ListingID != listings[0].ID {
			t.Errorf("key = %+v, want the instrument's GBP listing via the description, provisional", key)
		}
	}

	t.Run("a description alone matches, and beta is asked and serves nothing", func(t *testing.T) {
		before := len(s.fetches(t))
		k := s.state(t, types.AssetClassStock, "GBP", descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
		if len(s.fetches(t)) != before+1 {
			t.Errorf("%d fetches, want one more: alpha covers the instrument and beta does not", len(s.fetches(t))-before)
		}
		if fk := s.fetchKey(t, k.ID, s.betaName); fk.Outcome != gen.FetchOutcomeNotServed {
			t.Errorf("beta's fetch key = %+v, want not served", fk)
		}
		ids, err := s.q.ListIdentifiers(ctx, inst.ID)
		require.NoError(t, err)
		var descriptions []uuid.UUID
		for _, i := range ids {
			if i.Type == types.IdentifierTypeBrokerDescription {
				descriptions = append(descriptions, i.ID)
			}
		}
		if len(descriptions) != 1 || descriptions[0] != row.ID {
			t.Errorf("description rows = %v, want the one inserted", descriptions)
		}
	})

	t.Run("a datasource answering an unknown ISIN with another instrument is a contradiction", func(t *testing.T) {
		other := id(types.IdentifierTypeIsin, "", "GB00B03MLX29")
		s.beta.Responses[other] = market.IdentityResult{Filtered: []types.Identifier{other}, Listings: []market.Listing{
			{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{id(types.IdentifierTypeOpenfigiShareClass, "", "BBG001S5XDT7"), other, id(types.IdentifierTypeMicTicker, "XLON", "SHEL")}},
		}}
		k := s.state(t, types.AssetClassStock, "GBP", other, descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, k.ID)); diff != "" {
			t.Errorf("findings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a description and an identifier naming different instruments", func(t *testing.T) {
		k := s.state(t, types.AssetClassStock, "USD", cusip, descr)
		got := s.resolve(t, k)
		want := "unrecognised: cusip 92857W308 and broker_description ibkr:ACME CORP name different instruments"
		if written(got[0]) != want {
			t.Errorf("outcome = %s, want %s", written(got[0]), want)
		}
		if key := s.key(t, k.ID); key.InstrumentID != nil {
			t.Errorf("key = %+v, want no association", key)
		}
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, k.ID)); diff != "" {
			t.Errorf("findings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a bare ticker beside the description is sent and the description associates", func(t *testing.T) {
		// beta served the instrument above and so covers it. Its coverage is
		// cleared so that it is asked again, and alpha's kept, since the
		// stack's one connection cannot carry two fetches at once.
		_, err := s.tx.Exec(ctx, "DELETE FROM identity_coverage WHERE instrument_id = $1 AND datasource = 'beta'", inst.ID)
		require.NoError(t, err)
		s.beta.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Listings: []market.Listing{
			{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, xlon}},
		}}
		k := s.state(t, types.AssetClassStock, "GBP", ticker, descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
		if fk := s.fetchKey(t, k.ID, s.betaName); fk.Outcome != gen.FetchOutcomeServed || fk.SentType == nil || *fk.SentType != types.IdentifierTypeMicTicker {
			t.Errorf("beta's fetch key = %+v, want served under the ticker", fk)
		}
	})

	t.Run("another broker's description of the same text names nothing", func(t *testing.T) {
		schwab := s.newStatement(t, gen.BrokerSchwab)
		k := s.stateUnder(t, schwab, types.AssetClassStock, "USD", id(types.IdentifierTypeBrokerDescription, "schwab", "ACME CORP"))
		got := s.resolve(t, k)
		want := "unrecognised: failed to match broker description"
		if written(got[0]) != want {
			t.Errorf("outcome = %s, want %s", written(got[0]), want)
		}
	})

	t.Run("two descriptions, one known", func(t *testing.T) {
		k := s.state(t, types.AssetClassStock, "GBP", descr, id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORPORATION"))
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		via(t, s.key(t, k.ID))
	})

	// beta serves the ISIN, so it covers the instrument. The subtests above
	// need it uncovered, so this runs last.
	t.Run("an identifier the instrument carries is stronger than the description", func(t *testing.T) {
		k := s.state(t, types.AssetClassStock, "GBP", isin, descr)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		key := s.key(t, k.ID)
		if key.ViaID == nil || *key.ViaID != found.Identifier.ID {
			t.Errorf("key = %+v, want the instrument via its ISIN, confirmed", key)
		}
	})

	t.Run("reference data is asked of no datasource", func(t *testing.T) {
		ref, err := s.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassEquity})
		require.NoError(t, err)
		listing, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: ref.ID, Currency: "GBP"})
		require.NoError(t, err)
		fund := id(types.IdentifierTypeBrokerDescription, "fidelity_uk", "ZZ FUND ACC")
		seeded, err := s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: ref.ID, Type: fund.Type, Domain: fund.Domain, Value: fund.Value})
		require.NoError(t, err)
		fidelity := s.newStatement(t, gen.BrokerFidelityUk)
		before := len(s.fetches(t))
		k := s.stateUnder(t, fidelity, types.AssetClassSecurity, "GBP", fund)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		key := s.key(t, k.ID)
		if key.InstrumentID == nil || *key.InstrumentID != ref.ID || *key.ViaID != seeded.ID || *key.ListingID != listing.ID {
			t.Errorf("key = %+v, want the reference instrument via its description, provisional", key)
		}
		if len(s.fetches(t)) != before {
			t.Errorf("%d new fetches, want none: reference data is covered by every datasource", len(s.fetches(t))-before)
		}
	})
}

// TestCurrencyRows checks the cash keys the seed rejects.
func TestCurrencyRows(t *testing.T) {
	s := newStack(t)
	usd := id(types.IdentifierTypeCurrency, "", "USD")
	tests := []struct {
		name  string
		class types.AssetClass
		code  string
		id    types.Identifier
		want  string
	}{
		{name: "no such currency", class: types.AssetClassCash, code: "ZZZ", id: id(types.IdentifierTypeCurrency, "", "ZZZ"), want: "rejected: no currency ZZZ"},
		{name: "a class the seed contradicts", class: types.AssetClassEquity, code: "USD", id: usd, want: "rejected: asset class equity contradicts the instrument's cash"},
		{name: "no listing in the stated family", class: types.AssetClassCash, code: "EUR", id: usd, want: "rejected: no listing of USD in EUR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := s.state(t, tc.class, tc.code, tc.id)
			got := s.resolve(t, k)
			if written(got[0]) != tc.want {
				t.Errorf("outcome = %s, want %s", written(got[0]), tc.want)
			}
			if key := s.key(t, k.ID); key.InstrumentID != nil {
				t.Errorf("key = %+v, want no association", key)
			}
		})
	}
}

// TestLimitedDatasource checks that a datasource listing only some of an
// instrument's listings never contradicts a stated currency, and that one
// listing every listing does. Alpha covers the instrument first, so only
// beta is asked the second key.
func TestLimitedDatasource(t *testing.T) {
	for _, limited := range []bool{false, true} {
		t.Run(fmt.Sprintf("limited %t", limited), func(t *testing.T) {
			s := newStack(t)
			ctx := context.Background()
			s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
				{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
			}}
			k := s.state(t, types.AssetClassStock, "GBP", isin)
			s.resolve(t, k)
			s.enableBeta(t)
			s.beta.Limited = limited
			s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
				{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, xnas}},
			}}
			// The key resolves again under a new run, which asks beta.
			s.rerun(t)
			if got := s.resolve(t, k); written(got[0]) != "matched" {
				t.Fatalf("outcome = %s, want matched", written(got[0]))
			}
			var want []gen.FindingKind
			if !limited {
				want = []gen.FindingKind{gen.FindingKindDropped}
			}
			if diff := cmp.Diff(want, s.findings(t, k.ID)); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
			key := s.key(t, k.ID)
			listings, err := s.q.ListListings(ctx, *key.InstrumentID)
			require.NoError(t, err)
			if n := 1 + btoi(limited); len(listings) != n {
				t.Errorf("listings = %+v, want %d", listings, n)
			}
		})
	}
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

// TestCoveredRows checks that a key the database names is requested only
// from the datasources that have not covered its instrument, and that their
// response fills what the instrument lacks without replacing what it has.
func TestCoveredRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.resolve(t, s.state(t, types.AssetClassStock, "GBP", isin))
	before, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	inst := before.Instrument
	s.enableBeta(t)
	s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, isin, xnas}},
	}}
	fetched := len(s.fetches(t))

	k := s.state(t, types.AssetClassStock, "USD", isin)
	got := s.resolve(t, k)
	if written(got[0]) != "matched" {
		t.Fatalf("outcome = %s, want matched", written(got[0]))
	}
	if n := len(s.fetches(t)) - fetched; n != 1 {
		t.Errorf("%d new fetches, want 1: alpha covers the instrument", n)
	}
	fk := s.fetchKey(t, k.ID, s.betaName)
	listings, err := s.q.ListListings(ctx, inst.ID)
	require.NoError(t, err)
	byFamily := map[string]gen.Listing{}
	for _, l := range listings {
		byFamily[l.Currency] = l
	}
	usd, ok := byFamily["USD"]
	if len(listings) != 2 || !ok || usd.FetchKeyID == nil || *usd.FetchKeyID != fk.ID {
		t.Fatalf("listings = %+v, want GBP and a USD listing from beta's fetch key", listings)
	}
	ids, err := s.q.ListIdentifiers(ctx, inst.ID)
	require.NoError(t, err)
	var names []string
	for _, row := range ids {
		names = append(names, to.Identifier(row).Name())
		if row.Type == isin.Type && row.ID != before.Identifier.ID {
			t.Errorf("the ISIN row is %s, want the one alpha wrote, %s", row.ID, before.Identifier.ID)
		}
	}
	want := []string{"isin GB00BH4HKS39", "openfigi_share_class BBG001S5XDT5", "mic_ticker XLON:VOD", "mic_ticker XNAS:VOD"}
	if diff := cmp.Diff(want, names); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	key := s.key(t, k.ID)
	if key.ListingID == nil || *key.ListingID != usd.ID || *key.ViaID != before.Identifier.ID {
		t.Errorf("key = %+v, want the USD listing via the ISIN, confirmed", key)
	}
	coverage, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{inst.ID})
	require.NoError(t, err)
	var sources []string
	for _, c := range coverage {
		sources = append(sources, c.Datasource)
	}
	slices.Sort(sources)
	if diff := cmp.Diff([]string{s.alphaName, s.betaName}, sources); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
}

// TestDroppedUncovered checks that when the resolver drops a datasource's
// answer, the datasource does not cover the instrument and is asked again,
// and that an answer with no listing covers it.
func TestDroppedUncovered(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	k := s.state(t, types.AssetClassStock, "GBP", isin)
	s.resolve(t, k)
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument.ID
	covered := func() []string {
		rows, err := s.q.ListIdentityCoverage(ctx, []uuid.UUID{inst})
		require.NoError(t, err)
		var out []string
		for _, c := range rows {
			out = append(out, c.Datasource)
		}
		return out
	}
	s.enableBeta(t)
	// The key resolves again under a new run each time, as a replay resolves
	// it, and each run's fetches are counted on their own.

	// beta's share class contradicts the instrument's, so its set of
	// identifiers is dropped and it is asked again on each resolution.
	s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi2, isin, xlon}},
	}}
	for n := range 2 {
		s.rerun(t)
		got := s.resolve(t, k)
		if written(got[0]) != "matched" {
			t.Fatalf("outcome = %s, want matched", written(got[0]))
		}
		if d := len(s.fetches(t)); d != 1 {
			t.Errorf("resolution %d: %d fetches, want 1: a dropped answer does not cover the instrument", n, d)
		}
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, k.ID)); diff != "" {
			t.Errorf("resolution %d: findings mismatch (-want +got):\n%s", n, diff)
		}
		if diff := cmp.Diff([]string{s.alphaName}, covered()); diff != "" {
			t.Errorf("resolution %d: coverage mismatch (-want +got):\n%s", n, diff)
		}
	}

	// An answer with no listing covers the instrument.
	s.beta.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}}
	s.rerun(t)
	s.resolve(t, k)
	if d := len(s.fetches(t)); d != 1 {
		t.Errorf("%d fetches, want 1: beta is asked once more", d)
	}
	if diff := cmp.Diff([]string{s.alphaName, s.betaName}, covered()); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
	s.rerun(t)
	s.resolve(t, k)
	if d := len(s.fetches(t)); d != 0 {
		t.Errorf("%d fetches, want none: an empty answer covers the instrument", d)
	}
}

// TestLookupRows checks the keys the lookup decides against what the
// database names: two stated identifiers naming different instruments, and
// a stated class disjoint from the instrument's.
func TestLookupRows(t *testing.T) {
	s := newStack(t)
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi2, cusip, xnas}},
	}}
	s.resolve(t, s.state(t, types.AssetClassStock, "GBP", isin), s.state(t, types.AssetClassStock, "USD", cusip))
	fetched := len(s.fetches(t))

	contradicted := s.state(t, types.AssetClassStock, "USD", isin, cusip)
	disjoint := s.state(t, types.AssetClassEtf, "GBP", isin)
	got := s.resolve(t, contradicted, disjoint)
	want := []string{
		"unrecognised: isin GB00BH4HKS39 and cusip 92857W308 name different instruments",
		"unrecognised: asset class etf contradicts the instrument's stock",
	}
	if diff := cmp.Diff(want, []string{written(got[0]), written(got[1])}); diff != "" {
		t.Errorf("outcomes mismatch (-want +got):\n%s", diff)
	}
	if n := len(s.fetches(t)) - fetched; n != 0 {
		t.Errorf("%d new fetches, want none: the lookup decided both keys", n)
	}
	if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, contradicted.ID)); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
}

// TestMergeAcrossCurrencies checks that two instruments carrying a ticker at
// one venue, on listings of different currency families, do not disagree,
// so a response naming both merges them.
func TestMergeAcrossCurrencies(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	usdLine := id(types.IdentifierTypeMicTicker, "XLON", "VODUSD")
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{cusip, usdLine}},
	}}
	s.resolve(t, s.state(t, types.AssetClassStock, "GBP", isin), s.state(t, types.AssetClassStock, "USD", cusip))
	a, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	b, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: cusip.Type, Value: cusip.Value})
	require.NoError(t, err)

	sedol := id(types.IdentifierTypeSedol, "", "BH4HKS3")
	s.script.Responses[sedol] = market.IdentityResult{Filtered: []types.Identifier{sedol}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, cusip, sedol}},
	}}
	k := s.state(t, types.AssetClassStock, "USD", sedol)
	got := s.resolve(t, k)
	if written(got[0]) != "matched" {
		t.Fatalf("outcome = %s, want matched", written(got[0]))
	}
	if s.exists(t, b.Instrument.ID) {
		t.Errorf("the later instrument %s still exists", b.Instrument.ID)
	}
	ids, err := s.q.ListIdentifiers(ctx, a.Instrument.ID)
	require.NoError(t, err)
	var tickers []string
	for _, row := range ids {
		if row.Type == types.IdentifierTypeMicTicker {
			tickers = append(tickers, to.Identifier(row).Name())
		}
	}
	if diff := cmp.Diff([]string{"mic_ticker XLON:VOD", "mic_ticker XLON:VODUSD"}, tickers); diff != "" {
		t.Errorf("survivor tickers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindMerged}, s.findings(t, k.ID)); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
}

// TestMergeRefused checks that where two instruments a response names
// disagree on an identifier, both survive, the key associates with the
// instrument its stated identifier names, and a contradiction is recorded.
func TestMergeRefused(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	other := id(types.IdentifierTypeIsin, "", "GB00BH4HKS40")
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	s.script.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{cusip, other, xnas}},
	}}
	k := s.state(t, types.AssetClassStock, "USD", cusip)
	s.resolve(t, s.state(t, types.AssetClassStock, "GBP", isin), k)
	a, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	b, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: cusip.Type, Value: cusip.Value})
	require.NoError(t, err)

	// alpha covers both instruments, so beta carries the response that names
	// both when the key stating the CUSIP resolves again under a new run.
	s.enableBeta(t)
	s.beta.Responses[cusip] = market.IdentityResult{Filtered: []types.Identifier{cusip}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, cusip, xnas}},
	}}
	s.rerun(t)
	got := s.resolve(t, k)
	if written(got[0]) != "matched" {
		t.Fatalf("outcome = %s, want matched", written(got[0]))
	}
	for _, inst := range []uuid.UUID{a.Instrument.ID, b.Instrument.ID} {
		if !s.exists(t, inst) {
			t.Errorf("instrument %s is gone, want both kept", inst)
		}
	}
	key := s.key(t, k.ID)
	if key.InstrumentID == nil || *key.InstrumentID != b.Instrument.ID || *key.ViaID != b.Identifier.ID {
		t.Errorf("key = %+v, want the instrument the stated CUSIP identifies, via it", key)
	}
	rows, err := s.q.ListRunFindings(ctx, []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	want := fmt.Sprintf("isin GB00BH4HKS39 identifies instrument %s and isin GB00BH4HKS40 identifies %s; not merged", a.Instrument.ID, b.Instrument.ID)
	var details []string
	for _, f := range rows {
		if *f.Finding.StatedKeyID == k.ID {
			details = append(details, string(f.Finding.Kind)+": "+*f.Finding.Detail)
		}
	}
	if diff := cmp.Diff([]string{"contradiction: " + want}, details); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
	ids, err := s.q.ListIdentifiers(ctx, b.Instrument.ID)
	require.NoError(t, err)
	for _, row := range ids {
		if row.Type == figi.Type {
			t.Errorf("wrote %s onto the CUSIP's instrument, which the other instrument carries", row.Value)
		}
	}
}

// TestConfirmRows checks what a confirmation writes against real rows: the
// pick's instrument, the user's association on the key alone, and what later
// resolutions and statements make of it.
func TestConfirmRows(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.script.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, comp, xlon}},
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi2, xnas}},
	}}
	key := s.state(t, types.AssetClassStock, "GBP", ticker, descr)
	later := s.newStatement(t, gen.BrokerIbkr)
	mate := s.stateUnder(t, later, types.AssetClassStock, "GBP", descr)
	require.NoError(t, s.q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: s.user.ID, Ids: []uuid.UUID{key.ID, mate.ID}, GroupIds: []uuid.UUID{key.ID, key.ID}}))

	offer, err := s.resolver.Candidates(ctx, s.resolution, key)
	require.NoError(t, err)
	wantOffer := Offer{Candidates: []Candidate{{
		Datasource: s.alphaName, Strongest: figi,
		Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{comp, xlon}}}},
	}}}
	if diff := cmp.Diff(wantOffer, offer); diff != "" {
		t.Errorf("Candidates() mismatch (-want +got):\n%s", diff)
	}
	if kinds := s.findings(t, key.ID); len(kinds) != 0 {
		t.Errorf("findings = %v, want none: under a bare ticker the USD set of identifiers is not offered, and the drop is routine", kinds)
	}
	if _, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeIsin, Value: isin.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("FindIdentifier(isin) error = %v, want not found: a listing writes no instrument", err)
	}

	rk, err := s.resolver.Confirm(ctx, s.resolution, key, Pick{Datasource: s.alphaName, Identifier: isin})
	require.NoError(t, err)
	if rk.Outcome != gen.ResolutionOutcomeMatched || rk.StatedKeyID != key.ID {
		t.Fatalf("Confirm() = %+v, want the key matched", rk)
	}
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeIsin, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument
	if inst.AssetClass != types.AssetClassStock || inst.FetchKeyID == nil {
		t.Errorf("instrument = %+v, want a stock with provenance", inst)
	}
	ids, err := s.q.ListIdentifiers(ctx, inst.ID)
	require.NoError(t, err)
	via := map[uuid.UUID]string{}
	for _, row := range ids {
		via[row.ID] = to.Identifier(row).Name()
	}
	if k := s.key(t, key.ID); k.InstrumentID == nil || *k.InstrumentID != inst.ID || k.ListingID == nil || via[*k.ViaID] != "mic_ticker XLON:VOD" || *k.Arbiter != gen.ArbiterUser || k.GroupID != nil {
		t.Errorf("key = %+v, want the instrument's GBP listing via its venue ticker, provisional, by the user, in no group", k)
	}
	if m := s.key(t, mate.ID); m.InstrumentID != nil || m.Arbiter != nil {
		t.Errorf("the mate = %+v, want it untouched: a choice associates the one key", m)
	}
	resolved, err := s.q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: s.resolution.ID, UserID: s.user.ID})
	require.NoError(t, err)
	if len(resolved) != 1 || resolved[0].StatedKeyID != key.ID || resolved[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Errorf("resolution keys = %+v, want the key matched alone", resolved)
	}
	if _, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeOpenfigiShareClass, Value: figi2.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("FindIdentifier(figi2) error = %v, want not found: the dropped set of identifiers is not stored", err)
	}

	// A statement that read the key before the confirmation would run its
	// own resolution of it. That resolution asks the datasource again. A
	// bare ticker associates only through the user's choice. The run records
	// the key unrecognised and leaves the user's association in place.
	s.rerun(t)
	got := s.resolve(t, key)
	if got[0].Outcome != gen.ResolutionOutcomeUnrecognised || got[0].Offered != 1 {
		t.Errorf("a later resolution = %s offering %d, want unrecognised with the one candidate offered", written(got[0]), got[0].Offered)
	}
	if k := s.key(t, key.ID); *k.Arbiter != gen.ArbiterUser || *k.InstrumentID != inst.ID {
		t.Errorf("key after a later resolution = %+v, want the user's association", k)
	}
	if n := len(s.fetches(t)); n != 1 {
		t.Errorf("%d fetches in a later resolution, want 1: the key is sent again", n)
	}

	// A later statement stating the key links to its row, with the
	// association.
	next := s.stateUnder(t, s.newStatement(t, gen.BrokerIbkr), types.AssetClassStock, "GBP", ticker, descr)
	if next.ID != key.ID || next.Arbiter == nil || *next.Arbiter != gen.ArbiterUser {
		t.Errorf("a later statement's key = %+v, want the confirmed key %s", next, key.ID)
	}

	// A pick the database already holds reuses the instrument.
	other := id(types.IdentifierTypeBrokerDescription, "ibkr", "OTHER CORP")
	apart := s.stateUnder(t, s.newStatement(t, gen.BrokerIbkr), types.AssetClassStock, "GBP", ticker, other)
	rk, err = s.resolver.Confirm(ctx, s.resolution, apart, Pick{Datasource: s.alphaName, Identifier: isin})
	require.NoError(t, err)
	if k := s.key(t, apart.ID); rk.Outcome != gen.ResolutionOutcomeMatched || k.InstrumentID == nil || *k.InstrumentID != inst.ID || *k.Arbiter != gen.ArbiterUser {
		t.Errorf("a second confirmation = %+v, %+v, want the same instrument by the user", rk, k)
	}
	if _, err := s.resolver.Confirm(ctx, s.resolution, apart, Pick{Datasource: s.alphaName, Identifier: cusip}); !errors.Is(err, ErrNoCandidate) {
		t.Errorf("Confirm() with an identifier no set of identifiers carries: error = %v, want ErrNoCandidate", err)
	}
}

// TestConfirmJoins checks that a confirmation lands on the instrument that a
// member of the candidate identifies, and adds the head's identifiers to it.
func TestConfirmJoins(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	other, err := s.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	_, err = s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: other.ID, Type: figi.Type, Domain: figi.Domain, Value: figi.Value})
	require.NoError(t, err)
	s.enableBeta(t)
	s.script.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{isin, xlon}},
	}}
	s.beta.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	key := s.state(t, types.AssetClassStock, "GBP", ticker)
	rk, err := s.resolver.Confirm(ctx, s.resolution, key, Pick{Datasource: s.alphaName, Identifier: isin})
	require.NoError(t, err)
	k := s.key(t, key.ID)
	if rk.Outcome != gen.ResolutionOutcomeMatched || k.InstrumentID == nil || *k.InstrumentID != other.ID || *k.Arbiter != gen.ArbiterUser {
		t.Fatalf("Confirm() = %+v, %+v, want the key matched by the user with the instrument that has the FIGI", rk, k)
	}
	ids, err := s.q.ListIdentifiers(ctx, other.ID)
	require.NoError(t, err)
	var got []string
	for _, row := range ids {
		got = append(got, to.Identifier(row).Name())
	}
	slices.Sort(got)
	want := []string{"isin GB00BH4HKS39", "mic_ticker XLON:VOD", figi.Name()}
	slices.Sort(want)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
}

// TestConfirmMerged checks that two datasources describing one security
// offer one candidate, and that confirming it writes the head as the winner
// and attaches the member.
func TestConfirmMerged(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.enableBeta(t)
	s.script.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, xlon}},
	}}
	s.beta.Responses[ticker] = market.IdentityResult{Filtered: []types.Identifier{ticker}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: []types.Identifier{figi, isin, xlon}},
	}}
	key := s.state(t, types.AssetClassStock, "GBP", ticker)

	offer, err := s.resolver.Candidates(ctx, s.resolution, key)
	require.NoError(t, err)
	wantOffer := Offer{Candidates: []Candidate{{
		Datasource: s.alphaName, Attached: []string{s.betaName}, Strongest: figi,
		Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}},
	}}}
	if diff := cmp.Diff(wantOffer, offer); diff != "" {
		t.Errorf("Candidates() mismatch (-want +got):\n%s", diff)
	}

	// The confirmation runs under a run of its own, so its fetch keys are
	// the only ones the run holds.
	s.resolution, err = s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &s.user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = s.resolver.Confirm(ctx, s.resolution, key, Pick{Datasource: s.alphaName, Identifier: figi})
	require.NoError(t, err)
	alpha, beta := s.fetchKey(t, key.ID, s.alphaName), s.fetchKey(t, key.ID, s.betaName)
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeIsin, Value: isin.Value})
	require.NoError(t, err)
	inst := found.Instrument
	if inst.FetchKeyID == nil || *inst.FetchKeyID != alpha.ID {
		t.Errorf("instrument provenance = %v, want alpha's fetch key %s", inst.FetchKeyID, alpha.ID)
	}
	ids, err := s.q.ListIdentifiers(ctx, inst.ID)
	require.NoError(t, err)
	for _, row := range ids {
		if row.Type == isin.Type && (row.FetchKeyID == nil || *row.FetchKeyID != beta.ID) {
			t.Errorf("ISIN provenance = %v, want beta's fetch key %s", row.FetchKeyID, beta.ID)
		}
	}
	for _, fk := range []gen.FetchKey{alpha, beta} {
		if fk.InstrumentID == nil || *fk.InstrumentID != inst.ID {
			t.Errorf("fetch key %s points at %v, want the instrument %s", fk.ID, fk.InstrumentID, inst.ID)
		}
	}
	if k := s.key(t, key.ID); k.InstrumentID == nil || *k.InstrumentID != inst.ID || *k.Arbiter != gen.ArbiterUser {
		t.Errorf("key = %+v, want the instrument by the user", k)
	}
}
