package datasource

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/run"
)

// harness wires a Fetcher over mocks and records what it wrote.
type harness struct {
	store   *MockStore
	entry   *Entry
	fetcher *Fetcher
	parent  gen.Run

	keys   []gen.CreateFetchKeyParams
	blocks []gen.CreateDatasourceBlockParams
	child  gen.Run
}

func newHarness(t *testing.T, integration Identity, open []gen.DatasourceBlock) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	h := &harness{
		store:  NewMockStore(ctrl),
		parent: gen.Run{ID: db.NewID(), UserID: db.NewID(), Kind: gen.RunKindResolution},
	}
	h.entry = &Entry{Name: "fake", Integration: integration, Identity: integration, limiter: rate.NewLimiter(rate.Inf, 1)}

	runs := NewMockRunner(ctrl)
	runs.EXPECT().Child(gomock.Any(), gomock.Any(), gen.RunKindFetch, gomock.Any()).
		DoAndReturn(func(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
			h.child = gen.Run{ID: db.NewID(), UserID: parent.UserID, Kind: kind}
			return h.child, work(ctx, h.child)
		}).AnyTimes()

	h.store.EXPECT().CreateFetch(gomock.Any(), gomock.Any()).Return(gen.Fetch{}, nil).AnyTimes()
	h.store.EXPECT().ListOpenBlocks(gomock.Any(), gomock.Any()).Return(open, nil).AnyTimes()
	h.store.EXPECT().CreateFetchKey(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, arg gen.CreateFetchKeyParams) error {
			h.keys = append(h.keys, arg)
			return nil
		}).AnyTimes()
	h.store.EXPECT().CreateDatasourceBlock(gomock.Any(), gomock.Any()).
		DoAndReturn(func(_ context.Context, arg gen.CreateDatasourceBlockParams) error {
			h.blocks = append(h.blocks, arg)
			return nil
		}).AnyTimes()

	h.fetcher = NewFetcher(h.store, runs, discard(), WithRetry(time.Millisecond, 3))
	return h
}

func isin(value string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeIsin, Value: value}
}

// keyOf returns a key and the identifier the fake sends for it.
func keyOf(f *fake, value string) StatedKey {
	k := StatedKey{ID: db.NewID()}
	if f.serves == nil {
		f.serves = map[string]types.Identifier{}
	}
	f.serves[k.ID.String()] = isin(value)
	return k
}

// unservedKey returns a key the fake serves nothing for.
func unservedKey() StatedKey { return StatedKey{ID: db.NewID()} }

func (h *harness) run(t *testing.T, keys ...StatedKey) []KeyResult {
	t.Helper()
	_, results, err := h.fetcher.Fetch(context.Background(), h.parent, h.entry, gen.FetchKindIdentity, keys)
	if err != nil {
		t.Fatalf("Fetch() error = %v", err)
	}
	return results
}

func (h *harness) key(t *testing.T, id uuid.UUID) gen.CreateFetchKeyParams {
	t.Helper()
	for _, k := range h.keys {
		if k.StatedKeyID == id {
			return k
		}
	}
	t.Fatalf("no fetch key written for stated key %s", id)
	return gen.CreateFetchKeyParams{}
}

// TestFetchNotServed checks that a key the integration declines costs no call
// and leaves no identifier sent.
func TestFetchNotServed(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	k := unservedKey()

	results := h.run(t, k)
	if results[0].Outcome != gen.FetchOutcomeNotServed {
		t.Errorf("outcome = %s, want not_served", results[0].Outcome)
	}
	if f.calls != 0 {
		t.Errorf("calls = %d, want 0: a key the integration declines spends no quota", f.calls)
	}
	row := h.key(t, k.ID)
	if row.SentType != nil || row.Attempts != 0 || row.Reason == nil {
		t.Errorf("row = %+v, want no identifier sent, no attempt and a reason", row)
	}
}

// TestFetchServed checks that one call answers every key of the batch.
func TestFetchServed(t *testing.T) {
	f := &fake{}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

	results := h.run(t, one, two)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: the batch is one request", f.calls)
	}
	if len(f.sent[0]) != 2 {
		t.Errorf("sent %d identifiers, want 2", len(f.sent[0]))
	}
	for i, r := range results {
		if r.Outcome != gen.FetchOutcomeServed {
			t.Errorf("result %d outcome = %s, want served", i, r.Outcome)
		}
		if len(r.Candidates) != 1 {
			t.Errorf("result %d candidates = %d, want 1", i, len(r.Candidates))
		}
	}
	if row := h.key(t, one.ID); row.Attempts != 1 || row.Reason != nil {
		t.Errorf("row = %+v, want one attempt and no reason", row)
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

	results := h.run(t, k)
	if results[0].Outcome != gen.FetchOutcomeServed {
		t.Errorf("outcome = %s, want served on the third attempt", results[0].Outcome)
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, want 3", f.calls)
	}
	if row := h.key(t, k.ID); row.Attempts != 3 {
		t.Errorf("attempts = %d, want 3", row.Attempts)
	}
}

// TestFetchRetriesExhausted checks that a temporary failure that never clears
// becomes a block on the identifiers it was sent for.
func TestFetchRetriesExhausted(t *testing.T) {
	f := &fake{errs: []error{temporary("503"), temporary("503"), temporary("503")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")

	results := h.run(t, k)
	if results[0].Outcome != gen.FetchOutcomeFailedTemporary {
		t.Errorf("outcome = %s, want failed_temporary", results[0].Outcome)
	}
	if f.calls != 3 {
		t.Errorf("calls = %d, want the cap of 3", f.calls)
	}
	if len(h.blocks) != 1 || h.blocks[0].Scope != gen.BlockScopeIdentifier {
		t.Fatalf("blocks = %+v, want one on the identifier", h.blocks)
	}
	if *h.blocks[0].SentValue != "GB00B03MLX29" {
		t.Errorf("block value = %s, want the identifier sent", *h.blocks[0].SentValue)
	}
}

// TestFetchQuotaIsNotRetried checks that a temporary failure about the
// datasource costs one call and blocks the datasource.
func TestFetchQuotaIsNotRetried(t *testing.T) {
	f := &fake{errs: []error{quota("429")}}
	h := newHarness(t, f, nil)
	one, two := keyOf(f, "GB00B03MLX29"), keyOf(f, "US0378331005")

	results := h.run(t, one, two)
	if f.calls != 1 {
		t.Errorf("calls = %d, want 1: repeating a spent quota cannot help", f.calls)
	}
	for i, r := range results {
		if r.Outcome != gen.FetchOutcomeFailedTemporary {
			t.Errorf("result %d outcome = %s, want failed_temporary", i, r.Outcome)
		}
	}
	if len(h.blocks) != 1 || h.blocks[0].Scope != gen.BlockScopeDatasource {
		t.Fatalf("blocks = %+v, want one on the datasource", h.blocks)
	}
	if h.blocks[0].SentType != nil {
		t.Errorf("block names %v, want no identifier", h.blocks[0].SentType)
	}
}

// TestFetchCredentialBlocksDatasource checks that a permanent failure about
// the datasource blocks it without a retry.
func TestFetchCredentialBlocksDatasource(t *testing.T) {
	f := &fake{errs: []error{credential("401")}}
	h := newHarness(t, f, nil)
	k := keyOf(f, "GB00B03MLX29")

	results := h.run(t, k)
	if results[0].Outcome != gen.FetchOutcomeFailedPermanent {
		t.Errorf("outcome = %s, want failed_permanent", results[0].Outcome)
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

	results := h.run(t, good, bad)
	if results[0].Outcome != gen.FetchOutcomeServed {
		t.Errorf("good key outcome = %s, want served", results[0].Outcome)
	}
	if results[1].Outcome != gen.FetchOutcomeFailedPermanent {
		t.Errorf("bad key outcome = %s, want failed_permanent", results[1].Outcome)
	}
	if len(h.blocks) != 1 || *h.blocks[0].SentValue != "US0378331005" {
		t.Fatalf("blocks = %+v, want one on the refused identifier", h.blocks)
	}
}

// TestFetchOpenBlocks checks that a block read as the fetch starts suppresses
// its call, and that a datasource block suppresses every call.
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

			results := h.run(t, one, two)
			if f.calls != tc.calls {
				t.Errorf("calls = %d, want %d", f.calls, tc.calls)
			}
			for i, want := range tc.want {
				if results[i].Outcome != want {
					t.Errorf("result %d outcome = %s, want %s", i, results[i].Outcome, want)
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
	if h.child.Kind != gen.RunKindFetch {
		t.Errorf("child kind = %s, want fetch", h.child.Kind)
	}
	row := h.key(t, k.ID)
	if row.FetchID != h.child.ID || row.UserID != h.parent.UserID {
		t.Errorf("row = %+v, want the child run and the parent's user", row)
	}
}
