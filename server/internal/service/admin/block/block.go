// Package block serves stonks.admin.block.v1.
package block

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	"github.com/leedenison/stonks/proto/admin/block/v1/adminblockv1connect"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListBlocks(ctx context.Context, arg gen.ListBlocksParams) ([]gen.ListBlocksRow, error)
	ClearDatasourceBlock(ctx context.Context, id uuid.UUID) (uuid.UUID, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=block.go -destination=block_mock_test.go -package=block

// Server implements BlockService.
type Server struct {
	queries Queries
}

var _ adminblockv1connect.BlockServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries) *Server {
	return &Server{queries: queries}
}

// ListBlocks lists datasource blocks, newest first. The page token is the
// id of the last block served.
func (s *Server) ListBlocks(ctx context.Context, req *connect.Request[adminblockv1.ListBlocksRequest]) (*connect.Response[adminblockv1.ListBlocksResponse], error) {
	m := req.Msg
	before, err := page.Before(m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	p := page.New(m.GetPageSize())
	rows, err := s.queries.ListBlocks(ctx, gen.ListBlocksParams{IncludeCleared: m.GetIncludeCleared(), Before: before, Lim: p.Lim()})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminblockv1.ListBlocksResponse{}
	rows, more := page.Cut(p, rows)
	for _, r := range rows {
		out.Blocks = append(out.Blocks, block(r.DatasourceBlock, r.FetchID))
	}
	if more {
		out.NextPageToken = rows[len(rows)-1].DatasourceBlock.ID.String()
	}
	return connect.NewResponse(out), nil
}

// ClearBlock clears a block and the finding reporting it.
func (s *Server) ClearBlock(ctx context.Context, req *connect.Request[adminblockv1.ClearBlockRequest]) (*connect.Response[adminblockv1.ClearBlockResponse], error) {
	id, err := uuid.Parse(req.Msg.GetBlockId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := s.queries.ClearDatasourceBlock(ctx, id); errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such block"))
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminblockv1.ClearBlockResponse{}), nil
}

func block(b gen.DatasourceBlock, runID uuid.UUID) *adminblockv1.Block {
	out := &adminblockv1.Block{
		Id:         b.ID.String(),
		Datasource: b.Datasource,
		Kind:       types.ToProto[adminblockv1.FetchKind](b.Kind),
		Scope:      types.ToProto[adminblockv1.BlockScope](b.Scope),
		Reason:     b.Reason,
		RunId:      runID.String(),
		CreatedAt:  timestamppb.New(b.CreatedAt),
	}
	if sent, ok := to.BlockSent(b); ok {
		out.Sent = sent.ToProto()
	}
	if b.ClearedAt != nil {
		out.ClearedAt = timestamppb.New(*b.ClearedAt)
	}
	return out
}
