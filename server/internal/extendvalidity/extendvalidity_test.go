package extendvalidity

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

type fixture struct {
	queries  *MockQueries
	runs     *MockRunner
	extender *MockExtender
	replays  *MockReplays
	svc      *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{queries: NewMockQueries(ctrl), runs: NewMockRunner(ctrl), extender: NewMockExtender(ctrl), replays: NewMockReplays(ctrl)}
	f.svc = New(f.queries, f.runs, f.extender, f.replays)
	return f
}

// TestStart checks that extending validity runs as a system run over the held
// tickers it listed, and hands the facts it wrote to the replays.
func TestStart(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	held := []gen.ListHeldTickersRow{{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "SHEL"}}
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindExtendValidity, Trigger: gen.RunTriggerAdministrator}
	written := []resolve.WrittenIdentifierFacts{{Run: gen.Run{ID: db.NewID()}, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "SHEL"}}}}
	f.queries.EXPECT().ListHeldTickers(gomock.Any()).Return(held, nil)
	f.runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
		if spec.Kind != gen.RunKindExtendValidity || spec.Trigger != gen.RunTriggerAdministrator || spec.UserID != nil {
			t.Errorf("spec = %+v, want a system extend validity run an administrator started", spec)
		}
		return row, work(ctx, row)
	})
	f.extender.EXPECT().ExtendValidity(gomock.Any(), row, held).Return(written, nil)
	f.replays.EXPECT().ReplayNewIdentifierFacts(gomock.Any(), row, written).Return(nil)

	got, err := f.svc.Start(ctx)
	if err != nil || got.ID != row.ID {
		t.Errorf("Start() = %+v, %v, want the run %s", got, err, row.ID)
	}
}

// TestStartEmpty checks that extending validity with no held ticker is refused
// before a run is created.
func TestStartEmpty(t *testing.T) {
	f := newFixture(t)
	f.queries.EXPECT().ListHeldTickers(gomock.Any()).Return(nil, nil)
	if _, err := f.svc.Start(context.Background()); !errors.Is(err, ErrEmpty) {
		t.Errorf("Start() error = %v, want %v", err, ErrEmpty)
	}
}

// TestStartFails checks that a failed extend validity run fails and does not
// start a replay.
func TestStartFails(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("boom")
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindExtendValidity}
	f.queries.EXPECT().ListHeldTickers(gomock.Any()).Return([]gen.ListHeldTickersRow{{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "SHEL"}}, nil)
	f.runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, _ run.Spec, work run.Work) (gen.Run, error) {
		return row, work(ctx, row)
	})
	f.extender.EXPECT().ExtendValidity(gomock.Any(), row, gomock.Any()).Return(nil, boom)
	if _, err := f.svc.Start(context.Background()); !errors.Is(err, boom) {
		t.Errorf("Start() error = %v, want %v", err, boom)
	}
}
