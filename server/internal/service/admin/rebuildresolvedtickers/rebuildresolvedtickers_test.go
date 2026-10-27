package rebuildresolvedtickers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	adminrebuildresolvedtickersv1 "github.com/leedenison/stonks/proto/admin/rebuild_resolved_tickers/v1"
	"github.com/leedenison/stonks/proto/admin/rebuild_resolved_tickers/v1/adminrebuildresolvedtickersv1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service/admin/rebuildresolvedtickers/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var runID = uuid.MustParse("00000000-0000-0000-0000-000000000010")

func TestStartRebuildResolvedTickers(t *testing.T) {
	started := gen.Run{ID: runID, Kind: gen.RunKindRebuildResolvedTickers, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: time.Date(2026, 10, 27, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name     string
		startErr error
		wantCode connect.Code
	}{
		{name: "started"},
		{name: "failure", startErr: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			starter := mock.NewMockStarter(ctrl)
			opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
			srv := servicetest.Serve(t, func(mux *http.ServeMux) {
				mux.Handle(adminrebuildresolvedtickersv1connect.NewRebuildResolvedTickersServiceHandler(New(starter), opts...))
			})
			client := adminrebuildresolvedtickersv1connect.NewRebuildResolvedTickersServiceClient(srv.Client, srv.URL)
			starter.EXPECT().Start(gomock.Any()).Return(started, tc.startErr)

			res, err := client.StartRebuildResolvedTickers(context.Background(), connect.NewRequest(&adminrebuildresolvedtickersv1.StartRebuildResolvedTickersRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("StartRebuildResolvedTickers code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != runID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_REBUILD_RESOLVED_TICKERS || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("StartRebuildResolvedTickers = %v, want the pending rebuild resolved tickers run", got)
			}
		})
	}
}
