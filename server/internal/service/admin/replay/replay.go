// Package replay serves stonks.admin.replay.v1.
package replay

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	adminreplayv1 "github.com/leedenison/stonks/proto/admin/replay/v1"
	"github.com/leedenison/stonks/proto/admin/replay/v1/adminreplayv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	replayer "github.com/leedenison/stonks/server/internal/run/replay"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	GetUserRun(ctx context.Context, id uuid.UUID) (gen.GetUserRunRow, error)
}

var _ Queries = (*gen.Queries)(nil)

// Replayer is this package's view of the replay service.
type Replayer interface {
	Start(ctx context.Context, admin uuid.UUID, source gen.Run, scope replayer.Scope) (gen.Run, error)
}

var _ Replayer = (*replayer.Service)(nil)

//go:generate go tool mockgen -source=replay.go -destination=mock/replay_mock.go -package=mock

// Server implements ReplayService.
type Server struct {
	queries Queries
	replays Replayer
}

var _ adminreplayv1connect.ReplayServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries, replays Replayer) *Server {
	return &Server{queries: queries, replays: replays}
}

// StartReplay starts a replay over the keys of a run, as a run of that run's
// user started by the caller.
func (s *Server) StartReplay(ctx context.Context, req *connect.Request[adminreplayv1.StartReplayRequest]) (*connect.Response[adminreplayv1.StartReplayResponse], error) {
	p, err := auth.Admin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	id, err := uuid.Parse(req.Msg.GetRunId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := s.queries.GetUserRun(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such run"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	scope := replayer.Scope{Datasource: req.Msg.GetDatasource()}
	started, err := s.replays.Start(ctx, p.User.ID, row.Run, scope)
	switch {
	case errors.Is(err, replayer.ErrKind), errors.Is(err, replayer.ErrEmpty), errors.Is(err, replayer.ErrDisabled), errors.Is(err, replayer.ErrNoIdentity):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminreplayv1.StartReplayResponse{Run: to.ProtoRun(started)}), nil
}
