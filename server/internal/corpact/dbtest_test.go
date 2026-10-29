//go:build dbtest

package corpact

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
// datasources: alpha at precedence 10 and beta at 20. Alpha is the
// corporate actions source of the US.
type stack struct {
	q      *gen.Queries
	tx     pgx.Tx
	parent gen.Run
	alpha  *scripted.CorpActs
	beta   *scripted.CorpActs
	// alphaName and betaName are unique to the test.
	alphaName, betaName string
	sources             *market.Registry
	recorder            *Recorder
	// now is the moment every datasource answers.
	now time.Time
	// identityOnly names the datasources built without corporate actions, as
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

func (s *serial) CorpActs(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.CorpActRequest) ([]answerKey, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Fetcher.CorpActs(ctx, parent, e, reqs)
}

func newStack(t *testing.T) *stack {
	t.Helper()
	ctx := context.Background()
	tx := dbtest.Begin(t, pool)
	q := gen.New(tx)
	s := &stack{
		q: q, tx: tx, alpha: scripted.NewCorpActs(), beta: scripted.NewCorpActs(),
		alphaName: "alpha-" + uuid.NewString(), betaName: "beta-" + uuid.NewString(), now: answered,
		identityOnly: map[string]bool{}, cache: &memCache{entries: map[string][]byte{}},
	}
	for name, precedence := range map[string]int32{s.alphaName: 10, s.betaName: 20} {
		_, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: name, Enabled: true, Precedence: precedence})
		require.NoError(t, err)
	}
	s.source(t, s.alphaName)
	user, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	s.parent, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	factory := func(name string, i *scripted.CorpActs) market.Factory {
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

// source makes the datasource named the corporate actions source of the US.
// The stack holds the lock on the US row from its first call.
func (s *stack) source(t *testing.T, name string) {
	t.Helper()
	dbtest.CorpActSource(t, s.tx, "US", name)
}

// withoutCorpActs reloads the registry with the datasource named built
// without the corporate actions capability.
func (s *stack) withoutCorpActs(t *testing.T, name string) {
	t.Helper()
	s.identityOnly[name] = true
	require.NoError(t, s.sources.Reload(context.Background()))
}

func (s *stack) fetch(t *testing.T, tickers ...types.Identifier) {
	t.Helper()
	reqs := make([]market.CorpActRequest, len(tickers))
	for i, v := range tickers {
		reqs[i] = market.CorpActRequest{Ticker: v, Period: asked}
	}
	require.NoError(t, s.recorder.Fetch(context.Background(), s.parent, reqs))
}

// read returns the answer of mt over every day, and reports false when Read
// leaves it out.
func (s *stack) read(t *testing.T, mt types.MarketTicker) (Answer, bool) {
	t.Helper()
	got, err := Read(context.Background(), s.q, []types.MarketTicker{mt}, always)
	require.NoError(t, err)
	a, ok := got[mt]
	return a, ok
}

// found is a finding that names a corporate action of a ticker, with the
// datasource that answered its datasource fetch run.
type found struct {
	id         uuid.UUID
	kind       gen.FindingKind
	act        uuid.UUID
	datasource string
	cleared    bool
}

func (s *stack) findings(t *testing.T, mt types.MarketTicker) []found {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `
		SELECT findings.id, findings.kind, findings.corp_act_id, fetches.datasource, findings.cleared_at IS NOT NULL
		FROM findings
		JOIN corp_acts ON corp_acts.id = findings.corp_act_id
		JOIN corp_act_answers ON corp_act_answers.id = corp_acts.answer_id
		JOIN tickers ON tickers.id = corp_act_answers.ticker_id
		JOIN fetches ON fetches.id = findings.run_id
		WHERE tickers.market = $1 AND tickers.value = $2
		ORDER BY findings.id`, mt.Market, mt.Value)
	require.NoError(t, err)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (found, error) {
		var f found
		return f, r.Scan(&f.id, &f.kind, &f.act, &f.datasource, &f.cleared)
	})
	require.NoError(t, err)
	return out
}

func ticker(venue, value string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value}
}

func us(value string) types.MarketTicker { return types.MarketTicker{Market: "US", Value: value} }

// reported is a handled action on ex of ratio r.
func reported(ex time.Time, r types.Ratio) market.CorpAct {
	return market.CorpAct{ExDate: ex, Ratio: r, Type: "forward_split", Handled: true}
}

// covering answers with actions and coverage of asked.
func covering(acts ...market.CorpAct) market.CorpActResult {
	return market.CorpActResult{Acts: acts, Coverage: &asked}
}

// stored is what a test compares of an answer's actions, by its datasource.
func stored(a Answer) map[string][]string {
	out := map[string][]string{a.Datasource: {}}
	for _, e := range a.Acts {
		out[a.Datasource] = append(out[a.Datasource], e.ExDate.Format(time.DateOnly)+" "+ratioText(e.Ratio))
	}
	return out
}

// TestRecordReplaces checks that two actions on one day are both stored, that
// coverage ends on the day of the answer, and that a later answer replaces an
// earlier one: an action it repeats keeps its row, one it drops goes, and an
// empty answer leaves none.
func TestRecordReplaces(t *testing.T) {
	s := newStack(t)
	nvda := ticker("XNAS", "NVDA")
	june := day(2024, 6, 10)
	s.alpha.Responses[nvda] = covering(reported(june, ratio(1, 10)), reported(june, ratio(1, 10)), reported(day(2021, 7, 20), ratio(1, 4)))
	s.fetch(t, nvda)
	first, _ := s.read(t, us("NVDA"))
	want := map[string][]string{s.alphaName: {"2021-07-20 4 for 1", "2024-06-10 10 for 1", "2024-06-10 10 for 1"}}
	if diff := cmp.Diff(want, stored(first)); diff != "" {
		t.Errorf("first answer mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]types.Period{{From: day(2020, 1, 1), Before: day(2026, 3, 3)}}, first.Coverage); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}

	s.now = answered.AddDate(0, 0, 1)
	s.alpha.Responses[nvda] = covering(reported(june, ratio(1, 10)), reported(day(2025, 2, 3), ratio(1, 2)))
	s.fetch(t, nvda)
	second, _ := s.read(t, us("NVDA"))
	want = map[string][]string{s.alphaName: {"2024-06-10 10 for 1", "2025-02-03 2 for 1"}}
	if diff := cmp.Diff(want, stored(second)); diff != "" {
		t.Errorf("second answer mismatch (-want +got):\n%s", diff)
	}
	kept := second.Acts[0].ID
	if kept != first.Acts[1].ID && kept != first.Acts[2].ID {
		t.Errorf("repeated action id = %s, want one of the first answer's rows", kept)
	}
	if !second.Coverage[0].Before.Equal(day(2026, 3, 4)) {
		t.Errorf("coverage = %v, want it to end on the day of the second answer", second.Coverage)
	}

	s.now = answered.AddDate(0, 0, 2)
	s.alpha.Responses[nvda] = market.CorpActResult{}
	s.fetch(t, nvda)
	if got, ok := s.read(t, us("NVDA")); ok {
		t.Errorf("answer after an empty answer = %+v, want none", got)
	}
	answers, err := s.q.ListCorpActAnswers(context.Background(), gen.ListCorpActAnswersParams{Markets: []string{"US"}, Values: []string{"NVDA"}})
	require.NoError(t, err)
	if len(answers) != 1 || answers[0].CorpActAnswer.CoveredFrom != nil {
		t.Errorf("stored answers = %+v, want alpha's empty answer", answers)
	}
}

// TestRecordCachedAnswer checks that a cached answer keeps the day of the
// call that filled the cache, and that it leaves a later stored answer in
// place.
func TestRecordCachedAnswer(t *testing.T) {
	s := newStack(t)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(reported(day(2021, 7, 20), ratio(1, 4)))
	s.cache.on = true
	s.fetch(t, nvda)

	s.now = answered.AddDate(0, 0, 5)
	s.fetch(t, nvda)
	if got, _ := s.read(t, us("NVDA")); len(got.Coverage) != 1 || !got.Coverage[0].Before.Equal(day(2026, 3, 3)) {
		t.Errorf("answer after a cache hit = %+v, want coverage to the day the cache was filled", got)
	}

	s.cache.on = false
	s.now = answered.AddDate(0, 0, 10)
	s.alpha.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	s.fetch(t, nvda)
	s.cache.on = true
	s.now = answered.AddDate(0, 0, 11)
	s.fetch(t, nvda)
	got, _ := s.read(t, us("NVDA"))
	want := map[string][]string{s.alphaName: {"2024-06-10 10 for 1"}}
	if diff := cmp.Diff(want, stored(got)); diff != "" {
		t.Errorf("answer after an older cache hit mismatch (-want +got):\n%s", diff)
	}
}

// TestFetchAsksSource checks that a fetch asks the corporate actions source of
// each request's market alone, and leaves out a request at a market without
// a source.
func TestFetchAsksSource(t *testing.T) {
	s := newStack(t)
	nvda, vod := ticker("XNAS", "NVDA"), ticker("XLON", "VOD")
	for _, v := range []types.Identifier{nvda, vod} {
		s.alpha.Responses[v] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
		s.beta.Responses[v] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	}
	s.fetch(t, nvda, vod)
	if diff := cmp.Diff([]types.Identifier{nvda}, sent(s.alpha.Sent)); diff != "" {
		t.Errorf("sent to alpha mismatch (-want +got):\n%s", diff)
	}
	if len(s.beta.Sent) != 0 {
		t.Errorf("sent to beta = %+v, want nothing", s.beta.Sent)
	}
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(),
		"SELECT count(*) FROM corp_act_answers JOIN tickers ON tickers.id = corp_act_answers.ticker_id WHERE tickers.market = 'XLON' AND tickers.value = 'VOD' AND corp_act_answers.datasource IN ($1, $2)",
		s.alphaName, s.betaName).Scan(&n))
	if n != 0 {
		t.Errorf("%d answers stored for XLON:VOD, want none", n)
	}
}

// TestSourceChange checks that Read passes over the stored answer of a
// datasource other than the market's source, and that a change of source
// changes what Read returns within the transaction.
func TestSourceChange(t *testing.T) {
	s := newStack(t)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	s.beta.Responses[nvda] = covering(reported(day(2021, 7, 20), ratio(1, 4)))
	s.source(t, s.betaName)
	s.fetch(t, nvda)
	s.source(t, s.alphaName)
	s.fetch(t, nvda)

	got, _ := s.read(t, us("NVDA"))
	if diff := cmp.Diff(map[string][]string{s.alphaName: {"2024-06-10 10 for 1"}}, stored(got)); diff != "" {
		t.Errorf("answer of alpha mismatch (-want +got):\n%s", diff)
	}
	s.source(t, s.betaName)
	got, _ = s.read(t, us("NVDA"))
	if diff := cmp.Diff(map[string][]string{s.betaName: {"2021-07-20 4 for 1"}}, stored(got)); diff != "" {
		t.Errorf("answer of beta mismatch (-want +got):\n%s", diff)
	}
}

// TestUnhandledCorpAct checks that an unhandled action raises a finding that
// survives a refetch and stays cleared. It also checks that the action's
// ex-date stays out of the coverage until an answer drops the action.
func TestUnhandledCorpAct(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	nvda := ticker("XNAS", "NVDA")
	spin := market.CorpAct{ExDate: day(2024, 6, 10), Ratio: ratio(1, 1), Type: "spin_off"}
	s.alpha.Responses[nvda] = covering(spin)
	s.fetch(t, nvda)

	fs := s.findings(t, us("NVDA"))
	if len(fs) != 1 || fs[0].kind != gen.FindingKindUnhandledCorpAct || fs[0].datasource != s.alphaName {
		t.Errorf("findings = %+v, want one unhandled corporate action finding on alpha's run", fs)
	}
	split := []types.Period{{From: day(2020, 1, 1), Before: day(2024, 6, 10)}, {From: day(2024, 6, 11), Before: day(2026, 3, 3)}}
	a, _ := s.read(t, us("NVDA"))
	if diff := cmp.Diff(split, a.Coverage); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
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
		t.Errorf("findings after the action is dropped = %+v, want none", got)
	}
	a, _ = s.read(t, us("NVDA"))
	if diff := cmp.Diff([]types.Period{{From: day(2020, 1, 1), Before: day(2026, 3, 4)}}, a.Coverage); diff != "" {
		t.Errorf("coverage after the action is dropped mismatch (-want +got):\n%s", diff)
	}
}

// TestPriceFactor checks that an action with a price factor ratio is stored
// unhandled and raises a finding, although its datasource handles the type.
func TestPriceFactor(t *testing.T) {
	s := newStack(t)
	ge := ticker("XNYS", "GE")
	s.alpha.Responses[ge] = covering(reported(day(2023, 1, 4), ratio(1000, 1281)))
	s.fetch(t, ge)

	a, _ := s.read(t, us("GE"))
	if len(a.Acts) != 1 || a.Acts[0].Handled {
		t.Errorf("answer = %+v, want one unhandled action", a)
	}
	fs := s.findings(t, us("GE"))
	if len(fs) != 1 || fs[0].kind != gen.FindingKindUnhandledCorpAct {
		t.Errorf("findings = %+v, want one unhandled corporate action finding", fs)
	}
}

// TestNotServed checks that a failed or declined request, and a source
// without corporate actions, leave the stored answer as it was.
func TestNotServed(t *testing.T) {
	s := newStack(t)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(market.CorpAct{ExDate: day(2024, 6, 10), Ratio: ratio(1, 1), Type: "spin_off"})
	s.fetch(t, nvda)
	before, _ := s.read(t, us("NVDA"))
	findings := s.findings(t, us("NVDA"))

	s.now = answered.AddDate(0, 0, 1)
	s.alpha.Responses[nvda] = covering()
	s.alpha.Down = true
	s.fetch(t, nvda)
	s.alpha.Down, s.alpha.Declines = false, []string{"XNAS"}
	s.fetch(t, nvda)
	s.alpha.Declines = nil
	s.withoutCorpActs(t, s.alphaName)
	s.fetch(t, nvda)
	after, _ := s.read(t, us("NVDA"))
	if diff := cmp.Diff(before, after); diff != "" {
		t.Errorf("answer mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(findings, s.findings(t, us("NVDA")), cmp.AllowUnexported(found{})); diff != "" {
		t.Errorf("findings mismatch (-want +got):\n%s", diff)
	}
}

// TestOneAnswerPerMarket checks that the tickers of one market at two venues
// share one answer, and that a call asks about a ticker once per market.
func TestOneAnswerPerMarket(t *testing.T) {
	s := newStack(t)
	s.alpha.Responses[ticker("XNYS", "X")] = covering(reported(day(2021, 1, 4), ratio(1, 2)))
	s.alpha.Responses[ticker("XNAS", "X")] = covering(reported(day(2024, 6, 10), ratio(1, 3)))
	s.fetch(t, ticker("XNYS", "X"))
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, ticker("XNAS", "X"))
	got, _ := s.read(t, us("X"))
	want := map[string][]string{s.alphaName: {"2024-06-10 3 for 1"}}
	if diff := cmp.Diff(want, stored(got)); diff != "" {
		t.Errorf("answer mismatch (-want +got):\n%s", diff)
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
	for _, v := range []types.Identifier{ticker("XNAS", "NVDA"), ticker("XNAS", "BRK/B"), ticker("ZZZZ", "AMD")} {
		s.alpha.Responses[v] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	}
	s.fetch(t, ticker("XNAS", "BRK/B"), ticker("ZZZZ", "AMD"), ticker("XNAS", "NVDA"))
	if _, ok := s.read(t, us("NVDA")); !ok {
		t.Error("NVDA has no answer, want alpha's")
	}
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(),
		"SELECT count(*) FROM corp_act_answers JOIN tickers ON tickers.id = corp_act_answers.ticker_id WHERE tickers.value IN ('BRK/B', 'AMD')").Scan(&n))
	if n != 0 {
		t.Errorf("%d answers stored for the unspelled tickers, want none", n)
	}
}

// TestRead checks that Read keeps the tickers asked for, and what each
// answer holds within the period.
func TestRead(t *testing.T) {
	s := newStack(t)
	s.alpha.Responses[ticker("XNAS", "A")] = covering(reported(day(2023, 3, 1), ratio(1, 2)), reported(day(2024, 6, 10), ratio(1, 3)))
	s.alpha.Responses[ticker("XNAS", "B")] = covering(reported(day(2023, 3, 1), ratio(1, 2)))
	early := types.Period{From: day(2020, 1, 1), Before: day(2022, 1, 1)}
	s.alpha.Responses[ticker("XNAS", "C")] = market.CorpActResult{Coverage: &early}
	s.fetch(t, ticker("XNAS", "A"), ticker("XNAS", "B"), ticker("XNAS", "C"))

	p := types.Period{From: day(2023, 1, 1), Before: day(2024, 6, 10)}
	got, err := Read(context.Background(), s.q, []types.MarketTicker{us("A"), us("C")}, p)
	require.NoError(t, err)
	summary := map[string]map[string][]string{}
	for mt, a := range got {
		summary[mt.Name()] = stored(a)
		if diff := cmp.Diff([]types.Period{p}, a.Coverage); diff != "" {
			t.Errorf("%s coverage mismatch (-want +got):\n%s", mt.Name(), diff)
		}
	}
	want := map[string]map[string][]string{"US:A": {s.alphaName: {"2023-03-01 2 for 1"}}}
	if diff := cmp.Diff(want, summary); diff != "" {
		t.Errorf("Read mismatch (-want +got):\n%s", diff)
	}
}

// sent returns the tickers of reqs.
func sent(reqs []market.CorpActRequest) []types.Identifier {
	out := make([]types.Identifier, len(reqs))
	for i, q := range reqs {
		out[i] = q.Ticker
	}
	return out
}
