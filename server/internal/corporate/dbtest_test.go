//go:build dbtest

package corporate

import (
	"context"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/runtest"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// answered is the moment the stack's datasources first answer.
var answered = time.Date(2026, time.March, 2, 15, 30, 0, 0, time.UTC)

// asked is the period every request of the stack asks for.
var asked = types.Period{From: day(2020, 1, 1), Before: day(2027, 1, 1)}

// stack is a Recorder over a rolled-back transaction, with two scripted
// datasources: alpha at precedence 10 and beta at 20.
type stack struct {
	q      *gen.Queries
	tx     pgx.Tx
	parent gen.Run
	alpha  *scripted.CorporateEvents
	beta   *scripted.CorporateEvents
	// alphaName and betaName are unique to the test.
	alphaName, betaName string
	sources             *market.Registry
	recorder            *Recorder
	// now is the moment every datasource answers.
	now time.Time
	// identityOnly names the datasources built without corporate events, as
	// of the next reload.
	identityOnly map[string]bool
	cache        *memCache
}

// memCache is a fetch cache in memory. It passes every read through to a
// live fetch, and stores nothing, until on is set.
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

// serial runs fetches one at a time. The stack's queries share one
// transaction, whose connection serves one query at a time.
type serial struct {
	Fetcher
	mu sync.Mutex
}

func (s *serial) CorporateEvents(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.CorporateEventRequest) ([]answerKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Fetcher.CorporateEvents(ctx, parent, e, reqs)
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	s := &stack{
		q: q, tx: tx, alpha: scripted.NewCorporateEvents(), beta: scripted.NewCorporateEvents(),
		alphaName: "alpha-" + uuid.NewString(), betaName: "beta-" + uuid.NewString(), now: answered,
		identityOnly: map[string]bool{}, cache: &memCache{entries: map[string][]byte{}},
	}
	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.parent, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	for name, precedence := range map[string]int32{s.alphaName: 10, s.betaName: 20} {
		_, err = q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: precedence})
		require.NoError(t, err)
	}
	factory := func(name string, i *scripted.CorporateEvents) market.Factory {
		return market.Factory{New: func(market.Config) (market.Integration, error) {
			if s.identityOnly[name] {
				return scripted.New(), nil
			}
			return i, nil
		}}
	}
	log := slog.New(slog.DiscardHandler)
	s.sources, err = market.New(ctx, q, map[string]market.Factory{s.alphaName: factory(s.alphaName, s.alpha), s.betaName: factory(s.betaName, s.beta)}, log)
	require.NoError(t, err)
	clock := func() time.Time { return s.now }
	sleep := func(context.Context, time.Duration) error { return nil }
	fetcher := market.NewFetcher(db.New[market.Queries](tx), runtest.Runner{Q: q}, log, market.WithClock(clock, sleep), market.WithCache(s.cache), market.WithRetry(time.Millisecond, 1))
	mics, err := mic.Load(ctx, q)
	require.NoError(t, err)
	s.recorder = New(db.New[Queries](tx), &serial{Fetcher: fetcher}, s.sources, mics, log)
	return s
}

// only leaves corporate events to the datasource named, as of now.
func (s *stack) only(t *testing.T, name string) {
	t.Helper()
	s.identityOnly = map[string]bool{s.alphaName: name != s.alphaName, s.betaName: name != s.betaName}
	require.NoError(t, s.sources.Reload(context.Background()))
}

func (s *stack) fetch(t *testing.T, tickers ...types.Identifier) {
	t.Helper()
	reqs := make([]market.CorporateEventRequest, len(tickers))
	for i, v := range tickers {
		reqs[i] = market.CorporateEventRequest{Ticker: v, Period: asked}
	}
	require.NoError(t, s.recorder.Fetch(context.Background(), s.parent, reqs))
}

// read returns the answers of mt over every day.
func (s *stack) read(t *testing.T, mt types.MarketTicker) []Answer {
	t.Helper()
	got, err := Read(context.Background(), s.q, []types.MarketTicker{mt}, always)
	require.NoError(t, err)
	return got[mt]
}

// found is a finding that names a corporate event of a ticker, with the
// datasource of its fetch run.
type found struct {
	id         uuid.UUID
	kind       gen.FindingKind
	event      uuid.UUID
	datasource string
	cleared    bool
}

func (s *stack) findings(t *testing.T, mt types.MarketTicker) []found {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `
		SELECT findings.id, findings.kind, findings.corporate_event_id, fetches.datasource, findings.cleared_at IS NOT NULL
		FROM findings
		JOIN corporate_events ON corporate_events.id = findings.corporate_event_id
		JOIN corporate_event_answers ON corporate_event_answers.id = corporate_events.answer_id
		JOIN fetches ON fetches.id = findings.run_id
		WHERE corporate_event_answers.market = $1 AND corporate_event_answers.value = $2
		ORDER BY findings.id`, mt.Market, mt.Value)
	require.NoError(t, err)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		return f, r.Scan(&f.id, &f.kind, &f.event, &f.datasource, &f.cleared)
	})
	require.NoError(t, err)
	return out
}

func ticker(venue, value string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value}
}

func us(value string) types.MarketTicker { return types.MarketTicker{Market: "US", Value: value} }

// reported is a handled event on ex of ratio r.
func reported(ex time.Time, r types.Ratio) market.CorporateEvent {
	return market.CorporateEvent{ExDate: ex, Ratio: r, Type: "forward_split", Handled: true}
}

// covering answers with events and coverage of asked.
func covering(events ...market.CorporateEvent) market.CorporateEventResult {
	return market.CorporateEventResult{Events: events, Coverage: &asked}
}

// stored is what a test compares of an answer's events.
func stored(as []Answer) map[string][]string {
	out := map[string][]string{}
	for _, a := range as {
		out[a.Datasource] = []string{}
		for _, e := range a.Events {
			out[a.Datasource] = append(out[a.Datasource], e.ExDate.Format(time.DateOnly)+" "+ratioText(e.Ratio))
		}
	}
	return out
}

// TestRecordReplaces checks that two events on one day are both stored, that
// coverage ends on the day of the answer, and that a later answer replaces an
// earlier one: an event it repeats keeps its row, one it drops goes, and an
// empty answer leaves none.
func TestRecordReplaces(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	nvda := ticker("XNAS", "NVDA")
	june := day(2024, 6, 10)
	s.alpha.Responses[nvda] = covering(reported(june, ratio(1, 10)), reported(june, ratio(1, 10)), reported(day(2021, 7, 20), ratio(1, 4)))
	s.fetch(t, nvda)
	first := s.read(t, us("NVDA"))
	want := map[string][]string{s.alphaName: {"2021-07-20 4 for 1", "2024-06-10 10 for 1", "2024-06-10 10 for 1"}}
	if diff := cmp.Diff(want, stored(first)); diff != "" {
		t.Errorf("first answer mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]types.Period{{From: day(2020, 1, 1), Before: day(2026, 3, 3)}}, first[0].Coverage); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}

	s.now = answered.AddDate(0, 0, 1)
	s.alpha.Responses[nvda] = covering(reported(june, ratio(1, 10)), reported(day(2025, 2, 3), ratio(1, 2)))
	s.fetch(t, nvda)
	second := s.read(t, us("NVDA"))
	want = map[string][]string{s.alphaName: {"2024-06-10 10 for 1", "2025-02-03 2 for 1"}}
	if diff := cmp.Diff(want, stored(second)); diff != "" {
		t.Errorf("second answer mismatch (-want +got):\n%s", diff)
	}
	kept := second[0].Events[0].ID
	if kept != first[0].Events[1].ID && kept != first[0].Events[2].ID {
		t.Errorf("repeated event id = %s, want one of the first answer's rows", kept)
	}
	if !second[0].Coverage[0].Before.Equal(day(2026, 3, 4)) {
		t.Errorf("coverage = %v, want it to end on the day of the second answer", second[0].Coverage)
	}

	s.now = answered.AddDate(0, 0, 2)
	s.alpha.Responses[nvda] = market.CorporateEventResult{}
	s.fetch(t, nvda)
	if got := s.read(t, us("NVDA")); len(got) != 0 {
		t.Errorf("answers after an empty answer = %+v, want none", got)
	}
	answers, err := s.q.ListCorporateAnswers(context.Background(), gen.ListCorporateAnswersParams{Markets: []string{"US"}, Values: []string{"NVDA"}})
	require.NoError(t, err)
	if len(answers) != 1 || answers[0].CorporateEventAnswer.CoveredFrom != nil {
		t.Errorf("stored answers = %+v, want alpha's empty answer", answers)
	}
}

// TestRecordCachedAnswer checks that a cached answer keeps the day of the
// call that filled the cache, and that it leaves a later stored answer in
// place.
func TestRecordCachedAnswer(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(reported(day(2021, 7, 20), ratio(1, 4)))
	s.cache.on = true
	s.fetch(t, nvda)

	s.now = answered.AddDate(0, 0, 5)
	s.fetch(t, nvda)
	got := s.read(t, us("NVDA"))
	if len(got) != 1 || !got[0].Coverage[0].Before.Equal(day(2026, 3, 3)) {
		t.Errorf("answers after a cache hit = %+v, want coverage to the day the cache was filled", got)
	}

	s.cache.on = false
	s.now = answered.AddDate(0, 0, 10)
	s.alpha.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	s.fetch(t, nvda)
	s.cache.on = true
	s.now = answered.AddDate(0, 0, 11)
	s.fetch(t, nvda)
	want := map[string][]string{s.alphaName: {"2024-06-10 10 for 1"}}
	if diff := cmp.Diff(want, stored(s.read(t, us("NVDA")))); diff != "" {
		t.Errorf("answer after an older cache hit mismatch (-want +got):\n%s", diff)
	}
}

// TestRecordKeepsAnotherDatasource checks that one datasource's later answer
// neither blocks nor loses to an older answer of another.
func TestRecordKeepsAnotherDatasource(t *testing.T) {
	s := newStack(t)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	s.beta.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	s.only(t, s.betaName)
	s.now = answered.AddDate(0, 0, 5)
	s.fetch(t, nvda)
	s.only(t, s.alphaName)
	s.now = answered
	s.fetch(t, nvda)
	got := s.read(t, us("NVDA"))
	want := map[string][]string{s.alphaName: {"2024-06-10 10 for 1"}, s.betaName: {"2024-06-10 10 for 1"}}
	if diff := cmp.Diff(want, stored(got)); diff != "" {
		t.Errorf("answers mismatch (-want +got):\n%s", diff)
	}
	if len(got) == 2 && (got[0].Datasource != s.alphaName || got[0].Precedence != 10) {
		t.Errorf("first answer = %s at %d, want alpha first by precedence", got[0].Datasource, got[0].Precedence)
	}
}

// TestUnhandledEvent checks that an unhandled event raises a finding that
// survives a refetch, cleared or not, and removes the coverage of every
// datasource on its ex-date until an answer drops it.
func TestUnhandledEvent(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	nvda := ticker("XNAS", "NVDA")
	spin := market.CorporateEvent{ExDate: day(2024, 6, 10), Ratio: ratio(1, 1), Type: "spin_off"}
	s.alpha.Responses[nvda] = covering(spin)
	s.beta.Responses[nvda] = covering()
	s.fetch(t, nvda)

	fs := s.findings(t, us("NVDA"))
	kinds := map[gen.FindingKind]string{}
	for _, f := range fs {
		kinds[f.kind] = f.datasource
	}
	wantKinds := map[gen.FindingKind]string{gen.FindingKindUnhandledCorporateEvent: s.alphaName}
	if diff := cmp.Diff(wantKinds, kinds); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
	split := []types.Period{{From: day(2020, 1, 1), Before: day(2024, 6, 10)}, {From: day(2024, 6, 11), Before: day(2026, 3, 3)}}
	for _, a := range s.read(t, us("NVDA")) {
		if diff := cmp.Diff(split, a.Coverage); diff != "" {
			t.Errorf("%s coverage mismatch (-want +got):\n%s", a.Datasource, diff)
		}
	}

	for _, f := range fs {
		_, err := s.q.ClearFinding(ctx, f.id)
		require.NoError(t, err)
	}
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, nvda)
	again := s.findings(t, us("NVDA"))
	if len(again) != 1 || !again[0].cleared {
		t.Errorf("findings after a repeat = %+v, want the one, still cleared", again)
	}

	s.alpha.Responses[nvda] = covering()
	s.fetch(t, nvda)
	if got := s.findings(t, us("NVDA")); len(got) != 0 {
		t.Errorf("findings after the event is dropped = %+v, want none", got)
	}
	whole := []types.Period{{From: day(2020, 1, 1), Before: day(2026, 3, 4)}}
	for _, a := range s.read(t, us("NVDA")) {
		if diff := cmp.Diff(whole, a.Coverage); diff != "" {
			t.Errorf("%s coverage after the event is dropped mismatch (-want +got):\n%s", a.Datasource, diff)
		}
	}
}

// TestNotServed checks that a failed or declined request, and a datasource
// without corporate events, leave the stored answer as it was.
func TestNotServed(t *testing.T) {
	s := newStack(t)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(market.CorporateEvent{ExDate: day(2024, 6, 10), Ratio: ratio(1, 1), Type: "spin_off"})
	s.only(t, s.alphaName)
	s.fetch(t, nvda)
	before, findings := s.read(t, us("NVDA")), s.findings(t, us("NVDA"))

	s.now = answered.AddDate(0, 0, 1)
	s.alpha.Down = true
	s.fetch(t, nvda)
	s.alpha.Down, s.alpha.Declines = false, []string{"XNAS"}
	s.fetch(t, nvda)
	s.only(t, s.betaName)
	s.fetch(t, nvda)
	if diff := cmp.Diff(before, s.read(t, us("NVDA"))); diff != "" {
		t.Errorf("answers mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(findings, s.findings(t, us("NVDA")), cmp.AllowUnexported(found{})); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
}

// TestOneAnswerPerMarket checks that the tickers of one market at two venues
// share one answer, and that a call asks about a ticker once per market.
func TestOneAnswerPerMarket(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	s.alpha.Responses[ticker("XNYS", "X")] = covering(reported(day(2021, 1, 4), ratio(1, 2)))
	s.alpha.Responses[ticker("XNAS", "X")] = covering(reported(day(2024, 6, 10), ratio(1, 3)))
	s.fetch(t, ticker("XNYS", "X"))
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, ticker("XNAS", "X"))
	want := map[string][]string{s.alphaName: {"2024-06-10 3 for 1"}}
	if diff := cmp.Diff(want, stored(s.read(t, us("X")))); diff != "" {
		t.Errorf("answers mismatch (-want +got):\n%s", diff)
	}
	s.alpha.Sent = nil
	s.fetch(t, ticker("XNYS", "X"), ticker("XNAS", "X"))
	if len(s.alpha.Sent) != 1 || s.alpha.Sent[0].Ticker != ticker("XNYS", "X") {
		t.Errorf("sent = %+v, want XNYS:X alone", s.alpha.Sent)
	}
}

// TestUnspelled checks that a ticker the system cannot spell is left out
// without failing the call.
func TestUnspelled(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	for _, v := range []types.Identifier{ticker("XNAS", "NVDA"), ticker("XNAS", "BRK/B"), ticker("ZZZZ", "AMD")} {
		s.alpha.Responses[v] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	}
	s.fetch(t, ticker("XNAS", "BRK/B"), ticker("ZZZZ", "AMD"), ticker("XNAS", "NVDA"))
	if got := s.read(t, us("NVDA")); len(got) != 1 {
		t.Errorf("answers of NVDA = %+v, want alpha's", got)
	}
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(),
		"SELECT count(*) FROM corporate_event_answers WHERE value IN ('BRK/B', 'AMD')").Scan(&n))
	if n != 0 {
		t.Errorf("%d answers stored for the unspelled tickers, want none", n)
	}
}

// TestRead checks that Read keeps the tickers asked for, and what each
// answer holds within the period.
func TestRead(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	s.alpha.Responses[ticker("XNAS", "A")] = covering(reported(day(2023, 3, 1), ratio(1, 2)), reported(day(2024, 6, 10), ratio(1, 3)))
	s.alpha.Responses[ticker("XLON", "B")] = covering(reported(day(2023, 3, 1), ratio(1, 2)))
	s.alpha.Responses[ticker("XNAS", "B")] = covering(reported(day(2023, 3, 1), ratio(1, 2)))
	early := types.Period{From: day(2020, 1, 1), Before: day(2022, 1, 1)}
	s.alpha.Responses[ticker("XNAS", "C")] = market.CorporateEventResult{Coverage: &early}
	s.fetch(t, ticker("XNAS", "A"), ticker("XLON", "B"), ticker("XNAS", "B"), ticker("XNAS", "C"))

	p := types.Period{From: day(2023, 1, 1), Before: day(2024, 6, 10)}
	got, err := Read(context.Background(), s.q, []types.MarketTicker{us("A"), {Market: "XLON", Value: "B"}, us("C")}, p)
	require.NoError(t, err)
	summary := map[string]map[string][]string{}
	for mt, as := range got {
		summary[mt.Name()] = stored(as)
		for _, a := range as {
			if diff := cmp.Diff([]types.Period{p}, a.Coverage); diff != "" {
				t.Errorf("%s coverage mismatch (-want +got):\n%s", mt.Name(), diff)
			}
		}
	}
	want := map[string]map[string][]string{
		"US:A":   {s.alphaName: {"2023-03-01 2 for 1"}},
		"XLON:B": {s.alphaName: {"2023-03-01 2 for 1"}},
	}
	if diff := cmp.Diff(want, summary); diff != "" {
		t.Errorf("Read mismatch (-want +got):\n%s", diff)
	}
}
