// Package run serves stonks.run.v1.
//
// A run is read with the caller's user id in the query, so where another
// user started a run, it is not found rather than forbidden.
package run

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/proto/run/v1/runv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
)

// Reader is this package's view of the run queries.
type Reader interface {
	GetRun(ctx context.Context, arg gen.GetRunParams) (gen.Run, error)
}

var _ Reader = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=run.go -destination=mock/run_mock.go -package=mock

// Server implements RunService.
type Server struct {
	reader Reader
}

var _ runv1connect.RunServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader) *Server {
	return &Server{reader: reader}
}

// GetRun reads a run the caller started.
func (s *Server) GetRun(ctx context.Context, req *connect.Request[runv1.GetRunRequest]) (*connect.Response[runv1.GetRunResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	id, err := uuid.Parse(req.Msg.GetRunId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := s.reader.GetRun(ctx, gen.GetRunParams{ID: id, UserID: p.User.ID})
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such run"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&runv1.GetRunResponse{Run: to.ProtoRun(row)}), nil
}
