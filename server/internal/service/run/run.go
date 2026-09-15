// Package run serves stonks.run.v1.
//
// A run is read with the caller's user id in the query, so a run another
// user started is not found rather than forbidden.
package run

import (
	"context"
	"errors"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	runv1 "github.com/leedenison/stonks/proto/run/v1"
	"github.com/leedenison/stonks/proto/run/v1/runv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Reader is the view of the run queries this package depends on.
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
	return connect.NewResponse(&runv1.GetRunResponse{Run: toProto(row)}), nil
}

func toProto(r gen.Run) *runv1.Run {
	out := &runv1.Run{
		Id:        r.ID.String(),
		Kind:      kindToProto(r.Kind),
		Trigger:   triggerToProto(r.Trigger),
		State:     stateToProto(r.State),
		Error:     r.Error,
		CreatedAt: timestamppb.New(r.CreatedAt),
	}
	if r.ParentID != nil {
		parent := r.ParentID.String()
		out.ParentId = &parent
	}
	if r.StartedAt != nil {
		out.StartedAt = timestamppb.New(*r.StartedAt)
	}
	if r.FinishedAt != nil {
		out.FinishedAt = timestamppb.New(*r.FinishedAt)
	}
	return out
}

func kindToProto(k gen.RunKind) runv1.RunKind {
	switch k {
	case gen.RunKindStatement:
		return runv1.RunKind_RUN_KIND_STATEMENT
	case gen.RunKindResolution:
		return runv1.RunKind_RUN_KIND_RESOLUTION
	}
	return runv1.RunKind_RUN_KIND_UNSPECIFIED
}

func triggerToProto(t gen.RunTrigger) runv1.RunTrigger {
	switch t {
	case gen.RunTriggerUser:
		return runv1.RunTrigger_RUN_TRIGGER_USER
	case gen.RunTriggerRun:
		return runv1.RunTrigger_RUN_TRIGGER_RUN
	}
	return runv1.RunTrigger_RUN_TRIGGER_UNSPECIFIED
}

func stateToProto(s gen.RunState) runv1.RunState {
	switch s {
	case gen.RunStatePending:
		return runv1.RunState_RUN_STATE_PENDING
	case gen.RunStateRunning:
		return runv1.RunState_RUN_STATE_RUNNING
	case gen.RunStateCompleted:
		return runv1.RunState_RUN_STATE_COMPLETED
	case gen.RunStateFailed:
		return runv1.RunState_RUN_STATE_FAILED
	case gen.RunStateInterrupted:
		return runv1.RunState_RUN_STATE_INTERRUPTED
	}
	return runv1.RunState_RUN_STATE_UNSPECIFIED
}
