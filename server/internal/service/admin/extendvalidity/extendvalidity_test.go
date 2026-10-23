package extendvalidity

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	adminextendvalidityv1 "github.com/leedenison/stonks/proto/admin/extend_validity/v1"
	"github.com/leedenison/stonks/proto/admin/extend_validity/v1/adminextendvalidityv1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	extender "github.com/leedenison/stonks/server/internal/extendvalidity"
	"github.com/leedenison/stonks/server/internal/service/admin/extendvalidity/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var runID = uuid.MustParse("00000000-0000-0000-0000-000000000010")

func TestStartExtendValidity(t *testing.T) {
	started := gen.Run{ID: runID, Kind: gen.RunKindExtendValidity, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: time.Date(2026, 10, 23, 12, 0, 0, 0, time.UTC)}
	tests := []struct {
		name     string
		startErr error
		wantCode connect.Code
	}{
		{name: "started"},
		{name: "no ticker held", startErr: extender.ErrEmpty, wantCode: connect.CodeFailedPrecondition},
		{name: "failure", startErr: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			starter := mock.NewMockStarter(ctrl)
			opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
			srv := servicetest.Serve(t, func(mux *http.ServeMux) {
				mux.Handle(adminextendvalidityv1connect.NewExtendValidityServiceHandler(New(starter), opts...))
			})
			client := adminextendvalidityv1connect.NewExtendValidityServiceClient(srv.Client, srv.URL)
			starter.EXPECT().Start(gomock.Any()).Return(started, tc.startErr)

			res, err := client.StartExtendValidity(context.Background(), connect.NewRequest(&adminextendvalidityv1.StartExtendValidityRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("StartExtendValidity code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != runID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_EXTEND_VALIDITY || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("StartExtendValidity = %v, want the pending extend validity run", got)
			}
		})
	}
}
