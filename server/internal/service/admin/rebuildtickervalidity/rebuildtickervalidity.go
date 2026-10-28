// Package rebuildtickervalidity serves stonks.admin.rebuild_ticker_validity.v1.
package rebuildtickervalidity

import (
	"context"

	"connectrpc.com/connect"

	adminrebuildtickervalidityv1 "github.com/leedenison/stonks/proto/admin/rebuild_ticker_validity/v1"
	"github.com/leedenison/stonks/proto/admin/rebuild_ticker_validity/v1/adminrebuildtickervalidityv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	rebuilder "github.com/leedenison/stonks/server/internal/run/rebuildtickervalidity"
)

// Starter is this package's view of the rebuild ticker validity service.
type Starter interface {
	Start(ctx context.Context) (gen.Run, error)
}

var _ Starter = (*rebuilder.Service)(nil)

//go:generate go tool mockgen -source=rebuildtickervalidity.go -destination=mock/rebuildtickervalidity_mock.go -package=mock

// Server implements RebuildTickerValidityService.
type Server struct {
	runs Starter
}

var _ adminrebuildtickervalidityv1connect.RebuildTickerValidityServiceHandler = (*Server)(nil)

// New returns a Server.
func New(runs Starter) *Server {
	return &Server{runs: runs}
}

// StartRebuildTickerValidity starts a rebuild ticker validity run.
func (s *Server) StartRebuildTickerValidity(ctx context.Context, _ *connect.Request[adminrebuildtickervalidityv1.StartRebuildTickerValidityRequest]) (*connect.Response[adminrebuildtickervalidityv1.StartRebuildTickerValidityResponse], error) {
	if _, err := auth.Admin(ctx); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	started, err := s.runs.Start(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminrebuildtickervalidityv1.StartRebuildTickerValidityResponse{Run: to.ProtoRun(started)}), nil
}
