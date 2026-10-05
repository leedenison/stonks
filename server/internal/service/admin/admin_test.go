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
	"github.com/leedenison/stonks/server/internal/replay"
	"github.com/leedenison/stonks/server/internal/service/admin/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	userID   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	runID    = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	parentID = uuid.MustParse("00000000-0000-0000-0000-000000000009")
	childID  = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	// A second child of the run and a child of the first, so a listing shows
	// tree order: the first child's subtree before its sibling.
	siblingID           = uuid.MustParse("00000000-0000-0000-0000-000000000012")
	grandchildID        = uuid.MustParse("00000000-0000-0000-0000-000000000013")
	keyID               = uuid.MustParse("00000000-0000-0000-0000-000000000020")
	findingID           = uuid.MustParse("00000000-0000-0000-0000-000000000030")
	droppedID           = uuid.MustParse("00000000-0000-0000-0000-000000000033")
	siblingFindingID    = uuid.MustParse("00000000-0000-0000-0000-000000000035")
	grandchildFindingID = uuid.MustParse("00000000-0000-0000-0000-000000000036")
	fetchKeyID          = uuid.MustParse("00000000-0000-0000-0000-000000000034")
	blockID             = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	created             = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
	adminID             = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	principal           = auth.Principal{User: gen.User{ID: adminID, Email: "admin@example.com", Role: gen.UserRoleAdmin}, SessionID: servicetest.Session}
)

type fixture struct {
	reader  *mock.MockReader
	sources *mock.MockSources
	replays *mock.MockReplayer
	client  adminv1connect.AdminServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicemock.NewMockAuthenticator(ctrl)
	authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil).AnyTimes()
	f := &fixture{reader: mock.NewMockReader(ctrl), sources: mock.NewMockSources(ctrl), replays: mock.NewMockReplayer(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminv1connect.NewAdminServiceHandler(New(f.reader, f.sources, f.replays), opts...))
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
		UserId: userID.String(), UserEmail: "one@example.com", OpenFindings: 1, Matched: true,
	}
}

func TestListRuns(t *testing.T) {
	first := uuid.MustParse("00000000-0000-0000-0000-000000000103")
	second := uuid.MustParse("00000000-0000-0000-0000-000000000102")
	third := uuid.MustParse("00000000-0000-0000-0000-000000000101")
	rows := func(ids ...uuid.UUID) []gen.ListUserRunsRow {
		var out []gen.ListUserRunsRow
		for _, id := range ids {
			out = append(out, gen.ListUserRunsRow{Run: runRow(id, gen.RunKindStatement), Email: "one@example.com", OpenFindings: 1, RootID: id, Matched: true})
		}
		return out
	}
	// A resolution matched under a statement that was not: the statement is
	// the path to it.
	child := uuid.MustParse("00000000-0000-0000-0000-000000000104")
	childRow := runRow(child, gen.RunKindResolution)
	childRow.ParentID = &first
	nested := []gen.ListUserRunsRow{
		{Run: runRow(first, gen.RunKindStatement), Email: "one@example.com", OpenFindings: 1, RootID: first, Matched: false},
		{Run: childRow, Email: "one@example.com", OpenFindings: 1, RootID: first, Matched: true},
	}
	nestedMsg := runMsg(first, runv1.RunKind_RUN_KIND_STATEMENT)
	nestedMsg.Matched = false
	childMsg := runMsg(child, runv1.RunKind_RUN_KIND_RESOLUTION)
	childMsg.Run.ParentId = ptr.To(first.String())
	nestedMsg.Children = []*adminv1.UserRun{childMsg}
	statement := gen.RunKindStatement
	resolution := gen.RunKindResolution
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
		{
			name:    "a match nested under its unmatched parent",
			req:     &adminv1.ListRunsRequest{Kind: runv1.RunKind_RUN_KIND_RESOLUTION.Enum()},
			wantArg: gen.ListUserRunsParams{Kind: &resolution, Lim: defaultPageSize + 1},
			rows:    nested,
			want:    &adminv1.ListRunsResponse{Runs: []*adminv1.UserRun{nestedMsg}},
		},
		{
			name:    "a page is cut between top-level runs",
			req:     &adminv1.ListRunsRequest{PageSize: 1},
			wantArg: gen.ListUserRunsParams{Lim: 2},
			rows:    append(append([]gen.ListUserRunsRow{}, nested...), rows(second)...),
			want:    &adminv1.ListRunsResponse{Runs: []*adminv1.UserRun{nestedMsg}, NextPageToken: first.String()},
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
	shell := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ROYAL DUTCH SHELL"}
	key := gen.StatedKey{ID: keyID, Identifiers: []types.Identifier{shell, isin}}
	keyMsg := &typev1.StatedKey{
		Identifiers: []*typev1.Identifier{
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ROYAL DUTCH SHELL"},
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00B03MLX29"},
		},
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
					ResolutionKey: gen.ResolutionKey{StatedKeyID: keyID, Outcome: gen.ResolutionOutcomeUnrecognised}, StatedKey: key,
				}}, nil)
			},
			want: func(out *adminv1.GetRunResponse) {
				out.ResolutionItems = []*typev1.ResolutionItem{{
					StatedKey: keyMsg, StatedKeyId: keyID.String(), Outcome: typev1.ResolutionOutcome_RESOLUTION_OUTCOME_UNRECOGNISED,
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
		{
			name: "replay",
			kind: gen.RunKindReplay,
			expect: func(r *mock.MockReaderMockRecorder) {
				r.GetReplay(gomock.Any(), runID).Return(gen.GetReplayRow{
					Replay: gen.Replay{ID: runID, UserID: userID, SourceID: parentID, Datasource: ptr.To("openfigi"), StartedBy: adminID}, StartedByEmail: "admin@example.com",
				}, nil)
			},
			want: func(out *adminv1.GetRunResponse) {
				out.Replay = &adminv1.Replay{SourceRunId: parentID.String(), Datasource: "openfigi", StartedBy: "admin@example.com"}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			r := f.reader.EXPECT()
			r.GetUserRun(gomock.Any(), runID).Return(gen.GetUserRunRow{Run: runRow(runID, tc.kind), Email: "one@example.com", OpenFindings: 1}, nil)
			parent := runRow(parentID, gen.RunKindStatement)
			r.ListRunAncestors(gomock.Any(), runID).Return([]gen.ListRunAncestorsRow{{Run: parent, Email: "one@example.com", OpenFindings: 1}}, nil)
			below := func(id, parent uuid.UUID) gen.ListRunDescendantsRow {
				row := runRow(id, gen.RunKindFetch)
				row.Trigger, row.ParentID = gen.RunTriggerRun, &parent
				return gen.ListRunDescendantsRow{Run: row, Email: "one@example.com", OpenFindings: 1}
			}
			r.ListRunDescendants(gomock.Any(), &runID).Return([]gen.ListRunDescendantsRow{
				below(childID, runID), below(siblingID, runID), below(grandchildID, childID),
			}, nil)
			// Returned oldest first, so the sibling's finding precedes the
			// grandchild's until the handler orders them.
			r.ListRunFindings(gomock.Any(), []uuid.UUID{runID, childID, siblingID, grandchildID}).Return([]gen.ListRunFindingsRow{
				{
					Finding:     gen.Finding{ID: findingID, RunID: runID, Kind: gen.FindingKindBlock, BlockID: &blockID, CreatedAt: created},
					BlockReason: ptr.To("openfigi rejected the identifier: Invalid idValue format."),
					KeyID:       &keyID,
					KeyIdentifiers: []types.Identifier{
						{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ROYAL DUTCH SHELL A"},
						{Type: types.IdentifierTypeBrokerID, Domain: "ibkr", Value: "100000001"},
						{Type: types.IdentifierTypeIsin, Value: "GB00B03MLX29"},
					},
					KeyAssetClass: ptr.To(gen.AssetClassEquity),
					KeyCurrency:   ptr.To("GBP"),
				},
				{
					Finding: gen.Finding{ID: droppedID, RunID: childID, Kind: gen.FindingKindDropped, StatedKeyID: &keyID, FetchKeyID: &fetchKeyID,
						Step: ptr.To(gen.DropStepStated), Detail: ptr.To("candidates in USD, not the stated GBP"), CreatedAt: created},
					KeyID:          &keyID,
					KeyIdentifiers: []types.Identifier{{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "Cash fund"}},
				},
				{Finding: gen.Finding{ID: siblingFindingID, RunID: siblingID, Kind: gen.FindingKindBlock, BlockID: &blockID, CreatedAt: created}},
				{Finding: gen.Finding{ID: grandchildFindingID, RunID: grandchildID, Kind: gen.FindingKindBlock, BlockID: &blockID, CreatedAt: created}},
			}, nil)
			tc.expect(r)

			res, err := f.client.GetRun(context.Background(), connect.NewRequest(&adminv1.GetRunRequest{RunId: runID.String()}))
			if err != nil {
				t.Fatalf("GetRun() error = %v", err)
			}
			self := runMsg(runID, types.ToProto[runv1.RunKind](tc.kind))
			belowMsg := func(id, parent uuid.UUID) *adminv1.UserRun {
				msg := runMsg(id, runv1.RunKind_RUN_KIND_FETCH)
				msg.Run.Trigger, msg.Run.ParentId = runv1.RunTrigger_RUN_TRIGGER_RUN, ptr.To(parent.String())
				return msg
			}
			childMsg := belowMsg(childID, runID)
			childMsg.Children = []*adminv1.UserRun{belowMsg(grandchildID, childID)}
			self.Children = []*adminv1.UserRun{childMsg, belowMsg(siblingID, runID)}
			want := &adminv1.GetRunResponse{
				Run:       self,
				Ancestors: []*adminv1.UserRun{runMsg(parentID, runv1.RunKind_RUN_KIND_STATEMENT)},
				Findings: []*adminv1.Finding{
					{
						Id: findingID.String(), RunId: runID.String(), Kind: adminv1.FindingKind_FINDING_KIND_BLOCK,
						BlockId: ptr.To(blockID.String()), Detail: ptr.To("openfigi rejected the identifier: Invalid idValue format."),
						StatedKey: &typev1.StatedKey{
							Identifiers: []*typev1.Identifier{
								{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ROYAL DUTCH SHELL A"},
								{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_ID, Domain: "ibkr", Value: "100000001"},
								{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00B03MLX29"},
							},
							AssetClass: typev1.AssetClass_ASSET_CLASS_EQUITY, Currency: ptr.To("GBP"),
						},
						CreatedAt: timestamppb.New(created),
					},
					{
						Id: droppedID.String(), RunId: childID.String(), Kind: adminv1.FindingKind_FINDING_KIND_DROPPED,
						StatedKeyId: ptr.To(keyID.String()), StatedKey: &typev1.StatedKey{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "Cash fund"}}}, FetchKeyId: ptr.To(fetchKeyID.String()),
						Step: ptr.To(adminv1.DropStep_DROP_STEP_STATED), Detail: ptr.To("candidates in USD, not the stated GBP"),
						CreatedAt: timestamppb.New(created),
					},
					{
						Id: grandchildFindingID.String(), RunId: grandchildID.String(), Kind: adminv1.FindingKind_FINDING_KIND_BLOCK,
						BlockId: ptr.To(blockID.String()), CreatedAt: timestamppb.New(created),
					},
					{
						Id: siblingFindingID.String(), RunId: siblingID.String(), Kind: adminv1.FindingKind_FINDING_KIND_BLOCK,
						BlockId: ptr.To(blockID.String()), CreatedAt: timestamppb.New(created),
					},
				},
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

func TestStartReplay(t *testing.T) {
	source := runRow(runID, gen.RunKindStatement)
	started := gen.Run{ID: childID, UserID: userID, Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: created}
	unavailable := &adminv1.StartReplayRequest{RunId: runID.String(), Scope: &adminv1.StartReplayRequest_Unavailable{Unavailable: true}}
	datasource := &adminv1.StartReplayRequest{RunId: runID.String(), Scope: &adminv1.StartReplayRequest_Datasource{Datasource: "openfigi"}}
	tests := []struct {
		name      string
		req       *adminv1.StartReplayRequest
		readErr   error
		wantScope *replay.Scope
		startErr  error
		wantCode  connect.Code
	}{
		{name: "the keys left unavailable", req: unavailable, wantScope: &replay.Scope{}},
		{name: "the keys a datasource has not answered", req: datasource, wantScope: &replay.Scope{Datasource: "openfigi"}},
		{name: "no such run", req: unavailable, readErr: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "a run without keys", req: unavailable, wantScope: &replay.Scope{}, startErr: replay.ErrKind, wantCode: connect.CodeFailedPrecondition},
		{name: "nothing to replay", req: unavailable, wantScope: &replay.Scope{}, startErr: replay.ErrEmpty, wantCode: connect.CodeFailedPrecondition},
		{name: "a datasource not enabled", req: datasource, wantScope: &replay.Scope{Datasource: "openfigi"}, startErr: replay.ErrDisabled, wantCode: connect.CodeFailedPrecondition},
		{name: "a datasource serving no identity", req: datasource, wantScope: &replay.Scope{Datasource: "openfigi"}, startErr: replay.ErrNoIdentity, wantCode: connect.CodeFailedPrecondition},
		{name: "failure", req: unavailable, wantScope: &replay.Scope{}, startErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "no scope", req: &adminv1.StartReplayRequest{RunId: runID.String()}, wantCode: connect.CodeInvalidArgument},
		{name: "unavailable unset", req: &adminv1.StartReplayRequest{RunId: runID.String(), Scope: &adminv1.StartReplayRequest_Unavailable{}}, wantCode: connect.CodeInvalidArgument},
		{name: "an empty datasource", req: &adminv1.StartReplayRequest{RunId: runID.String(), Scope: &adminv1.StartReplayRequest_Datasource{}}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed run id", req: &adminv1.StartReplayRequest{RunId: "not-a-uuid", Scope: &adminv1.StartReplayRequest_Unavailable{Unavailable: true}}, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode != connect.CodeInvalidArgument {
				f.reader.EXPECT().GetUserRun(gomock.Any(), runID).Return(gen.GetUserRunRow{Run: source, Email: "one@example.com"}, tc.readErr)
			}
			if tc.wantScope != nil {
				f.replays.EXPECT().Start(gomock.Any(), adminID, source, *tc.wantScope).Return(started, tc.startErr)
			}
			res, err := f.client.StartReplay(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("StartReplay(%s) code = %v (err %v), want %v", tc.name, connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != childID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_REPLAY || got.GetTrigger() != runv1.RunTrigger_RUN_TRIGGER_ADMINISTRATOR || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("StartReplay(%s) = %v, want the pending replay run", tc.name, got)
			}
		})
	}
}

func TestClearFinding(t *testing.T) {
	tests := []struct {
		name     string
		row      gen.Finding
		err      error
		clears   bool
		wantCode connect.Code
	}{
		{name: "reports nothing withheld", row: gen.Finding{ID: findingID}, clears: true},
		{name: "reports a block", row: gen.Finding{ID: findingID, BlockID: &blockID}, wantCode: connect.CodeFailedPrecondition},
		{name: "not found", err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.reader.EXPECT().GetFinding(gomock.Any(), findingID).Return(tc.row, tc.err)
			if tc.clears {
				f.reader.EXPECT().ClearFinding(gomock.Any(), findingID).Return(nil)
			}
			_, err := f.client.ClearFinding(context.Background(), connect.NewRequest(&adminv1.ClearFindingRequest{FindingId: findingID.String()}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("ClearFinding() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}

func TestListDatasources(t *testing.T) {
	f := newFixture(t)
	f.reader.EXPECT().ListDatasourceSettings(gomock.Any()).Return([]gen.ListDatasourceSettingsRow{
		{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), HasCredential: true},
		{Name: "other", Precedence: 20},
	}, nil)
	res, err := f.client.ListDatasources(context.Background(), connect.NewRequest(&adminv1.ListDatasourcesRequest{}))
	if err != nil {
		t.Fatalf("ListDatasources() error = %v", err)
	}
	want := &adminv1.ListDatasourcesResponse{Datasources: []*adminv1.Datasource{
		{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), HasCredential: true},
		{Name: "other", Precedence: 20},
	}}
	if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
		t.Errorf("ListDatasources() mismatch (-want +got):\n%s", diff)
	}
}

// TestUpdateDatasource checks that a change is written and the registry
// reloaded, that the credential never comes back, and what is refused.
func TestUpdateDatasource(t *testing.T) {
	tests := []struct {
		name     string
		req      *adminv1.UpdateDatasourceRequest
		carries  bool
		wantArg  *gen.UpdateDatasourceParams
		row      gen.Datasource
		err      error
		want     *adminv1.Datasource
		wantCode connect.Code
	}{
		{
			name:    "enabled with an endpoint and a credential",
			req:     &adminv1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true, Endpoint: ptr.To("http://stub"), Credential: ptr.To("secret")},
			carries: true,
			wantArg: &gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Endpoint: ptr.To("http://stub"), Credential: ptr.To("secret")},
			row:     gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Credential: ptr.To("secret")},
			want:    &adminv1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), HasCredential: true},
		},
		{
			name:    "disabled with the endpoint cleared",
			req:     &adminv1.UpdateDatasourceRequest{Name: "absent", Endpoint: ptr.To("")},
			wantArg: &gen.UpdateDatasourceParams{Name: "absent"},
			row:     gen.Datasource{Name: "absent", Precedence: 20},
			want:    &adminv1.Datasource{Name: "absent", Precedence: 20},
		},
		{
			name:     "enabling an integration the build lacks",
			req:      &adminv1.UpdateDatasourceRequest{Name: "absent", Enabled: true},
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "no such datasource",
			req:      &adminv1.UpdateDatasourceRequest{Name: "gone"},
			wantArg:  &gen.UpdateDatasourceParams{Name: "gone"},
			err:      db.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.sources.EXPECT().Carries(tc.req.GetName()).Return(tc.carries).AnyTimes()
			if tc.wantArg != nil {
				f.reader.EXPECT().UpdateDatasource(gomock.Any(), *tc.wantArg).Return(tc.row, tc.err)
			}
			if tc.want != nil {
				f.sources.EXPECT().Reload(gomock.Any()).Return(nil)
			}
			res, err := f.client.UpdateDatasource(context.Background(), connect.NewRequest(tc.req))
			if (err != nil) != (tc.wantCode != 0) || (err != nil && connect.CodeOf(err) != tc.wantCode) {
				t.Fatalf("UpdateDatasource() error = %v, want code %v", err, tc.wantCode)
			}
			if tc.want == nil {
				return
			}
			if diff := cmp.Diff(&adminv1.UpdateDatasourceResponse{Datasource: tc.want}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("UpdateDatasource() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReorderDatasources checks that the position becomes the precedence and
// that a list naming the datasources wrongly is refused.
func TestReorderDatasources(t *testing.T) {
	rows := []gen.Datasource{{Name: "alpha", Precedence: 1}, {Name: "beta", Precedence: 2}}
	tests := []struct {
		name     string
		names    []string
		wantArg  *gen.SetDatasourcePrecedenceParams
		wantCode connect.Code
	}{
		{name: "reversed", names: []string{"beta", "alpha"}, wantArg: &gen.SetDatasourcePrecedenceParams{Names: []string{"beta", "alpha"}, Precedences: []int32{1, 2}}},
		{name: "one named twice", names: []string{"beta", "beta"}, wantCode: connect.CodeInvalidArgument},
		{name: "one left out", names: []string{"beta"}, wantCode: connect.CodeInvalidArgument},
		{name: "one that is no datasource", names: []string{"beta", "alpha", "gamma"}, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.reader.EXPECT().ListDatasources(gomock.Any()).Return(rows, nil)
			if tc.wantArg != nil {
				f.reader.EXPECT().SetDatasourcePrecedence(gomock.Any(), *tc.wantArg).Return(nil)
				f.sources.EXPECT().Reload(gomock.Any()).Return(nil)
			}
			_, err := f.client.ReorderDatasources(context.Background(), connect.NewRequest(&adminv1.ReorderDatasourcesRequest{Names: tc.names}))
			if (err != nil) != (tc.wantCode != 0) || (err != nil && connect.CodeOf(err) != tc.wantCode) {
				t.Errorf("ReorderDatasources(%v) error = %v, want code %v", tc.names, err, tc.wantCode)
			}
		})
	}
}

func TestListBlocks(t *testing.T) {
	isin := types.IdentifierTypeIsin
	f := newFixture(t)
	f.reader.EXPECT().ListBlocks(gomock.Any(), gen.ListBlocksParams{Before: &findingID, Lim: defaultPageSize + 1}).Return([]gen.ListBlocksRow{
		{DatasourceBlock: gen.DatasourceBlock{
			ID: blockID, Datasource: "openfigi", Kind: gen.FetchKindIdentity, Scope: gen.BlockScopeIdentifier,
			SentType: &isin, SentValue: ptr.To("GB00B03MLX29"), Reason: "unknown identifier", CreatedAt: created,
		}, FetchID: childID},
		{DatasourceBlock: gen.DatasourceBlock{
			ID: keyID, Datasource: "openfigi", Kind: gen.FetchKindIdentity, Scope: gen.BlockScopeDatasource,
			Reason: "quota spent", CreatedAt: created,
		}, FetchID: childID},
	}, nil)
	res, err := f.client.ListBlocks(context.Background(), connect.NewRequest(&adminv1.ListBlocksRequest{PageToken: findingID.String()}))
	if err != nil {
		t.Fatalf("ListBlocks() error = %v", err)
	}
	want := &adminv1.ListBlocksResponse{Blocks: []*adminv1.Block{
		{
			Id: blockID.String(), Datasource: "openfigi", Kind: adminv1.FetchKind_FETCH_KIND_IDENTITY, Scope: adminv1.BlockScope_BLOCK_SCOPE_IDENTIFIER,
			Sent:   &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00B03MLX29"},
			Reason: "unknown identifier", RunId: childID.String(), CreatedAt: timestamppb.New(created),
		},
		{
			Id: keyID.String(), Datasource: "openfigi", Kind: adminv1.FetchKind_FETCH_KIND_IDENTITY, Scope: adminv1.BlockScope_BLOCK_SCOPE_DATASOURCE,
			Reason: "quota spent", RunId: childID.String(), CreatedAt: timestamppb.New(created),
		},
	}}
	if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
		t.Errorf("ListBlocks() mismatch (-want +got):\n%s", diff)
	}
}

func TestClearBlock(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		clears   bool
		wantCode connect.Code
	}{
		{name: "found", clears: true},
		{name: "not found", err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.reader.EXPECT().GetDatasourceBlock(gomock.Any(), blockID).Return(gen.DatasourceBlock{ID: blockID}, tc.err)
			if tc.clears {
				f.reader.EXPECT().ClearDatasourceBlock(gomock.Any(), blockID).Return(nil)
			}
			_, err := f.client.ClearBlock(context.Background(), connect.NewRequest(&adminv1.ClearBlockRequest{BlockId: blockID.String()}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("ClearBlock() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
