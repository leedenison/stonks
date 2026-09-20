package run

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/proto/run/v1/runv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/run/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	runID     = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	parentID  = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	created   = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: servicetest.Session}
)

type fixture struct {
	reader *mock.MockReader
	authn  *servicemock.MockAuthenticator
	client runv1connect.RunServiceClient
}

// newFixture mounts a Server with the real handler chain and a client that
// carries a session cookie.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{reader: mock.NewMockReader(ctrl), authn: servicemock.NewMockAuthenticator(ctrl)}
	opts := servicetest.Options(t, f.authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(runv1connect.NewRunServiceHandler(New(f.reader), opts...))
	})
	f.client = runv1connect.NewRunServiceClient(srv.Client, srv.URL)
	return f
}

func TestGetRun(t *testing.T) {
	started := created.Add(time.Second)
	finished := created.Add(2 * time.Second)
	failure := "boom"
	tests := []struct {
		name     string
		id       string
		authErr  error
		row      gen.Run
		err      error
		want     *runv1.Run
		wantCode connect.Code
	}{
		{
			name: "pending",
			id:   runID.String(),
			row:  gen.Run{ID: runID, UserID: userID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser, State: gen.RunStatePending, CreatedAt: created},
			want: &runv1.Run{Id: runID.String(), Kind: runv1.RunKind_RUN_KIND_STATEMENT, Trigger: runv1.RunTrigger_RUN_TRIGGER_USER, State: runv1.RunState_RUN_STATE_PENDING, CreatedAt: timestamppb.New(created)},
		},
		{
			name: "failed child",
			id:   runID.String(),
			row:  gen.Run{ID: runID, UserID: userID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &parentID, State: gen.RunStateFailed, Error: &failure, CreatedAt: created, StartedAt: &started, FinishedAt: &finished},
			want: &runv1.Run{
				Id: runID.String(), Kind: runv1.RunKind_RUN_KIND_RESOLUTION, Trigger: runv1.RunTrigger_RUN_TRIGGER_RUN, ParentId: ptr.To(parentID.String()),
				State: runv1.RunState_RUN_STATE_FAILED, Error: &failure, CreatedAt: timestamppb.New(created), StartedAt: timestamppb.New(started), FinishedAt: timestamppb.New(finished),
			},
		},
		{name: "not found", id: runID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: runID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "malformed id", id: "not-a-uuid", wantCode: connect.CodeInvalidArgument},
		{name: "unauthenticated", id: runID.String(), authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.authErr == nil && tc.wantCode != connect.CodeInvalidArgument {
				f.reader.EXPECT().GetRun(gomock.Any(), gen.GetRunParams{ID: runID, UserID: userID}).Return(tc.row, tc.err)
			}
			res, err := f.client.GetRun(context.Background(), connect.NewRequest(&runv1.GetRunRequest{RunId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("GetRun(%q) code = %v (err %v), want %v", tc.id, connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(&runv1.GetRunResponse{Run: tc.want}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("GetRun(%q) mismatch (-want +got):\n%s", tc.id, diff)
			}
		})
	}
}
