// Package rebuildresolvedtickers serves stonks.admin.rebuild_resolved_tickers.v1.
package rebuildresolvedtickers

import (
	"context"

	"connectrpc.com/connect"

	adminrebuildresolvedtickersv1 "github.com/leedenison/stonks/proto/admin/rebuild_resolved_tickers/v1"
	"github.com/leedenison/stonks/proto/admin/rebuild_resolved_tickers/v1/adminrebuildresolvedtickersv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	rebuilder "github.com/leedenison/stonks/server/internal/run/rebuildresolvedtickers"
)

// Starter is this package's view of the rebuild resolved tickers service.
type Starter interface {
	Start(ctx context.Context) (gen.Run, error)
}

var _ Starter = (*rebuilder.Service)(nil)

//go:generate go tool mockgen -source=rebuildresolvedtickers.go -destination=mock/rebuildresolvedtickers_mock.go -package=mock

// Server implements RebuildResolvedTickersService.
type Server struct {
	runs Starter
}

var _ adminrebuildresolvedtickersv1connect.RebuildResolvedTickersServiceHandler = (*Server)(nil)

// New returns a Server.
func New(runs Starter) *Server {
	return &Server{runs: runs}
}

// StartRebuildResolvedTickers starts a rebuild resolved tickers run.
func (s *Server) StartRebuildResolvedTickers(ctx context.Context, _ *connect.Request[adminrebuildresolvedtickersv1.StartRebuildResolvedTickersRequest]) (*connect.Response[adminrebuildresolvedtickersv1.StartRebuildResolvedTickersResponse], error) {
	if _, err := auth.Admin(ctx); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	started, err := s.runs.Start(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminrebuildresolvedtickersv1.StartRebuildResolvedTickersResponse{Run: to.ProtoRun(started)}), nil
}
