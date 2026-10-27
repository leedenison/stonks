// Package reasserttickers serves stonks.admin.reassert_tickers.v1.
package reasserttickers

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	adminreasserttickersv1 "github.com/leedenison/stonks/proto/admin/reassert_tickers/v1"
	"github.com/leedenison/stonks/proto/admin/reassert_tickers/v1/adminreasserttickersv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	reasserter "github.com/leedenison/stonks/server/internal/run/reasserttickers"
)

// Starter is this package's view of the reassert tickers service.
type Starter interface {
	Start(ctx context.Context) (gen.Run, error)
}

var _ Starter = (*reasserter.Service)(nil)

//go:generate go tool mockgen -source=reasserttickers.go -destination=mock/reasserttickers_mock.go -package=mock

// Server implements ReassertTickersService.
type Server struct {
	runs Starter
}

var _ adminreasserttickersv1connect.ReassertTickersServiceHandler = (*Server)(nil)

// New returns a Server.
func New(runs Starter) *Server {
	return &Server{runs: runs}
}

// StartReassertTickers starts a reassert tickers run over every held ticker.
func (s *Server) StartReassertTickers(ctx context.Context, _ *connect.Request[adminreasserttickersv1.StartReassertTickersRequest]) (*connect.Response[adminreasserttickersv1.StartReassertTickersResponse], error) {
	if _, err := auth.Admin(ctx); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	started, err := s.runs.Start(ctx)
	switch {
	case errors.Is(err, reasserter.ErrEmpty):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminreasserttickersv1.StartReassertTickersResponse{Run: to.ProtoRun(started)}), nil
}
