// Package refreshcorpacts serves stonks.admin.refresh_corp_acts.v1.
package refreshcorpacts

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	adminrefreshcorpactsv1 "github.com/leedenison/stonks/proto/admin/refresh_corp_acts/v1"
	"github.com/leedenison/stonks/proto/admin/refresh_corp_acts/v1/adminrefreshcorpactsv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	refresher "github.com/leedenison/stonks/server/internal/run/refreshcorpacts"
)

// Starter is this package's view of the refresh corporate actions starter.
type Starter interface {
	Start(ctx context.Context) (gen.Run, error)
}

var _ Starter = (*refresher.Starter)(nil)

//go:generate go tool mockgen -source=refreshcorpacts.go -destination=mock/refreshcorpacts_mock.go -package=mock

// Server implements RefreshCorpActsService.
type Server struct {
	runs Starter
}

var _ adminrefreshcorpactsv1connect.RefreshCorpActsServiceHandler = (*Server)(nil)

// New returns a Server.
func New(runs Starter) *Server {
	return &Server{runs: runs}
}

// StartRefreshCorpActs starts a refresh corporate actions run over the
// tickers that named a held instrument.
func (s *Server) StartRefreshCorpActs(ctx context.Context, _ *connect.Request[adminrefreshcorpactsv1.StartRefreshCorpActsRequest]) (*connect.Response[adminrefreshcorpactsv1.StartRefreshCorpActsResponse], error) {
	if _, err := auth.Admin(ctx); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	started, err := s.runs.Start(ctx)
	switch {
	case errors.Is(err, refresher.ErrEmpty):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminrefreshcorpactsv1.StartRefreshCorpActsResponse{Run: to.ProtoRun(started)}), nil
}
