package market

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// start is the time the harness's clock reads before it sleeps.
var start = time.Date(2026, time.October, 1, 0, 0, 0, 0, time.UTC)

// harness is a Fetcher whose clock advances only when it sleeps, with its
// writes and sleeps recorded.
type harness struct {
	store   *MockStore
	entry   *Entry
	fetcher *Fetcher
	parent  gen.Run
	cache   *memCache

	keys   []gen.CreateFetchKeyParams
	blocks []gen.CreateDatasourceBlockParams
	child  gen.Run
	now    time.Time
	sleeps []time.Duration
	// created is what each block write reports it inserted.
	created int64
	// keyErr fails every fetch key write.
	keyErr error
	// txRuns is how many times Tx runs its closure, as it does when the
	// database refuses an attempt. Zero runs it once.
	txRuns int
	// onSleep runs as the clock sleeps.
	onSleep func()
}

func newHarness(t *testing.T, integration Integration, open []gen.DatasourceBlock) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	h := &harness{
		store:   NewMockStore(ctrl),
		parent:  gen.Run{ID: db.NewID(), UserID: ptr.To(db.NewID()), Kind: gen.RunKindResolveStmtKeys},
		cache:   &memCache{entries: map[string][]byte{}},
		now:     start,
		created: 1,
	}
	h.entry = &Entry{Name: "fake", Integration: integration, limiter: newLimiter(rate.Inf, 1)}

	runs := NewMockRunner(ctrl)
	runs.EXPECT().Child(gomock.Any(), gomock.Any(), gen.RunKindDatasourceFetch, gomock.Any()).
		DoAndReturn(func(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
			h.child = gen.Run{ID: db.NewID(), UserID: parent.UserID, Kind: kind}
			return h.child, work(ctx, h.child)
		}).AnyTimes()

	h.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error {
		for range max(h.txRuns, 1) - 1 {
			if err := fn(h.store); err != nil {
				return err
			}
		}
		return fn(h.store)
	}).AnyTimes()
	h.store.EXPECT().CreateFetch(gomock.Any(), gomock.Any()).Return(gen.Fetch{}, nil).AnyTimes()
	h.store.EXPECT().ListOpenBlocks(gomock.Any(), gomock.Any()).Return(open, nil).AnyTimes()
	h.store.EXPECT().CreateFetchKey(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, arg gen.CreateFetchKeyParams) error {
			if h.keyErr != nil {
				return h.keyErr
			}
			h.keys = append(h.keys, arg)
			return nil
		}).AnyTimes()
	h.store.EXPECT().CreateDatasourceBlock(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, arg gen.CreateDatasourceBlockParams) (int64, error) {
			h.blocks = append(h.blocks, arg)
			return h.created, nil
		}).AnyTimes()

	clock := func() time.Time { return h.now }
	sleep := func(ctx context.Context, d time.Duration) error {
		h.sleeps = append(h.sleeps, d)
		h.now = h.now.Add(d)
		if h.onSleep != nil {
			h.onSleep()
		}
		return ctx.Err()
	}
	h.fetcher = NewFetcher(h.store, runs, discard(), WithRetry(time.Millisecond, 3), WithClock(clock, sleep), WithCache(h.cache))
	return h
}

// memCache is a Cache in memory, so a hit reads what a miss stored.
type memCache struct {
	entries map[string][]byte
	getErr  error
	setErr  error
}

func (c *memCache) Get(_ context.Context, keys []string) ([][]byte, error) {
	if c.getErr != nil {
		return nil, c.getErr
	}
	out := make([][]byte, len(keys))
	for i, k := range keys {
		out[i] = c.entries[k]
	}
	return out, nil
}

func (c *memCache) Set(_ context.Context, key string, value []byte) error {
	if c.setErr != nil {
		return c.setErr
	}
	c.entries[key] = value
	return nil
}

func (c *memCache) Drop(_ context.Context, datasource string) error {
	for k := range c.entries {
		if strings.HasPrefix(k, dropPrefix(datasource)) {
			delete(c.entries, k)
		}
	}
	return nil
}

// parameterised is a fake whose call takes params from every key.
type parameterised struct {
	*fake
	params []string
}

func (p *parameterised) Params(gen.StatedKey) []string { return p.params }

func isin(value string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeIsin, Value: value}
}

// keyOf returns a key and the identifier the fake sends for it.
func keyOf(f *fake, value string) gen.StatedKey {
	k := gen.StatedKey{ID: db.NewID()}
	if f.serves == nil {
		f.serves = map[string]types.Identifier{}
	}
	f.serves[k.ID.String()] = isin(value)
	return k
}

// unservedKey returns a key that the fake leaves unserved.
func unservedKey() gen.StatedKey { return gen.StatedKey{ID: db.NewID()} }

func (h *harness) run(t *testing.T, keys ...gen.StatedKey) []FetchKey[gen.StatedKey, IdentityResult] {
	t.Helper()
	fetchKeys, err := fetch(context.Background(), h.fetcher, h.parent, h.entry, identityKind, keys)
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	for i, r := range fetchKeys {
		if r.Source != h.entry.Name {
			t.Errorf("fetch key %d names source %q, want %q", i, r.Source, h.entry.Name)
		}
	}
	return fetchKeys
}

func (h *harness) key(t *testing.T, id uuid.UUID) gen.CreateFetchKeyParams {
	t.Helper()
	for _, k := range h.keys {
		if k.StatedKeyID != nil && *k.StatedKeyID == id {
			return k
		}
	}
	t.Fatalf("no fetch key written for stated key %s", id)
	return gen.CreateFetchKeyParams{}
}

// TestFetchNotServed checks that when the integration declines a key, the key
// costs no call and leaves no identifier sent.
func TestFetchNotServed(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	k := unservedKey()

	fetchKeys := h.run(t, k)
	if fetchKeys[0].Outcome != gen.FetchOutcomeNotServed {
		t.Errorf("outcome = %s, want not_served", fetchKeys[0].Outcome)
	}
	if f.calls != 0 {
		t.Errorf("calls = %d, want 0: a key the integration declines spends no quota", f.calls)
	}
	row := h.key(t, k.ID)
	if row.SentType != nil || row.Attempts != 0 || row.Reason == nil {
		t.Errorf("row = %+v, want no identifier sent, no attempt and a reason", row)
	}
}

// TestFetchServed checks that one call serves every key of the batch.
func TestFetchServed(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

	fetchKeys := h.run(t, one, two)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: the batch is one request", f.calls)
	}
	if len(f.sent[0]) != 2 {
		t.Errorf("sent %d identifiers, want 2", len(f.sent[0]))
	}
	for i, r := range fetchKeys {
		if r.Outcome != gen.FetchOutcomeServed {
			t.Errorf("fetch key %d outcome = %s, want served", i, r.Outcome)
		}
		if len(r.Response.Listings) != 1 {
			t.Errorf("fetch key %d listings = %d, want 1", i, len(r.Response.Listings))
		}
	}
	if row := h.key(t, one.ID); row.Attempts != 1 || row.Reason != nil || row.Candidates != 1 {
		t.Errorf("row = %+v, want one attempt, no reason and one listing", row)
	}
	if len(h.blocks) != 0 {
		t.Errorf("blocks = %+v, want none", h.blocks)
	}
}

// TestFetchRetries checks that a temporary failure about the identifier is
// repeated, and that the attempts are recorded.
func TestFetchRetries(t *testing.T) {
	f := &fake{errs: []error{temporary("503"), temporary("503")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")

	fetchKeys := h.run(t, k)
	if fetchKeys[0].Outcome != gen.FetchOutcomeServed {
		t.Errorf("outcome = %s, want served on the third attempt", fetchKeys[0].Outcome)
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
	if row := h.key(t, k.ID); row.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", row.Attempts)
	}
}

// TestFetchRetriesExhausted checks that a temporary failure that never clears
// leaves its keys failed temporarily, with the calls spent, and no block.
func TestFetchRetriesExhausted(t *testing.T) {
	f := &fake{errs: []error{temporary("503"), temporary("503"), temporary("503")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")

	fetchKeys := h.run(t, k)
	if fetchKeys[0].Outcome != gen.FetchOutcomeFailedTemporary {
		t.Errorf("outcome = %s, want failed_temporary", fetchKeys[0].Outcome)
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, want the cap of 3", f.calls)
	}
	if row := h.key(t, k.ID); row.Attempts != 3 || row.Reason == nil || *row.Reason != "503" {
		t.Errorf("row = %+v, want 3 attempts and the failure's reason", row)
	}
	if len(h.blocks) != 0 {
		t.Errorf("blocks = %+v, want none: a later fetch asks again", h.blocks)
	}
}

// TestFetchQuotaIsNotRetried checks that a temporary failure about the
// datasource costs one call and blocks nothing.
func TestFetchQuotaIsNotRetried(t *testing.T) {
	f := &fake{errs: []error{quota("429")}}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

	fetchKeys := h.run(t, one, two)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: repeating a spent quota cannot help", f.calls)
	}
	for i, r := range fetchKeys {
		if r.Outcome != gen.FetchOutcomeFailedTemporary {
			t.Errorf("fetch key %d outcome = %s, want failed_temporary", i, r.Outcome)
		}
	}
	if len(h.blocks) != 0 {
		t.Errorf("blocks = %+v, want none: a spent quota returns", h.blocks)
	}
	if _, paused := h.entry.limiter.paused(h.now); paused {
		t.Error("the datasource is paused, want no pause without a delay")
	}
}

// TestFetchPause checks that a temporary failure of the datasource with a
// delay fails every later key without a call, sleep or block until the delay
// passes.
func TestFetchPause(t *testing.T) {
	f := &fake{batch: 1, errs: []error{spent("quota spent", 6*time.Hour)}}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")
	const reason = "paused until 2026-10-01T06:00:00Z after: quota spent"

	fetchKeys := h.run(t, one, two)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: the second key falls in the pause", f.calls)
	}
	want := []struct {
		reason   string
		attempts int16
	}{{"quota spent", 1}, {reason, 0}}
	for i, w := range want {
		row := h.key(t, fetchKeys[i].Request.ID)
		if fetchKeys[i].Outcome != gen.FetchOutcomeFailedTemporary || row.Reason == nil || *row.Reason != w.reason || row.Attempts != w.attempts {
			t.Errorf("key %d = %s, %+v, want failed_temporary with %q after %d attempts", i, fetchKeys[i].Outcome, row, w.reason, w.attempts)
		}
	}

	three := keyOf(f, "US5949181045")
	h.now = start.Add(time.Hour)
	if r := h.run(t, three); f.calls != 1 || r[0].Reason != reason {
		t.Errorf("a fetch inside the pause: calls = %d, reason %q, want no call and %q", f.calls, r[0].Reason, reason)
	}
	if len(h.sleeps) != 0 || len(h.blocks) != 0 {
		t.Errorf("sleeps = %v, blocks = %+v, want none: a paused key fails at once and returns", h.sleeps, h.blocks)
	}

	h.now = start.Add(6 * time.Hour)
	if r := h.run(t, three); f.calls != 2 || r[0].Outcome != gen.FetchOutcomeServed {
		t.Errorf("a fetch after the pause: calls = %d, outcome %s, want a call that is served", f.calls, r[0].Outcome)
	}
}

// TestFetchPauseDuringBackoff checks that a request waiting to retry makes no
// further call once another fetch pauses the datasource.
func TestFetchPauseDuringBackoff(t *testing.T) {
	f := &fake{errs: []error{temporary("503")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")
	h.onSleep = func() { h.entry.limiter.pause(h.now.Add(time.Hour), "quota spent") }

	fetchKeys := h.run(t, k)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1", f.calls)
	}
	if r := fetchKeys[0]; r.Outcome != gen.FetchOutcomeFailedTemporary || !strings.HasPrefix(r.Reason, "paused until ") {
		t.Errorf("fetch key = %s %q, want failed_temporary for the pause", r.Outcome, r.Reason)
	}
	if len(h.blocks) != 0 {
		t.Errorf("blocks = %+v, want none", h.blocks)
	}
}

// TestFetchCredentialBlocksDatasource checks that a permanent failure about
// the datasource blocks it without a retry.
func TestFetchCredentialBlocksDatasource(t *testing.T) {
	f := &fake{errs: []error{credential("401")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")

	fetchKeys := h.run(t, k)
	if fetchKeys[0].Outcome != gen.FetchOutcomeFailedPermanent {
		t.Errorf("outcome = %s, want failed_permanent", fetchKeys[0].Outcome)
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1", f.calls)
	}
	if len(h.blocks) != 1 || h.blocks[0].Scope != gen.BlockScopeDatasource {
		t.Errorf("blocks = %+v, want one on the datasource", h.blocks)
	}
}

// TestFetchPerKeyError checks that an error in one position of a good
// response is that key's alone.
func TestFetchPerKeyError(t *testing.T) {
	f := &fake{perKey: map[string]error{"US0378331005": permanent("unknown identifier")}}
	h := newHarness(t, f, nil)
	good, bad := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

	fetchKeys := h.run(t, good, bad)
	if fetchKeys[0].Outcome != gen.FetchOutcomeServed {
		t.Errorf("good key outcome = %s, want served", fetchKeys[0].Outcome)
	}
	if fetchKeys[1].Outcome != gen.FetchOutcomeFailedPermanent {
		t.Errorf("bad key outcome = %s, want failed_permanent", fetchKeys[1].Outcome)
	}
	if len(h.blocks) != 1 || *h.blocks[0].SentValue != "US0378331005" {
		t.Fatalf("blocks = %+v, want one on the refused identifier", h.blocks)
	}
}

// TestFetchRefusalOfTheRequest checks that a permanent failure of what one
// request asked fails its key and leaves every block as it was, whether the
// whole request fails or one position of a good response.
func TestFetchRefusalOfTheRequest(t *testing.T) {
	tests := []struct {
		name string
		f    *fake
	}{
		{name: "the whole request", f: &fake{errs: []error{refusal("outside the window")}}},
		{name: "one position", f: &fake{perKey: map[string]error{"GB00B03MLX29": refusal("outside the window")}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			h := newHarness(t, tc.f, nil)
			fetchKeys := h.run(t, keyOf(tc.f, "GB00B03MLX29"))
			if got := fetchKeys[0]; got.Outcome != gen.FetchOutcomeFailedPermanent || got.Reason != "outside the window" {
				t.Errorf("outcome = %s, reason = %q, want failed_permanent and the refusal", got.Outcome, got.Reason)
			}
			if tc.f.calls != 1 {
				t.Errorf("calls = %d, want 1", tc.f.calls)
			}
			if len(h.blocks) != 0 {
				t.Errorf("blocks = %+v, want none", h.blocks)
			}
		})
	}
}

// TestFetchOpenBlocks checks that where a block is read as the fetch starts,
// it suppresses its call, and that a datasource block suppresses every call.
func TestFetchOpenBlocks(t *testing.T) {
	tests := []struct {
		name  string
		open  []gen.DatasourceBlock
		calls int
		want  []gen.FetchOutcome
	}{
		{
			name: "one identifier",
			open: []gen.DatasourceBlock{{
				Scope: gen.BlockScopeIdentifier, Reason: "refused",
				SentType: ptr.To(types.IdentifierTypeIsin), SentValue: ptr.To("US0378331005"),
			}},
			calls: 1,
			want:  []gen.FetchOutcome{gen.FetchOutcomeServed, gen.FetchOutcomeBlocked},
		},
		{
			name:  "the whole datasource",
			open:  []gen.DatasourceBlock{{Scope: gen.BlockScopeDatasource, Reason: "quota spent"}},
			calls: 0,
			want:  []gen.FetchOutcome{gen.FetchOutcomeBlocked, gen.FetchOutcomeBlocked},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{}
			h := newHarness(t, f, tc.open)
			one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

			fetchKeys := h.run(t, one, two)
			if f.calls != tc.calls {
				t.Errorf("calls = %d, want %d", f.calls, tc.calls)
			}
			for i, want := range tc.want {
				if fetchKeys[i].Outcome != want {
					t.Errorf("fetch key %d outcome = %s, want %s", i, fetchKeys[i].Outcome, want)
				}
			}
			for _, k := range h.keys {
				if k.Outcome == gen.FetchOutcomeBlocked && (k.Attempts != 0 || k.SentType == nil) {
					t.Errorf("blocked row = %+v, want no attempt and the identifier the block matched", k)
				}
			}
			if len(h.blocks) != 0 {
				t.Errorf("blocks = %+v, want none written", h.blocks)
			}
		})
	}
}

// TestFetchRecordsAgainstItsRun checks that the fetch is a child run and that
// every key names it.
func TestFetchRecordsAgainstItsRun(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")

	h.run(t, k)
	if h.child.Kind != gen.RunKindDatasourceFetch {
		t.Errorf("child kind = %s, want fetch", h.child.Kind)
	}
	row := h.key(t, k.ID)
	if row.FetchID != h.child.ID || row.UserID != h.parent.UserID {
		t.Errorf("row = %+v, want the child run and the parent's user", row)
	}
}

// TestFetchChunks checks that where a batch is larger than the integration
// accepts, it is sent as several requests, each responded to in the order of
// its keys.
func TestFetchChunks(t *testing.T) {
	f := &fake{batch: 2}
	h := newHarness(t, f, nil)
	values := []string{"GB00B03MLX29", "US0378331005", "US5949181045", "US0231351067", "US02079K3059"}
	keys := make([]gen.StatedKey, len(values))
	for i, v := range values {
		keys[i] = keyOf(f, v)
	}

	fetchKeys := h.run(t, keys...)
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3 requests of at most 2", f.calls)
	}
	for n, sent := range f.sent {
		if len(sent) > 2 {
			t.Errorf("request %d sent %d identifiers, want at most 2", n, len(sent))
		}
	}
	for i, r := range fetchKeys {
		if r.Outcome != gen.FetchOutcomeServed {
			t.Errorf("fetch key %d outcome = %s, want served", i, r.Outcome)
			continue
		}
		if got := r.Response.Listings[0].Identifiers[0].Value; got != values[i] {
			t.Errorf("fetch key %d returned %s, want %s", i, got, values[i])
		}
	}
}

// TestFetchChunkFails checks that when a request fails about its identifiers,
// it fails only the keys it carried.
func TestFetchChunkFails(t *testing.T) {
	f := &fake{batch: 1, errs: []error{nil, permanent("refused")}}
	h := newHarness(t, f, nil)
	one, two, three := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005"), keyOf(f, "US5949181045")

	fetchKeys := h.run(t, one, two, three)
	want := []gen.FetchOutcome{gen.FetchOutcomeServed, gen.FetchOutcomeFailedPermanent, gen.FetchOutcomeServed}
	for i, w := range want {
		if fetchKeys[i].Outcome != w {
			t.Errorf("fetch key %d outcome = %s, want %s", i, fetchKeys[i].Outcome, w)
		}
	}
	if len(h.blocks) != 1 || *h.blocks[0].SentValue != "US0378331005" {
		t.Errorf("blocks = %+v, want one on the identifier of the failed request", h.blocks)
	}
}

// TestFetchChunkBlocksDatasource checks that when a request blocks the
// datasource, the fetch leaves the requests after it unsent and their keys
// blocked.
func TestFetchChunkBlocksDatasource(t *testing.T) {
	f := &fake{batch: 1, errs: []error{nil, credential("401")}}
	h := newHarness(t, f, nil)
	one, two, three := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005"), keyOf(f, "US5949181045")

	fetchKeys := h.run(t, one, two, three)
	if f.calls != 2 {
		t.Errorf("calls = %d, want 2: nothing is sent once the datasource is blocked", f.calls)
	}
	want := []gen.FetchOutcome{gen.FetchOutcomeServed, gen.FetchOutcomeFailedPermanent, gen.FetchOutcomeBlocked}
	for i, w := range want {
		if fetchKeys[i].Outcome != w {
			t.Errorf("fetch key %d outcome = %s, want %s", i, fetchKeys[i].Outcome, w)
		}
	}
	if row := h.key(t, three.ID); row.Attempts != 0 || row.Reason == nil || *row.Reason != "401" {
		t.Errorf("row = %+v, want no attempt and the reason of the datasource block", row)
	}
	if len(h.blocks) != 1 || h.blocks[0].Scope != gen.BlockScopeDatasource {
		t.Errorf("blocks = %+v, want one on the datasource", h.blocks)
	}
}

// plain is an integration that serves no kind of data.
type plain struct{ f *fake }

func (p plain) Classify(err error) Failure { return p.f.Classify(err) }
func (p plain) Limit() (rate.Limit, int)   { return p.f.Limit() }
func (p plain) Batch() int                 { return p.f.Batch() }
func (p plain) Endpoint() string           { return p.f.Endpoint() }

// TestFetchKindNotServed checks that a datasource without a server for the
// kind is not called, and that its keys say why.
func TestFetchKindNotServed(t *testing.T) {
	f := &fake{}
	h := newHarness(t, plain{f}, nil)
	k := keyOf(f, "GB00B03MLX29")

	fetchKeys := h.run(t, k)
	if fetchKeys[0].Outcome != gen.FetchOutcomeNotServed {
		t.Errorf("outcome = %s, want not_served", fetchKeys[0].Outcome)
	}
	if f.calls != 0 {
		t.Errorf("calls = %d, want 0", f.calls)
	}
	if row := h.key(t, k.ID); row.SentType != nil || row.Reason == nil {
		t.Errorf("row = %+v, want no identifier sent and a reason", row)
	}
}

// TestFetchRetryAfter checks that a refusal naming when to try again makes
// the datasource wait that long, capped, rather than backing off.
func TestFetchRetryAfter(t *testing.T) {
	tests := []struct {
		name  string
		after time.Duration
		want  time.Duration
	}{
		{name: "a short wait", after: 500 * time.Millisecond, want: 500 * time.Millisecond},
		{name: "a long wait is capped", after: time.Minute, want: retryMax},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{errs: []error{rateLimited("429", tc.after)}}
			h := newHarness(t, f, nil)
			k := keyOf(f, "GB00B03MLX29")

			fetchKeys := h.run(t, k)
			if fetchKeys[0].Outcome != gen.FetchOutcomeServed || f.calls != 2 {
				t.Errorf("outcome = %s after %d calls, want served after 2", fetchKeys[0].Outcome, f.calls)
			}
			if diff := cmp.Diff([]time.Duration{tc.want}, h.sleeps); diff != "" {
				t.Errorf("sleeps mismatch (-want +got):\n%s", diff)
			}
			if got := h.entry.limiter.waits(start); got != tc.want {
				t.Errorf("wait from the start = %s, want %s", got, tc.want)
			}
		})
	}
}

// TestFetchWaits checks that a fetch sleeps once, for what remains of the
// provider's stated wait, before its first call.
func TestFetchWaits(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	h.entry.limiter.waitUntil(start.Add(time.Second))
	k := keyOf(f, "GB00B03MLX29")

	h.run(t, k)
	if diff := cmp.Diff([]time.Duration{time.Second}, h.sleeps); diff != "" {
		t.Errorf("sleeps mismatch (-want +got):\n%s", diff)
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1", f.calls)
	}
}

// TestFetchCancelled checks that a fetch whose context ends during the
// backoff makes no further call.
func TestFetchCancelled(t *testing.T) {
	f := &fake{errs: []error{temporary("503"), temporary("503")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	h.onSleep = cancel

	fetchKeys, err := fetch(ctx, h.fetcher, h.parent, h.entry, identityKind, []gen.StatedKey{k})
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if f.calls != 1 || len(h.sleeps) != 1 {
		t.Errorf("calls = %d, sleeps = %v, want one call and one sleep", f.calls, h.sleeps)
	}
	if fetchKeys[0].Outcome != gen.FetchOutcomeFailedTemporary {
		t.Errorf("outcome = %s, want failed_temporary", fetchKeys[0].Outcome)
	}
}

// TestFetchShortResponse checks that the keys a response leaves out fail
// temporarily, so a later fetch asks again.
func TestFetchShortResponse(t *testing.T) {
	f := &fake{drop: 1}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

	fetchKeys := h.run(t, one, two)
	want := []gen.FetchOutcome{gen.FetchOutcomeServed, gen.FetchOutcomeFailedTemporary}
	for i, w := range want {
		if fetchKeys[i].Outcome != w {
			t.Errorf("fetch key %d outcome = %s, want %s", i, fetchKeys[i].Outcome, w)
		}
	}
}

// TestFetchBlockSuppressesLaterRequests checks that an identifier a request
// blocks is not sent by a later request of the same fetch.
func TestFetchBlockSuppressesLaterRequests(t *testing.T) {
	f := &fake{batch: 1, perKey: map[string]error{"US0378331005": permanent("unknown identifier")}}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "US0378331005"), keyOf(f, "US0378331005")

	fetchKeys := h.run(t, one, two)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: the block from the first request suppresses the second", f.calls)
	}
	want := []gen.FetchOutcome{gen.FetchOutcomeFailedPermanent, gen.FetchOutcomeBlocked}
	for i, w := range want {
		if fetchKeys[i].Outcome != w {
			t.Errorf("fetch key %d outcome = %s, want %s", i, fetchKeys[i].Outcome, w)
		}
	}
}

// TestFetchFindings checks that a block counts a finding only where its write
// inserted one, and once however many times the transaction runs.
func TestFetchFindings(t *testing.T) {
	tests := []struct {
		name    string
		created int64
		txRuns  int
		want    map[string]int64
	}{
		{name: "a new block", created: 1, want: map[string]int64{"stonks.findings{kind=block}": 1}},
		{name: "a new block written twice", created: 1, txRuns: 2, want: map[string]int64{"stonks.findings{kind=block}": 1}},
		{name: "a block already open", created: 0, want: map[string]int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{errs: []error{credential("401")}}
			h := newHarness(t, f, nil)
			h.created, h.txRuns = tc.created, tc.txRuns
			counts(t)

			h.run(t, keyOf(f, "GB00B03MLX29"))
			got := counts(t)
			for name := range got {
				if !strings.HasPrefix(name, "stonks.findings") {
					delete(got, name)
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("counts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFetchWriteFails checks that a fetch whose rows cannot be written fails
// with no fetch keys.
func TestFetchWriteFails(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	boom := errors.New("boom")
	h.keyErr = boom

	fetchKeys, err := fetch(context.Background(), h.fetcher, h.parent, h.entry, identityKind, []gen.StatedKey{keyOf(f, "GB00B03MLX29")})
	if !errors.Is(err, boom) || fetchKeys != nil {
		t.Errorf("fetch() = %v, %v, want no fetch keys and %v", fetchKeys, err, boom)
	}
}

// TestFetchCacheHit checks that a key fetched again within the cache's
// lifetime is served from the cache, with no call, zero attempts and the
// answer the call produced.
func TestFetchCacheHit(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	first, second := keyOf(f, "GB00B03MLX29"), keyOf(f, "GB00B03MLX29")
	counts(t)

	miss := h.run(t, first)[0]
	if f.calls != 1 || miss.cached {
		t.Fatalf("calls = %d, cached = %v after the first fetch, want one call that missed", f.calls, miss.cached)
	}
	if len(h.cache.entries) != 1 {
		t.Fatalf("entries = %d, want the served answer stored", len(h.cache.entries))
	}
	if !miss.AnsweredAt.Equal(start) {
		t.Errorf("miss answered at %v, want the moment of the call %v", miss.AnsweredAt, start)
	}

	h.now = start.Add(time.Hour)
	hit := h.run(t, second)[0]
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: the second fetch is answered from the cache", f.calls)
	}
	if hit.Outcome != gen.FetchOutcomeServed || !hit.cached {
		t.Errorf("outcome = %s, cached = %v, want served from the cache", hit.Outcome, hit.cached)
	}
	if diff := cmp.Diff(miss.Response, hit.Response); diff != "" {
		t.Errorf("the cached answer differs from the one the call produced (-call +cache):\n%s", diff)
	}
	if !hit.AnsweredAt.Equal(start) {
		t.Errorf("hit answered at %v, want the moment of the call that filled the cache %v", hit.AnsweredAt, start)
	}
	if row := h.key(t, second.ID); row.Outcome != gen.FetchOutcomeServed || row.Attempts != 0 || row.Candidates != 1 || row.AnsweredAt == nil || !row.AnsweredAt.Equal(start) {
		t.Errorf("row = %+v, want served with zero attempts, one listing, answered at %v", row, start)
	}
	got := counts(t)
	for name := range got {
		if !strings.HasPrefix(name, "stonks.datasource.keys") {
			delete(got, name)
		}
	}
	want := map[string]int64{
		"stonks.datasource.keys{cached=false,datasource=fake,outcome=served}": 1,
		"stonks.datasource.keys{cached=true,datasource=fake,outcome=served}":  1,
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("counts mismatch (-want +got):\n%s", diff)
	}
}

// TestFetchCacheEmptyAnswer checks that an answer with no listing is
// cached as any other, and keeps the provider's statement that it knows
// nothing under the identifier.
func TestFetchCacheEmptyAnswer(t *testing.T) {
	f := &fake{empty: true, unknown: true}
	h := newHarness(t, f, nil)
	first, second := keyOf(f, "GB00B03MLX29"), keyOf(f, "GB00B03MLX29")

	h.run(t, first)
	hit := h.run(t, second)[0]
	if f.calls != 1 || !hit.cached || len(hit.Response.Listings) != 0 || !hit.Response.Unknown {
		t.Errorf("calls = %d, cached = %v, listings = %d, unknown = %v, want one call and an empty, unknown hit",
			f.calls, hit.cached, len(hit.Response.Listings), hit.Response.Unknown)
	}
}

// TestFetchCacheFailureNotStored checks that a key the call failed is not
// cached, so the next fetch calls again.
func TestFetchCacheFailureNotStored(t *testing.T) {
	f := &fake{perKey: map[string]error{"US0378331005": permanent("unknown identifier")}}
	h := newHarness(t, f, nil)

	h.run(t, keyOf(f, "US0378331005"))
	if len(h.cache.entries) != 0 {
		t.Errorf("entries = %v, want none: a failure is not an answer", h.cache.entries)
	}
}

// TestFetchCacheParams checks that the parameters the integration derives
// from a key are part of its cache key.
func TestFetchCacheParams(t *testing.T) {
	f := &fake{}
	h := newHarness(t, &parameterised{fake: f, params: []string{"USD"}}, nil)
	k := keyOf(f, "GB00B03MLX29")

	h.run(t, k)
	want := cacheKey(h.entry.Name, gen.FetchKindIdentity, isin("GB00B03MLX29"), []string{"USD"})
	if _, ok := h.cache.entries[want]; !ok {
		t.Errorf("entries = %v, want one under %q", h.cache.entries, want)
	}
}

// TestFetchCachePaused checks that the cache is read before the pause: a
// paused datasource serves a hit and fails a miss.
func TestFetchCachePaused(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	cached, fresh := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")
	h.run(t, cached)
	h.entry.limiter.pause(h.now.Add(time.Hour), "quota spent")

	fetchKeys := h.run(t, keyOf(f, "GB00B03MLX29"), fresh)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: a paused datasource gets no call", f.calls)
	}
	if fetchKeys[0].Outcome != gen.FetchOutcomeServed || !fetchKeys[0].cached {
		t.Errorf("cached key outcome = %s, want served from the cache", fetchKeys[0].Outcome)
	}
	if fetchKeys[1].Outcome != gen.FetchOutcomeFailedTemporary {
		t.Errorf("fresh key outcome = %s, want failed_temporary", fetchKeys[1].Outcome)
	}
}

// TestFetchFreshSkipsCache checks that a fresh fetch calls the datasource
// whatever the cache holds, and replaces the cached answer with its own. The
// fetcher it was made from still reads the cache.
func TestFetchFreshSkipsCache(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	h.run(t, keyOf(f, "GB00B03MLX29"))

	later := start.Add(24 * time.Hour)
	h.now = later
	fetchKeys, err := fetch(context.Background(), h.fetcher.Fresh(), h.parent, h.entry, identityKind, []gen.StatedKey{keyOf(f, "GB00B03MLX29")})
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	got := fetchKeys[0]
	if f.calls != 2 || got.cached {
		t.Errorf("calls = %d, cached = %v, want a second call that skipped the cache", f.calls, got.cached)
	}
	if !got.AnsweredAt.Equal(later) {
		t.Errorf("answered at %v, want the moment of the fresh call %v", got.AnsweredAt, later)
	}

	h.now = later.Add(time.Hour)
	hit := h.run(t, keyOf(f, "GB00B03MLX29"))[0]
	if f.calls != 2 || !hit.cached {
		t.Errorf("calls = %d, cached = %v, want the original fetcher served from the cache", f.calls, hit.cached)
	}
	if !hit.AnsweredAt.Equal(later) {
		t.Errorf("hit answered at %v, want the fresh answer's moment %v", hit.AnsweredAt, later)
	}
}

// TestFetchFreshPaused checks that a fresh fetch from a paused datasource
// fails temporarily without a call, though the cache holds an answer.
func TestFetchFreshPaused(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	h.run(t, keyOf(f, "GB00B03MLX29"))
	h.entry.limiter.pause(h.now.Add(time.Hour), "quota spent")

	fetchKeys, err := fetch(context.Background(), h.fetcher.Fresh(), h.parent, h.entry, identityKind, []gen.StatedKey{keyOf(f, "GB00B03MLX29")})
	if err != nil {
		t.Fatalf("fetch() error = %v", err)
	}
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: a paused datasource gets no call", f.calls)
	}
	if got := fetchKeys[0]; got.Outcome != gen.FetchOutcomeFailedTemporary || got.cached {
		t.Errorf("outcome = %s, cached = %v, want failed_temporary without the cache", got.Outcome, got.cached)
	}
}

// TestFetchTickerIdentity checks that a ticker identity fetch is served as
// an identity fetch, and that its fetch key leaves the stated key empty.
func TestFetchTickerIdentity(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)

	fetchKeys, err := h.fetcher.TickerIdentity(context.Background(), h.parent, h.entry, []gen.StatedKey{keyOf(f, "GB00B03MLX29")})
	if err != nil {
		t.Fatalf("TickerIdentity() error = %v", err)
	}
	if fetchKeys[0].Outcome != gen.FetchOutcomeServed {
		t.Errorf("outcome = %s, want served", fetchKeys[0].Outcome)
	}
	if len(h.keys) != 1 || h.keys[0].StatedKeyID != nil {
		t.Errorf("fetch keys = %+v, want one that names no stated key", h.keys)
	}
}

// TestFetchCacheErrors checks that a cache that cannot be read or written
// fails the fetch with no fetch keys.
func TestFetchCacheErrors(t *testing.T) {
	boom := errors.New("boom")
	tests := []struct {
		name string
		set  func(c *memCache)
	}{
		{name: "read", set: func(c *memCache) { c.getErr = boom }},
		{name: "write", set: func(c *memCache) { c.setErr = boom }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := &fake{}
			h := newHarness(t, f, nil)
			tc.set(h.cache)
			fetchKeys, err := fetch(context.Background(), h.fetcher, h.parent, h.entry, identityKind, []gen.StatedKey{keyOf(f, "GB00B03MLX29")})
			if !errors.Is(err, boom) || fetchKeys != nil {
				t.Errorf("fetch() = %v, %v, want no fetch keys and %v", fetchKeys, err, boom)
			}
			if len(h.keys) != 0 {
				t.Errorf("fetch keys = %d, want none recorded", len(h.keys))
			}
		})
	}
}

// TestFetchDomainBlock checks that a domain whose send fails permanently is
// blocked as a domain: the block holds no value, it suppresses a later send
// of the domain, and it leaves an identifier of the domain to be sent.
func TestFetchDomainBlock(t *testing.T) {
	nyse := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS"}
	value := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "AAPL"}
	f := &fake{batch: 1, perKey: map[string]error{"": permanent("the plan lacks the endpoint")}}
	h := newHarness(t, f, nil)
	first, second, other := gen.StatedKey{ID: db.NewID()}, gen.StatedKey{ID: db.NewID()}, gen.StatedKey{ID: db.NewID()}
	f.serves = map[string]types.Identifier{first.ID.String(): nyse, second.ID.String(): nyse, other.ID.String(): value}

	got := h.run(t, first, second, other)
	outcomes := []gen.FetchOutcome{got[0].Outcome, got[1].Outcome, got[2].Outcome}
	want := []gen.FetchOutcome{gen.FetchOutcomeFailedPermanent, gen.FetchOutcomeBlocked, gen.FetchOutcomeServed}
	if diff := cmp.Diff(want, outcomes); diff != "" {
		t.Errorf("outcomes mismatch (-want +got):\n%s", diff)
	}
	if len(h.blocks) != 1 {
		t.Fatalf("blocks = %+v, want one", h.blocks)
	}
	b := h.blocks[0]
	if b.Scope != gen.BlockScopeDomain || b.SentType == nil || *b.SentType != types.IdentifierTypeMicTicker || b.SentDomain != "XNYS" || b.SentValue != nil {
		t.Errorf("block = %+v, want the domain XNYS with no value", b)
	}
	if row := h.key(t, first.ID); row.SentValue != nil || row.SentDomain != "XNYS" {
		t.Errorf("row = %+v, want the domain sent with no value", row)
	}

	open := []gen.DatasourceBlock{{Scope: gen.BlockScopeDomain, SentType: b.SentType, SentDomain: "XNYS", Reason: "the plan lacks the endpoint"}}
	h = newHarness(t, f, open)
	again := gen.StatedKey{ID: db.NewID()}
	f.serves[again.ID.String()] = nyse
	if got := h.run(t, again)[0]; got.Outcome != gen.FetchOutcomeBlocked {
		t.Errorf("outcome under an open domain block = %s, want blocked", got.Outcome)
	}
}
