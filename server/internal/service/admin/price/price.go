// Package price serves stonks.admin.price.v1.
//
// The service exists for the Google Sheets tool, so each procedure fails with
// FAILED_PRECONDITION while the Google Sheets datasource is absent or
// disabled.
package price

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/googlesheets"
	"github.com/leedenison/stonks/server/internal/price"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	GetListingByID(ctx context.Context, id uuid.UUID) (gen.Listing, error)
}

var _ Queries = (*gen.Queries)(nil)

// Demand is this package's view of the price demand.
type Demand interface {
	UseCurrencies(ctx context.Context) error
	Gaps(ctx context.Context, user *uuid.UUID) ([]price.Gap, []uuid.UUID, error)
}

var _ Demand = (*price.Demand)(nil)

// Sources is this package's view of the datasource registry.
type Sources interface {
	Entry(name string) *market.Entry
}

var _ Sources = (*market.Registry)(nil)

//go:generate go tool mockgen -source=price.go -destination=mock/price_mock.go -package=mock

// Server implements PriceService.
type Server struct {
	queries Queries
	demand  Demand
	sources Sources
}

var _ adminpricev1connect.PriceServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries, demand Demand, sources Sources) *Server {
	return &Server{queries: queries, demand: demand, sources: sources}
}

// enabled checks the condition every procedure shares.
func (s *Server) enabled() error {
	if s.sources.Entry(googlesheets.Name) == nil {
		return connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("the %s datasource is not enabled", googlesheets.Name))
	}
	return nil
}

// ListPriceGaps lists the gaps of every user, and each listing that has a gap
// and lacks a ticker to ask for. It first creates the USD listing of each
// currency in use that lacks one, because a rate gap names that listing.
func (s *Server) ListPriceGaps(ctx context.Context, _ *connect.Request[adminpricev1.ListPriceGapsRequest]) (*connect.Response[adminpricev1.ListPriceGapsResponse], error) {
	if err := s.enabled(); err != nil {
		return nil, err
	}
	if err := s.demand.UseCurrencies(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	gaps, unasked, err := s.demand.Gaps(ctx, nil)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminpricev1.ListPriceGapsResponse{}
	for _, g := range gaps {
		pg := &adminpricev1.PriceGap{ListingId: g.Listing.String(), Identifier: g.Identifier.ToProto()}
		for _, p := range g.Periods {
			pg.Periods = append(pg.Periods, p.ToProto())
		}
		out.Gaps = append(out.Gaps, pg)
	}
	for _, id := range unasked {
		l, err := s.queries.GetListingByID(ctx, id)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, errors.Join(fmt.Errorf("get listing %s", id), err))
		}
		out.Unasked = append(out.Unasked, &adminpricev1.UnaskedListing{ListingId: id.String(), InstrId: l.InstrID.String(), Currency: l.Currency})
	}
	return connect.NewResponse(out), nil
}
