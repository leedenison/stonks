// Package refreshcorporateevents serves stonks.admin.refresh_corporate_events.v1.
package refreshcorporateevents

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	adminrefreshcorporateeventsv1 "github.com/leedenison/stonks/proto/admin/refresh_corporate_events/v1"
	"github.com/leedenison/stonks/proto/admin/refresh_corporate_events/v1/adminrefreshcorporateeventsv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	refresher "github.com/leedenison/stonks/server/internal/run/refreshcorporateevents"
)

// Starter is this package's view of the refresh corporate events starter.
type Starter interface {
	Start(ctx context.Context) (gen.Run, error)
}

var _ Starter = (*refresher.Starter)(nil)

//go:generate go tool mockgen -source=refreshcorporateevents.go -destination=mock/refreshcorporateevents_mock.go -package=mock

// Server implements RefreshCorporateEventsService.
type Server struct {
	runs Starter
}

var _ adminrefreshcorporateeventsv1connect.RefreshCorporateEventsServiceHandler = (*Server)(nil)

// New returns a Server.
func New(runs Starter) *Server {
	return &Server{runs: runs}
}

// StartRefreshCorporateEvents starts a refresh corporate events run over the
// tickers that named a held instrument.
func (s *Server) StartRefreshCorporateEvents(ctx context.Context, _ *connect.Request[adminrefreshcorporateeventsv1.StartRefreshCorporateEventsRequest]) (*connect.Response[adminrefreshcorporateeventsv1.StartRefreshCorporateEventsResponse], error) {
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
	return connect.NewResponse(&adminrefreshcorporateeventsv1.StartRefreshCorporateEventsResponse{Run: to.ProtoRun(started)}), nil
}
