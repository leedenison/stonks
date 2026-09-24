package admin

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

	adminv1 "github.com/leedenison/stonks/proto/admin/v1"
	"github.com/leedenison/stonks/proto/admin/v1/adminv1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	runID     = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	childID   = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	keyID     = uuid.MustParse("00000000-0000-0000-0000-000000000020")
	findingID = uuid.MustParse("00000000-0000-0000-0000-000000000030")
	blockID   = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	created   = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	principal = auth.Principal{User: gen.User{Email: "admin@example.com", Role: gen.UserRoleAdmin}, SessionID: servicetest.Session}
)

type fixture struct {
	reader *mock.MockReader
	client adminv1connect.AdminServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicemock.NewMockAuthenticator(ctrl)
	authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil).AnyTimes()
	f := &fixture{reader: mock.NewMockReader(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminv1connect.NewAdminServiceHandler(New(f.reader), opts...))
	})
	f.client = adminv1connect.NewAdminServiceClient(srv.Client, srv.URL)
	return f
}

func runRow(id uuid.UUID, kind gen.RunKind) gen.Run {
	return gen.Run{ID: id, UserID: userID, Kind: kind, Trigger: gen.RunTriggerUser, State: gen.RunStateCompleted, CreatedAt: created}
}

func runMsg(id uuid.UUID, kind runv1.RunKind) *adminv1.UserRun {
	return &adminv1.UserRun{
		Run: &runv1.Run{
			Id: id.String(), Kind: kind, Trigger: runv1.RunTrigger_RUN_TRIGGER_USER,
			State: runv1.RunState_RUN_STATE_COMPLETED, CreatedAt: timestamppb.New(created),
		},
		UserId: userID.String(), UserEmail: "one@example.com",
	}
}

func TestListRuns(t *testing.T) {
	first := uuid.MustParse("00000000-0000-0000-0000-000000000103")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000102")
	third := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	rows := func(ids ...uuid.UUID) []gen.ListUserRunsRow {
		var out []gen.ListUserRunsRow
		for _, id := range ids {
			out = append(out, gen.ListUserRunsRow{Run: runRow(id, gen.RunKindStatement), Email: "one@example.com"})
		}
		return out
	}
	statement := gen.RunKindStatement
	administrator := gen.RunTriggerAdministrator
	tests := []struct {
		name     string
		req      *adminv1.ListRunsRequest
		wantArg  gen.ListUserRunsParams
		rows     []gen.ListUserRunsRow
		err      error
		want     *adminv1.ListRunsResponse
		wantCode connect.Code
	}{
		{
			name:    "default page",
			req:     &adminv1.ListRunsRequest{},
			wantArg: gen.ListUserRunsParams{Lim: defaultPageSize + 1},
			rows:    rows(first),
			want:    &adminv1.ListRunsResponse{Runs: []*adminv1.UserRun{runMsg(first, runv1.RunKind_RUN_KIND_STATEMENT)}},
		},
		{
			name: "filters and a further page",
			req: &adminv1.ListRunsRequest{
				Kind: runv1.RunKind_RUN_KIND_STATEMENT.Enum(), Trigger: runv1.RunTrigger_RUN_TRIGGER_ADMINISTRATOR.Enum(),
				UserId: ptr.To(userID.String()), PageSize: 2, PageToken: runID.String(),
			},
			wantArg: gen.ListUserRunsParams{Kind: &statement, Trigger: &administrator, UserID: &userID, Before: &runID, Lim: 3},
			rows:    rows(first, second, third),
			want: &adminv1.ListRunsResponse{
				Runs:          []*adminv1.UserRun{runMsg(first, runv1.RunKind_RUN_KIND_STATEMENT), runMsg(second, runv1.RunKind_RUN_KIND_STATEMENT)},
				NextPageToken: second.String(),
			},
		},
		{name: "unspecified kind", req: &adminv1.ListRunsRequest{Kind: runv1.RunKind_RUN_KIND_UNSPECIFIED.Enum()}, wantCode: connect.CodeInvalidArgument},
		{name: "oversized page", req: &adminv1.ListRunsRequest{PageSize: 201}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed token", req: &adminv1.ListRunsRequest{PageToken: "next"}, wantCode: connect.CodeInvalidArgument},
		{name: "failure", req: &adminv1.ListRunsRequest{}, wantArg: gen.ListUserRunsParams{Lim: defaultPageSize + 1}, err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode != connect.CodeInvalidArgument {
				f.reader.EXPECT().ListUserRuns(gomock.Any(), tc.wantArg).Return(tc.rows, tc.err)
			}
			res, err := f.client.ListRuns(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListRuns(%v) code = %v (err %v), want %v", tc.req, connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListRuns(%v) mismatch (-want +got):\n%s", tc.req, diff)
			}
		})
	}
}

func TestGetRun(t *testing.T) {
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00B03MLX29"}
	key := gen.StatedKey{ID: keyID, Description: ptr.To("ROYAL DUTCH SHELL"), Identifiers: []types.Identifier{isin}}
	keyMsg := &typev1.StatedKey{
		Description: ptr.To("ROYAL DUTCH SHELL"),
		Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00B03MLX29"}},
	}
	tests := []struct {
		name   string
		kind   gen.RunKind
		expect func(r *mock.MockReaderMockRecorder)
		want   func(out *adminv1.GetRunResponse)
	}{
		{
			name: "statement",
			kind: gen.RunKindStatement,
			expect: func(r *mock.MockReaderMockRecorder) {
				r.ListStatementItems(gomock.Any(), gen.ListStatementItemsParams{StatementID: runID, UserID: userID}).
					Return([]gen.StatementItem{{Ordinal: 3, Reason: "no quantity", Stated: []byte(`{}`)}}, nil)
			},
			want: func(out *adminv1.GetRunResponse) {
				out.StatementItems = []*statementv1.StatementItem{{Ordinal: 3, Reason: "no quantity", Row: &statementv1.Row{}}}
			},
		},
		{
			name: "resolution",
			kind: gen.RunKindResolution,
			expect: func(r *mock.MockReaderMockRecorder) {
				r.ListResolutionItems(gomock.Any(), runID).Return([]gen.ListResolutionItemsRow{{
					ResolutionKey: gen.ResolutionKey{StatedKeyID: keyID, Outcome: gen.ResolutionOutcomeUnresolved}, StatedKey: key,
				}}, nil)
			},
			want: func(out *adminv1.GetRunResponse) {
				out.ResolutionItems = []*adminv1.ResolutionItem{{
					StatedKey: keyMsg, StatedKeyId: keyID.String(), Outcome: adminv1.ResolutionOutcome_RESOLUTION_OUTCOME_UNRESOLVED,
				}}
			},
		},
		{
			name: "fetch",
			kind: gen.RunKindFetch,
			expect: func(r *mock.MockReaderMockRecorder) {
				r.ListFetchItems(gomock.Any(), runID).Return([]gen.ListFetchItemsRow{{
					FetchKey: gen.FetchKey{
						StatedKeyID: keyID, Outcome: gen.FetchOutcomeFailedPermanent, Attempts: 1,
						SentType: &isin.Type, SentValue: &isin.Value, Reason: ptr.To("unknown identifier"),
					},
					StatedKey: key,
				}}, nil)
			},
			want: func(out *adminv1.GetRunResponse) {
				out.FetchItems = []*adminv1.FetchItem{{
					StatedKey: keyMsg, StatedKeyId: keyID.String(), Outcome: adminv1.FetchOutcome_FETCH_OUTCOME_FAILED_PERMANENT, Attempts: 1,
					Sent:   &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00B03MLX29"},
					Reason: ptr.To("unknown identifier"),
				}}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			r := f.reader.EXPECT()
			r.GetUserRun(gomock.Any(), runID).Return(gen.GetUserRunRow{Run: runRow(runID, tc.kind), Email: "one@example.com"}, nil)
			child := runRow(childID, gen.RunKindFetch)
			child.Trigger, child.ParentID = gen.RunTriggerRun, &runID
			r.ListChildRuns(gomock.Any(), gen.ListChildRunsParams{ParentID: &runID, UserID: userID}).Return([]gen.Run{child}, nil)
			r.ListRunFindings(gomock.Any(), runID).Return([]gen.Finding{{
				ID: findingID, RunID: runID, Kind: gen.FindingKindBlock, BlockID: &blockID, CreatedAt: created,
			}}, nil)
			tc.expect(r)

			res, err := f.client.GetRun(context.Background(), connect.NewRequest(&adminv1.GetRunRequest{RunId: runID.String()}))
			if err != nil {
				t.Fatalf("GetRun() error = %v", err)
			}
			want := &adminv1.GetRunResponse{
				Run: runMsg(runID, db.ToProto[runv1.RunKind](tc.kind)),
				Children: []*runv1.Run{{
					Id: childID.String(), Kind: runv1.RunKind_RUN_KIND_FETCH, Trigger: runv1.RunTrigger_RUN_TRIGGER_RUN,
					ParentId: ptr.To(runID.String()), State: runv1.RunState_RUN_STATE_COMPLETED, CreatedAt: timestamppb.New(created),
				}},
				Findings: []*adminv1.Finding{{
					Id: findingID.String(), RunId: runID.String(), Kind: adminv1.FindingKind_FINDING_KIND_BLOCK,
					BlockId: ptr.To(blockID.String()), CreatedAt: timestamppb.New(created),
				}},
			}
			tc.want(want)
			if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("GetRun() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetRunErrors(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		err      error
		wantCode connect.Code
	}{
		{name: "not found", id: runID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: runID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "malformed id", id: "not-a-uuid", wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode != connect.CodeInvalidArgument {
				f.reader.EXPECT().GetUserRun(gomock.Any(), runID).Return(gen.GetUserRunRow{}, tc.err)
			}
			_, err := f.client.GetRun(context.Background(), connect.NewRequest(&adminv1.GetRunRequest{RunId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("GetRun(%q) code = %v (err %v), want %v", tc.id, connect.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
