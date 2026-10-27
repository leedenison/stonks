// Package coverage serves stonks.admin.coverage.v1.
package coverage

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	"github.com/leedenison/stonks/proto/admin/coverage/v1/admincoveragev1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/attest"
	reader "github.com/leedenison/stonks/server/internal/identity/coverage"
	"github.com/leedenison/stonks/server/internal/identity/validity"
	"github.com/leedenison/stonks/server/internal/types"
)

// Reader is this package's view of the coverage package.
type Reader interface {
	InstrumentTickers(ctx context.Context, instrument uuid.UUID) ([]reader.InstrumentTicker, error)
	Ticker(ctx context.Context, v types.MarketTicker, today time.Time) (*reader.Page, error)
}

var _ Reader = (*reader.Reader)(nil)

// Attester is this package's view of the attest package.
type Attester interface {
	Start(ctx context.Context, admin uuid.UUID, v types.MarketTicker, instrument *uuid.UUID, from time.Time, to *time.Time) (gen.Run, error)
	Withdraw(ctx context.Context, id uuid.UUID) error
}

var _ Attester = (*attest.Service)(nil)

//go:generate go tool mockgen -source=coverage.go -destination=mock/coverage_mock.go -package=mock

// Server implements CoverageService.
type Server struct {
	reader   Reader
	attester Attester
	clock    func() time.Time
}

var _ admincoveragev1connect.CoverageServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader, attester Attester, clock func() time.Time) *Server {
	return &Server{reader: reader, attester: attester, clock: clock}
}

// ListInstrumentTickers lists the tickers that name the instrument.
func (s *Server) ListInstrumentTickers(ctx context.Context, req *connect.Request[admincoveragev1.ListInstrumentTickersRequest]) (*connect.Response[admincoveragev1.ListInstrumentTickersResponse], error) {
	id, err := uuid.Parse(req.Msg.GetInstrumentId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rows, err := s.reader.InstrumentTickers(ctx, id)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admincoveragev1.ListInstrumentTickersResponse{}
	for _, r := range rows {
		out.Tickers = append(out.Tickers, r.ToProto())
	}
	return connect.NewResponse(out), nil
}

// GetTicker reads one ticker's facts and validity.
func (s *Server) GetTicker(ctx context.Context, req *connect.Request[admincoveragev1.GetTickerRequest]) (*connect.Response[admincoveragev1.GetTickerResponse], error) {
	t, err := s.reader.Ticker(ctx, types.MarketTickerOf(req.Msg.GetTicker()), s.clock())
	switch {
	case errors.Is(err, validity.ErrSpelling):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admincoveragev1.GetTickerResponse{Ticker: t.ToProto()}), nil
}

// Attest starts an attest identifier run, as the administrator calling.
func (s *Server) Attest(ctx context.Context, req *connect.Request[admincoveragev1.AttestRequest]) (*connect.Response[admincoveragev1.AttestResponse], error) {
	p, err := auth.Admin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
	m := req.Msg
	v := types.MarketTickerOf(m.GetTicker())
	var instrument *uuid.UUID
	if id := m.GetInstrumentId(); id != "" {
		parsed, err := uuid.Parse(id)
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		instrument = &parsed
	}
	from, err := time.Parse(time.DateOnly, m.GetFrom())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	var until *time.Time
	if m.To != nil {
		parsed, err := time.Parse(time.DateOnly, m.GetTo())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		until = &parsed
	}
	started, err := s.attester.Start(ctx, p.User.ID, v, instrument, from, until)
	switch {
	case errors.Is(err, validity.ErrFuture), errors.Is(err, attest.ErrRange):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, validity.ErrSpelling):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case errors.Is(err, db.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admincoveragev1.AttestResponse{Run: to.ProtoRun(started)}), nil
}

// WithdrawAttestation deletes an attested assertion.
func (s *Server) WithdrawAttestation(ctx context.Context, req *connect.Request[admincoveragev1.WithdrawAttestationRequest]) (*connect.Response[admincoveragev1.WithdrawAttestationResponse], error) {
	id, err := uuid.Parse(req.Msg.GetAssertionId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	err = s.attester.Withdraw(ctx, id)
	switch {
	case errors.Is(err, attest.ErrNotAttested):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, db.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admincoveragev1.WithdrawAttestationResponse{}), nil
}
