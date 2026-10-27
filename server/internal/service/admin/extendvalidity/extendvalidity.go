// Package extendvalidity serves stonks.admin.extend_validity.v1.
package extendvalidity

import (
	"context"
	"errors"

	"connectrpc.com/connect"

	adminextendvalidityv1 "github.com/leedenison/stonks/proto/admin/extend_validity/v1"
	"github.com/leedenison/stonks/proto/admin/extend_validity/v1/adminextendvalidityv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	extender "github.com/leedenison/stonks/server/internal/run/extendvalidity"
)

// Starter is this package's view of the extend validity service.
type Starter interface {
	Start(ctx context.Context) (gen.Run, error)
}

var _ Starter = (*extender.Service)(nil)

//go:generate go tool mockgen -source=extendvalidity.go -destination=mock/extendvalidity_mock.go -package=mock

// Server implements ExtendValidityService.
type Server struct {
	runs Starter
}

var _ adminextendvalidityv1connect.ExtendValidityServiceHandler = (*Server)(nil)

// New returns a Server.
func New(runs Starter) *Server {
	return &Server{runs: runs}
}

// StartExtendValidity starts an extend validity run over every held ticker.
func (s *Server) StartExtendValidity(ctx context.Context, _ *connect.Request[adminextendvalidityv1.StartExtendValidityRequest]) (*connect.Response[adminextendvalidityv1.StartExtendValidityResponse], error) {
	if _, err := auth.Admin(ctx); err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	started, err := s.runs.Start(ctx)
	switch {
	case errors.Is(err, extender.ErrEmpty):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminextendvalidityv1.StartExtendValidityResponse{Run: to.ProtoRun(started)}), nil
}
