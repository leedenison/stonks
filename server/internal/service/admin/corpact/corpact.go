// Package corpact serves stonks.admin.corpact.v1.
package corpact

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	admincorpactv1 "github.com/leedenison/stonks/proto/admin/corpact/v1"
	"github.com/leedenison/stonks/proto/admin/corpact/v1/admincorpactv1connect"
	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListInstrMarkets(ctx context.Context, instrs []uuid.UUID) ([]gen.ListInstrMarketsRow, error)
	ListTickerValidity(ctx context.Context, arg gen.ListTickerValidityParams) ([]gen.ListTickerValidityRow, error)
	GetCorpAct(ctx context.Context, id uuid.UUID) (gen.GetCorpActRow, error)
	ListInstrCorpActs(ctx context.Context, instrID uuid.UUID) ([]gen.ListInstrCorpActsRow, error)
	ListStatedSplits(ctx context.Context, arg gen.ListStatedSplitsParams) ([]gen.ListStatedSplitsRow, error)
	DecideCorpAct(ctx context.Context, arg gen.DecideCorpActParams) (uuid.UUID, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=corpact.go -destination=mock/corpact_mock.go -package=mock

// Server implements CorpActService.
type Server struct {
	queries Queries
}

var _ admincorpactv1connect.CorpActServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries) *Server {
	return &Server{queries: queries}
}

// ListInstrCorpActs lists the stored actions of the instrument.
func (s *Server) ListInstrCorpActs(ctx context.Context, req *connect.Request[admincorpactv1.ListInstrCorpActsRequest]) (*connect.Response[admincorpactv1.ListInstrCorpActsResponse], error) {
	id, err := uuid.Parse(req.Msg.GetInstrId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	acts, err := corpact.ListInstrActs(ctx, s.queries, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admincorpactv1.ListInstrCorpActsResponse{}
	for _, a := range acts {
		out.CorpActs = append(out.CorpActs, a.ToProto())
	}
	return connect.NewResponse(out), nil
}

// GetCorpAct reads one action with its evidence.
func (s *Server) GetCorpAct(ctx context.Context, req *connect.Request[admincorpactv1.GetCorpActRequest]) (*connect.Response[admincorpactv1.GetCorpActResponse], error) {
	id, err := uuid.Parse(req.Msg.GetCorpActId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	ev, err := corpact.ReadEvidence(ctx, s.queries, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such corporate action"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admincorpactv1.GetCorpActResponse{CorpAct: ev.Act.ToProto()}
	for _, sp := range ev.StmtSplits {
		msg := &admincorpactv1.StmtSplit{EffectiveDate: sp.EffectiveDate.Format(time.DateOnly)}
		if sp.Ratio != nil {
			msg.Ratio = sp.Ratio.ToProto()
		}
		out.StmtSplits = append(out.StmtSplits, msg)
	}
	for _, a := range ev.Others {
		out.OtherCorpActs = append(out.OtherCorpActs, a.ToProto())
	}
	return connect.NewResponse(out), nil
}

// DecideCorpAct records the decision on an action.
func (s *Server) DecideCorpAct(ctx context.Context, req *connect.Request[admincorpactv1.DecideCorpActRequest]) (*connect.Response[admincorpactv1.DecideCorpActResponse], error) {
	id, err := uuid.Parse(req.Msg.GetCorpActId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	d, ok := types.FromProto[gen.CorpActDecision](req.Msg.GetDecision())
	if !ok || d == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown decision"))
	}
	_, err = s.queries.DecideCorpAct(ctx, gen.DecideCorpActParams{ID: id, Decision: d})
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such corporate action"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admincorpactv1.DecideCorpActResponse{}), nil
}
