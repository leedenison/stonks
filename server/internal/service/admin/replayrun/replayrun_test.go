package replayrun

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	adminreplayrunv1 "github.com/leedenison/stonks/proto/admin/replay_run/v1"
	"github.com/leedenison/stonks/proto/admin/replay_run/v1/adminreplayrunv1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	replayer "github.com/leedenison/stonks/server/internal/run/replayrun"
	"github.com/leedenison/stonks/server/internal/service/admin/replayrun/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	userID  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	runID   = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	childID = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	created = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	adminID = servicetest.Admin.User.ID
)

type fixture struct {
	queries *mock.MockQueries
	replays *mock.MockReplayer
	client  adminreplayrunv1connect.ReplayRunServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicetest.AsAdmin(ctrl)
	f := &fixture{queries: mock.NewMockQueries(ctrl), replays: mock.NewMockReplayer(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminreplayrunv1connect.NewReplayRunServiceHandler(New(f.queries, f.replays), opts...))
	})
	f.client = adminreplayrunv1connect.NewReplayRunServiceClient(srv.Client, srv.URL)
	return f
}

func runRow(id uuid.UUID, kind gen.RunKind) gen.Run {
	return gen.Run{ID: id, UserID: &userID, Kind: kind, Trigger: gen.RunTriggerUser, State: gen.RunStateCompleted, CreatedAt: created}
}

func TestStartReplayRun(t *testing.T) {
	source := runRow(runID, gen.RunKindIngestStmt)
	started := gen.Run{ID: childID, UserID: &userID, Kind: gen.RunKindReplayRun, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: created}
	unavailable := &adminreplayrunv1.StartReplayRunRequest{RunId: runID.String(), Scope: &adminreplayrunv1.StartReplayRunRequest_Unavailable{Unavailable: true}}
	datasource := &adminreplayrunv1.StartReplayRunRequest{RunId: runID.String(), Scope: &adminreplayrunv1.StartReplayRunRequest_Datasource{Datasource: "openfigi"}}
	tests := []struct {
		name      string
		req       *adminreplayrunv1.StartReplayRunRequest
		readErr   error
		wantScope *replayer.Scope
		startErr  error
		wantCode  connect.Code
	}{
		{name: "the keys left unavailable", req: unavailable, wantScope: &replayer.Scope{}},
		{name: "the keys a datasource has not answered", req: datasource, wantScope: &replayer.Scope{Datasource: "openfigi"}},
		{name: "no such run", req: unavailable, readErr: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "a run without keys", req: unavailable, wantScope: &replayer.Scope{}, startErr: replayer.ErrKind, wantCode: connect.CodeFailedPrecondition},
		{name: "nothing to replay", req: unavailable, wantScope: &replayer.Scope{}, startErr: replayer.ErrEmpty, wantCode: connect.CodeFailedPrecondition},
		{name: "a datasource not enabled", req: datasource, wantScope: &replayer.Scope{Datasource: "openfigi"}, startErr: replayer.ErrDisabled, wantCode: connect.CodeFailedPrecondition},
		{name: "a datasource serving no identity", req: datasource, wantScope: &replayer.Scope{Datasource: "openfigi"}, startErr: replayer.ErrNoIdentity, wantCode: connect.CodeFailedPrecondition},
		{name: "failure", req: unavailable, wantScope: &replayer.Scope{}, startErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "no scope", req: &adminreplayrunv1.StartReplayRunRequest{RunId: runID.String()}, wantCode: connect.CodeInvalidArgument},
		{name: "unavailable unset", req: &adminreplayrunv1.StartReplayRunRequest{RunId: runID.String(), Scope: &adminreplayrunv1.StartReplayRunRequest_Unavailable{}}, wantCode: connect.CodeInvalidArgument},
		{name: "an empty datasource", req: &adminreplayrunv1.StartReplayRunRequest{RunId: runID.String(), Scope: &adminreplayrunv1.StartReplayRunRequest_Datasource{}}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed run id", req: &adminreplayrunv1.StartReplayRunRequest{RunId: "not-a-uuid", Scope: &adminreplayrunv1.StartReplayRunRequest_Unavailable{Unavailable: true}}, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode != connect.CodeInvalidArgument {
				f.queries.EXPECT().GetUserRun(gomock.Any(), runID).Return(gen.GetUserRunRow{Run: source, Email: ptr.To("one@example.com")}, tc.readErr)
			}
			if tc.wantScope != nil {
				f.replays.EXPECT().Start(gomock.Any(), adminID, source, *tc.wantScope).Return(started, tc.startErr)
			}
			res, err := f.client.StartReplayRun(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("StartReplayRun(%s) code = %v (err %v), want %v", tc.name, servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != childID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_REPLAY_RUN || got.GetTrigger() != runv1.RunTrigger_RUN_TRIGGER_ADMINISTRATOR || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("StartReplayRun(%s) = %v, want the pending replay run", tc.name, got)
			}
		})
	}
}
