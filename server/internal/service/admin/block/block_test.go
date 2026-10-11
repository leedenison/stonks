package block

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

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	"github.com/leedenison/stonks/proto/admin/block/v1/adminblockv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	fetchID = uuid.MustParse("00000000-0000-0000-0000-000000000011")
	blockID = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	otherID = uuid.MustParse("00000000-0000-0000-0000-000000000032")
	created = time.Date(2026, 9, 24, 12, 0, 0, 0, time.UTC)
)

type fixture struct {
	queries *MockQueries
	client  adminblockv1connect.BlockServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicetest.AsAdmin(ctrl)
	f := &fixture{queries: NewMockQueries(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminblockv1connect.NewBlockServiceHandler(New(f.queries), opts...))
	})
	f.client = adminblockv1connect.NewBlockServiceClient(srv.Client, srv.URL)
	return f
}

func blockRow(id uuid.UUID, scope gen.BlockScope, reason string) gen.ListBlocksRow {
	return gen.ListBlocksRow{DatasourceBlock: gen.DatasourceBlock{
		ID: id, Datasource: "openfigi", Kind: gen.FetchKindIdentity, Scope: scope, Reason: reason, CreatedAt: created,
	}, FetchID: fetchID}
}

func TestListBlocks(t *testing.T) {
	isin := types.IdentifierTypeIsin
	identifier := blockRow(blockID, gen.BlockScopeIdentifier, "unknown identifier")
	identifier.DatasourceBlock.SentType = &isin
	identifier.DatasourceBlock.SentValue = ptr.To("GB00B03MLX29")
	tests := []struct {
		name     string
		req      *adminblockv1.ListBlocksRequest
		wantArg  *gen.ListBlocksParams
		rows     []gen.ListBlocksRow
		want     *adminblockv1.ListBlocksResponse
		wantCode connect.Code
	}{
		{
			name:    "a page after a token",
			req:     &adminblockv1.ListBlocksRequest{PageToken: fetchID.String()},
			wantArg: &gen.ListBlocksParams{Before: &fetchID, Lim: page.DefaultSize + 1},
			rows:    []gen.ListBlocksRow{identifier, blockRow(otherID, gen.BlockScopeDatasource, "quota spent")},
			want: &adminblockv1.ListBlocksResponse{Blocks: []*adminblockv1.Block{
				{
					Id: blockID.String(), Datasource: "openfigi", Kind: adminblockv1.FetchKind_FETCH_KIND_IDENTITY, Scope: adminblockv1.BlockScope_BLOCK_SCOPE_IDENTIFIER,
					Sent:   &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB00B03MLX29"},
					Reason: "unknown identifier", RunId: fetchID.String(), CreatedAt: timestamppb.New(created),
				},
				{
					Id: otherID.String(), Datasource: "openfigi", Kind: adminblockv1.FetchKind_FETCH_KIND_IDENTITY, Scope: adminblockv1.BlockScope_BLOCK_SCOPE_DATASOURCE,
					Reason: "quota spent", RunId: fetchID.String(), CreatedAt: timestamppb.New(created),
				},
			}},
		},
		{
			name:    "the cleared included",
			req:     &adminblockv1.ListBlocksRequest{IncludeCleared: true},
			wantArg: &gen.ListBlocksParams{IncludeCleared: true, Lim: page.DefaultSize + 1},
			want:    &adminblockv1.ListBlocksResponse{},
		},
		{name: "oversized page", req: &adminblockv1.ListBlocksRequest{PageSize: 201}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed token", req: &adminblockv1.ListBlocksRequest{PageToken: "not-a-uuid"}, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantArg != nil {
				f.queries.EXPECT().ListBlocks(gomock.Any(), *tc.wantArg).Return(tc.rows, nil)
			}
			res, err := f.client.ListBlocks(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListBlocks(%v) code = %v (err %v), want %v", tc.req, servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListBlocks(%v) mismatch (-want +got):\n%s", tc.req, diff)
			}
		})
	}
}

// TestListBlocksPages checks that a full page is cut to its size and that
// the token of the next page is the id of the last block served.
func TestListBlocksPages(t *testing.T) {
	f := newFixture(t)
	rows := []gen.ListBlocksRow{blockRow(blockID, gen.BlockScopeDatasource, "a"), blockRow(otherID, gen.BlockScopeDatasource, "b")}
	f.queries.EXPECT().ListBlocks(gomock.Any(), gen.ListBlocksParams{Lim: 2}).Return(rows, nil)
	res, err := f.client.ListBlocks(context.Background(), connect.NewRequest(&adminblockv1.ListBlocksRequest{PageSize: 1}))
	if err != nil {
		t.Fatalf("ListBlocks() error = %v", err)
	}
	if len(res.Msg.GetBlocks()) != 1 || res.Msg.GetBlocks()[0].GetId() != blockID.String() || res.Msg.GetNextPageToken() != blockID.String() {
		t.Errorf("ListBlocks() = %v, want the first block and its id as the next token", res.Msg)
	}
}

func TestClearBlock(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		err      error
		wantCode connect.Code
	}{
		{name: "found", id: blockID.String()},
		{name: "not found", id: blockID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: blockID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "malformed id", id: "not-a-uuid", wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode != connect.CodeInvalidArgument {
				f.queries.EXPECT().ClearDatasourceBlock(gomock.Any(), blockID).Return(blockID, tc.err)
			}
			_, err := f.client.ClearBlock(context.Background(), connect.NewRequest(&adminblockv1.ClearBlockRequest{BlockId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("ClearBlock(%q) code = %v (err %v), want %v", tc.id, servicetest.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
