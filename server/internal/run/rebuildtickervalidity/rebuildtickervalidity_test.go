package rebuildtickervalidity

import (
	"context"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run"
)

// TestStart checks that a rebuild is a system run an administrator started,
// in its own lane.
func TestStart(t *testing.T) {
	ctrl := gomock.NewController(t)
	runs := NewMockRunner(ctrl)
	row := gen.Run{Kind: gen.RunKindRebuildTickerValidity}
	runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, spec run.Spec, _ run.Work) (gen.Run, error) {
		if spec.Kind != gen.RunKindRebuildTickerValidity || spec.Trigger != gen.RunTriggerAdministrator || spec.UserID != nil || spec.Lane != lane {
			t.Errorf("spec = %+v, want a system rebuild ticker validity run an administrator started", spec)
		}
		return row, nil
	})
	if got, err := New(NewMockStore(ctrl), runs, 2).Start(context.Background()); err != nil || got.Kind != row.Kind {
		t.Errorf("Start() = %+v, %v, want the run", got, err)
	}
}
