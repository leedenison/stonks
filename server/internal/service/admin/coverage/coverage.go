// Package coverage serves stonks.admin.coverage.v1.
package coverage

import (
	"context"
	"errors"
	"time"

	"connectrpc.com/connect"

	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	"github.com/leedenison/stonks/proto/admin/coverage/v1/admincoveragev1connect"
	reader "github.com/leedenison/stonks/server/internal/coverage"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// Reader is this package's view of the coverage package.
type Reader interface {
	Domains(ctx context.Context) ([]gen.ListTickerDomainsRow, error)
	Gaps(ctx context.Context, domain string, today time.Time) ([]reader.Gap, error)
	Ticker(ctx context.Context, v types.Identifier, today time.Time) (*reader.Ticker, error)
}

var _ Reader = (*reader.Reader)(nil)

//go:generate go tool mockgen -source=coverage.go -destination=mock/coverage_mock.go -package=mock

// Server implements CoverageService.
type Server struct {
	reader Reader
	clock  func() time.Time
}

var _ admincoveragev1connect.CoverageServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader, clock func() time.Time) *Server {
	return &Server{reader: reader, clock: clock}
}

// ListDomains lists the operating MICs on which keys with transactions state
// tickers.
func (s *Server) ListDomains(ctx context.Context, _ *connect.Request[admincoveragev1.ListDomainsRequest]) (*connect.Response[admincoveragev1.ListDomainsResponse], error) {
	rows, err := s.reader.Domains(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admincoveragev1.ListDomainsResponse{}
	for _, d := range rows {
		out.Domains = append(out.Domains, to.ProtoDomain(d))
	}
	return connect.NewResponse(out), nil
}

// ListGaps lists the gaps of every ticker stated on the domain.
func (s *Server) ListGaps(ctx context.Context, req *connect.Request[admincoveragev1.ListGapsRequest]) (*connect.Response[admincoveragev1.ListGapsResponse], error) {
	gaps, err := s.reader.Gaps(ctx, req.Msg.GetDomain(), s.clock())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admincoveragev1.ListGapsResponse{}
	for _, g := range gaps {
		out.Gaps = append(out.Gaps, g.ToProto())
	}
	return connect.NewResponse(out), nil
}

// GetTicker reads one ticker's facts and validity.
func (s *Server) GetTicker(ctx context.Context, req *connect.Request[admincoveragev1.GetTickerRequest]) (*connect.Response[admincoveragev1.GetTickerResponse], error) {
	m := req.Msg.GetTicker()
	typ, ok := types.FromProto[types.IdentifierType](m.GetType())
	if !ok || typ == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown identifier type"))
	}
	t, err := s.reader.Ticker(ctx, types.Identifier{Type: typ, Domain: m.GetDomain(), Value: m.GetValue()}, s.clock())
	switch {
	case errors.Is(err, validity.ErrNotTicker), errors.Is(err, validity.ErrSpelling):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admincoveragev1.GetTickerResponse{Ticker: t.ToProto()}), nil
}
