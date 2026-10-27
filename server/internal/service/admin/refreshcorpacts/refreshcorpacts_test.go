package refreshcorpacts

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	adminrefreshcorpactsv1 "github.com/leedenison/stonks/proto/admin/refresh_corp_acts/v1"
	"github.com/leedenison/stonks/proto/admin/refresh_corp_acts/v1/adminrefreshcorpactsv1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	refresher "github.com/leedenison/stonks/server/internal/run/refreshcorpacts"
	"github.com/leedenison/stonks/server/internal/service/admin/refreshcorpacts/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var runID = uuid.MustParse("00000000-0000-0000-0000-000000000010")

func TestStartRefreshCorpActs(t *testing.T) {
	started := gen.Run{ID: runID, Kind: gen.RunKindRefreshCorpActs, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name     string
		startErr error
		wantCode connect.Code
	}{
		{name: "started"},
		{name: "no instrument held", startErr: refresher.ErrEmpty, wantCode: connect.CodeFailedPrecondition},
		{name: "failure", startErr: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			starter := mock.NewMockStarter(ctrl)
			opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
			srv := servicetest.Serve(t, func(mux *http.ServeMux) {
				mux.Handle(adminrefreshcorpactsv1connect.NewRefreshCorpActsServiceHandler(New(starter), opts...))
			})
			client := adminrefreshcorpactsv1connect.NewRefreshCorpActsServiceClient(srv.Client, srv.URL)
			starter.EXPECT().Start(gomock.Any()).Return(started, tc.startErr)

			res, err := client.StartRefreshCorpActs(context.Background(), connect.NewRequest(&adminrefreshcorpactsv1.StartRefreshCorpActsRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("StartRefreshCorpActs code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != runID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_REFRESH_CORP_ACTS || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("StartRefreshCorpActs = %v, want the pending refresh corporate actions run", got)
			}
		})
	}
}
