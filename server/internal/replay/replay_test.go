package replay

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

var (
	userID   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	adminID  = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	sourceID = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	isin     = types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}
)

func statedKey(n int, ids ...types.Identifier) gen.StatedKey {
	id := uuid.MustParse("00000000-0000-7000-8000-" + strings.Repeat("0", 11) + string(rune('0'+n)))
	return gen.StatedKey{ID: id, UserID: userID, StatementID: sourceID, Identifiers: ids}
}

func source(kind gen.RunKind) gen.Run {
	return gen.Run{ID: sourceID, UserID: userID, Kind: kind, Trigger: gen.RunTriggerUser, State: gen.RunStateCompleted}
}

// identity is an identity integration serving the keys stating an ISIN.
type identity struct{}

func (identity) Classify(error) market.Failure { return market.Failure{Temporary: true} }
func (identity) Limit() (rate.Limit, int)      { return rate.Inf, 1 }
func (identity) Batch() int                    { return 10 }
func (identity) Endpoint() string              { return "https://identity.test" }
func (identity) Serves(k gen.StatedKey) (types.Identifier, error) {
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeIsin {
			return id, nil
		}
	}
	return types.Identifier{}, errors.New("no ISIN")
}
func (identity) Fetch(context.Context, []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	return nil, errors.New("not called")
}

type fixture struct {
	store    *MockStore
	runs     *MockRunner
	resolver *MockResolver
	sources  *MockSources
	svc      *Service

	entries    []*market.Entry
	resolveErr error
	lockErr    error

	// spec is what Start was given, and started whether it was called.
	spec    run.Spec
	started bool
	workErr error
	// replays, resolution, resolved and regrouped record the writes in order.
	replays    []gen.CreateReplayParams
	resolution gen.Run
	resolved   []gen.StatedKey
	regrouped  []uuid.UUID
}

// newFixture returns a service whose writes run inline and in order.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl), resolver: NewMockResolver(ctrl), sources: NewMockSources(ctrl)}
	f.sources.EXPECT().Enabled().DoAndReturn(func() []*market.Entry { return f.entries }).AnyTimes()
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error { return fn(f.store) }).AnyTimes()
	f.store.EXPECT().CreateReplay(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateReplayParams) error {
		f.replays = append(f.replays, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().LockUserKeys(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, user uuid.UUID) error {
		if f.lockErr != nil {
			return f.lockErr
		}
		f.regrouped = append(f.regrouped, user)
		return nil
	}).AnyTimes()
	f.store.EXPECT().ListGroupableKeys(gomock.Any(), userID).Return(nil, nil).AnyTimes()
	f.store.EXPECT().ClearStatedKeyGroups(gomock.Any(), userID).Return(nil).AnyTimes()
	f.resolver.EXPECT().Resolve(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, res gen.Run, keys []gen.StatedKey) ([]gen.ResolutionKey, error) {
		f.resolution, f.resolved = res, keys
		return nil, f.resolveErr
	}).AnyTimes()
	f.runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
		f.spec, f.started = spec, true
		row := gen.Run{ID: db.NewID(), UserID: spec.UserID, Kind: spec.Kind, Trigger: spec.Trigger, State: gen.RunStatePending}
		if spec.Prepare != nil {
			if err := spec.Prepare(ctx, row); err != nil {
				return row, err
			}
		}
		f.workErr = work(ctx, row)
		return row, nil
	}).AnyTimes()
	f.runs.EXPECT().Child(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
		row := gen.Run{ID: db.NewID(), UserID: parent.UserID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID}
		return row, work(ctx, row)
	}).AnyTimes()
	f.svc = New(f.store, f.runs, f.resolver, f.sources)
	return f
}

func TestStartUnavailable(t *testing.T) {
	f := newFixture(t)
	two := statedKey(2)
	f.store.EXPECT().ListUnavailableKeys(gomock.Any(), gen.ListUnavailableKeysParams{UserID: userID, SourceID: sourceID}).Return([]gen.ListUnavailableKeysRow{{StatedKey: two}}, nil)

	row, err := f.svc.Start(context.Background(), adminID, source(gen.RunKindStatement), Scope{})
	if err != nil || f.workErr != nil {
		t.Fatalf("Start() error = %v, work error = %v", err, f.workErr)
	}
	if row.Kind != gen.RunKindReplay || row.State != gen.RunStatePending {
		t.Errorf("Start() = %+v, want the pending replay run", row)
	}
	if f.spec.Kind != gen.RunKindReplay || f.spec.Trigger != gen.RunTriggerAdministrator || f.spec.UserID != userID || f.spec.Lane != lane || f.spec.Prepare == nil {
		t.Errorf("spec = %+v, want a replay of the source's user in the replay lane with a prepare step", f.spec)
	}
	wantReplays := []gen.CreateReplayParams{{ID: row.ID, UserID: userID, SourceID: sourceID, StartedBy: adminID}}
	if diff := cmp.Diff(wantReplays, f.replays); diff != "" {
		t.Errorf("replays row mismatch (-want +got):\n%s", diff)
	}
	if f.resolution.Kind != gen.RunKindResolution || f.resolution.ParentID == nil || *f.resolution.ParentID != row.ID {
		t.Errorf("resolution = %+v, want a resolution child of the replay", f.resolution)
	}
	if diff := cmp.Diff([]gen.StatedKey{two}, f.resolved); diff != "" {
		t.Errorf("resolved keys mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]uuid.UUID{userID}, f.regrouped); diff != "" {
		t.Errorf("regrouped users mismatch (-want +got):\n%s", diff)
	}
}

func TestStartResolution(t *testing.T) {
	f := newFixture(t)
	one := statedKey(1, isin)
	f.store.EXPECT().ListUnavailableKeys(gomock.Any(), gen.ListUnavailableKeysParams{UserID: userID, SourceID: sourceID}).Return([]gen.ListUnavailableKeysRow{{StatedKey: one}}, nil)

	if _, err := f.svc.Start(context.Background(), adminID, source(gen.RunKindResolution), Scope{}); err != nil || f.workErr != nil {
		t.Fatalf("Start() error = %v, work error = %v", err, f.workErr)
	}
	if diff := cmp.Diff([]gen.StatedKey{one}, f.resolved); diff != "" {
		t.Errorf("resolved keys mismatch (-want +got):\n%s", diff)
	}
}

func TestStartDatasource(t *testing.T) {
	f := newFixture(t)
	f.entries = []*market.Entry{{Name: "alpha", Integration: identity{}}}
	served, unserved := statedKey(1, isin), statedKey(2)
	f.store.EXPECT().ListKeysUncoveredBy(gomock.Any(), gen.ListKeysUncoveredByParams{UserID: userID, SourceID: sourceID, Datasource: "alpha"}).
		Return([]gen.ListKeysUncoveredByRow{{StatedKey: served}, {StatedKey: unserved}}, nil)

	row, err := f.svc.Start(context.Background(), adminID, source(gen.RunKindStatement), Scope{Datasource: "alpha"})
	if err != nil || f.workErr != nil {
		t.Fatalf("Start() error = %v, work error = %v", err, f.workErr)
	}
	if len(f.replays) != 1 || f.replays[0].Datasource == nil || *f.replays[0].Datasource != "alpha" || f.replays[0].ID != row.ID {
		t.Errorf("replays rows = %+v, want one naming alpha", f.replays)
	}
	if diff := cmp.Diff([]gen.StatedKey{served}, f.resolved); diff != "" {
		t.Errorf("resolved keys mismatch (-want +got):\n%s", diff)
	}
}

func TestStartRefused(t *testing.T) {
	tests := []struct {
		name    string
		kind    gen.RunKind
		scope   Scope
		entries []*market.Entry
		// uncovered is what the datasource query answers; unavailable what
		// the unavailable query answers.
		uncovered   []gen.StatedKey
		unavailable []gen.StatedKey
		want        error
	}{
		{name: "a fetch run", kind: gen.RunKindFetch, want: ErrKind},
		{name: "no key left unavailable", kind: gen.RunKindStatement, want: ErrEmpty},
		{name: "a datasource not enabled", kind: gen.RunKindStatement, scope: Scope{Datasource: "beta"}, entries: []*market.Entry{{Name: "alpha", Integration: identity{}}}, want: ErrDisabled},
		{name: "a datasource serving no identity", kind: gen.RunKindStatement, scope: Scope{Datasource: "alpha"}, entries: []*market.Entry{{Name: "alpha"}}, want: ErrNoIdentity},
		{name: "no key the datasource serves", kind: gen.RunKindStatement, scope: Scope{Datasource: "alpha"}, entries: []*market.Entry{{Name: "alpha", Integration: identity{}}}, uncovered: []gen.StatedKey{statedKey(2)}, want: ErrEmpty},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.entries = tc.entries
			f.store.EXPECT().ListUnavailableKeys(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, gen.ListUnavailableKeysParams) ([]gen.ListUnavailableKeysRow, error) {
				var rows []gen.ListUnavailableKeysRow
				for _, k := range tc.unavailable {
					rows = append(rows, gen.ListUnavailableKeysRow{StatedKey: k})
				}
				return rows, nil
			}).AnyTimes()
			f.store.EXPECT().ListKeysUncoveredBy(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, gen.ListKeysUncoveredByParams) ([]gen.ListKeysUncoveredByRow, error) {
				var rows []gen.ListKeysUncoveredByRow
				for _, k := range tc.uncovered {
					rows = append(rows, gen.ListKeysUncoveredByRow{StatedKey: k})
				}
				return rows, nil
			}).AnyTimes()

			_, err := f.svc.Start(context.Background(), adminID, source(tc.kind), tc.scope)
			if !errors.Is(err, tc.want) {
				t.Errorf("Start() error = %v, want %v", err, tc.want)
			}
			if f.started {
				t.Error("Start() started a run, want none")
			}
		})
	}
}

func TestWorkFails(t *testing.T) {
	tests := []struct {
		name       string
		resolveErr error
		lockErr    error
		want       string
		regrouped  int
	}{
		{name: "the resolution fails", resolveErr: errors.New("boom"), want: "resolution: boom"},
		{name: "the regroup fails", lockErr: errors.New("boom"), want: "lock keys: boom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.resolveErr, f.lockErr = tc.resolveErr, tc.lockErr
			key := statedKey(1, isin)
			f.store.EXPECT().ListUnavailableKeys(gomock.Any(), gomock.Any()).Return([]gen.ListUnavailableKeysRow{{StatedKey: key}}, nil)

			if _, err := f.svc.Start(context.Background(), adminID, source(gen.RunKindStatement), Scope{}); err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			if f.workErr == nil || f.workErr.Error() != tc.want {
				t.Errorf("work error = %v, want %q", f.workErr, tc.want)
			}
			if len(f.regrouped) != tc.regrouped {
				t.Errorf("regrouped %d users, want %d", len(f.regrouped), tc.regrouped)
			}
		})
	}
}
