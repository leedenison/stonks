package reasserttickers

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	adminreasserttickersv1 "github.com/leedenison/stonks/proto/admin/reassert_tickers/v1"
	"github.com/leedenison/stonks/proto/admin/reassert_tickers/v1/adminreasserttickersv1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	reasserter "github.com/leedenison/stonks/server/internal/run/reasserttickers"
	"github.com/leedenison/stonks/server/internal/service/admin/reasserttickers/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var runID = uuid.MustParse("00000000-0000-0000-0000-000000000010")

func TestStartReassertTickers(t *testing.T) {
	started := gen.Run{ID: runID, Kind: gen.RunKindReassertTickers, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name     string
		startErr error
		wantCode connect.Code
	}{
		{name: "started"},
		{name: "no ticker held", startErr: reasserter.ErrEmpty, wantCode: connect.CodeFailedPrecondition},
		{name: "failure", startErr: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			starter := mock.NewMockStarter(ctrl)
			opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
			srv := servicetest.Serve(t, func(mux *http.ServeMux) {
				mux.Handle(adminreasserttickersv1connect.NewReassertTickersServiceHandler(New(starter), opts...))
			})
			client := adminreasserttickersv1connect.NewReassertTickersServiceClient(srv.Client, srv.URL)
			starter.EXPECT().Start(gomock.Any()).Return(started, tc.startErr)

			res, err := client.StartReassertTickers(context.Background(), connect.NewRequest(&adminreasserttickersv1.StartReassertTickersRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("StartReassertTickers code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != runID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_REASSERT_TICKERS || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("StartReassertTickers = %v, want the pending reassert tickers run", got)
			}
		})
	}
}
