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
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/googlesheets"
	"github.com/leedenison/stonks/server/internal/price"
	"github.com/leedenison/stonks/server/internal/types"
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

// Uploader is this package's view of the price upload.
type Uploader interface {
	Upload(ctx context.Context, e *market.Entry, asAt time.Time, series []price.Series) (gen.Run, []price.Refusal, error)
}

var _ Uploader = (*price.Uploader)(nil)

// Sources is this package's view of the datasource registry.
type Sources interface {
	Entry(name string) *market.Entry
}

var _ Sources = (*market.Registry)(nil)

//go:generate go tool mockgen -source=price.go -destination=mock/price_mock.go -package=mock

// Server implements PriceService.
type Server struct {
	queries  Queries
	demand   Demand
	sources  Sources
	uploader Uploader
	clock    func() time.Time
}

var _ adminpricev1connect.PriceServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries, demand Demand, sources Sources, uploader Uploader, clock func() time.Time) *Server {
	return &Server{queries: queries, demand: demand, sources: sources, uploader: uploader, clock: clock}
}

// source returns the Google Sheets datasource, which every procedure needs
// enabled.
func (s *Server) source() (*market.Entry, error) {
	e := s.sources.Entry(googlesheets.Name)
	if e == nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("the %s datasource is not enabled", googlesheets.Name))
	}
	return e, nil
}

// ListPriceGaps lists the gaps of every user, and each listing that has a gap
// and lacks a ticker to ask for. It first creates the USD listing of each
// currency in use that lacks one, because a rate gap names that listing.
func (s *Server) ListPriceGaps(ctx context.Context, _ *connect.Request[adminpricev1.ListPriceGapsRequest]) (*connect.Response[adminpricev1.ListPriceGapsResponse], error) {
	if _, err := s.source(); err != nil {
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

// UploadPrices stores the series of one evaluation of the spreadsheet.
func (s *Server) UploadPrices(ctx context.Context, req *connect.Request[adminpricev1.UploadPricesRequest]) (*connect.Response[adminpricev1.UploadPricesResponse], error) {
	e, err := s.source()
	if err != nil {
		return nil, err
	}
	m := req.Msg
	asAt, err := time.Parse(time.DateOnly, m.GetAsAt())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("as_at: %w", err))
	}
	if asAt.After(types.Day(s.clock())) {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("as_at %s is later than today", m.GetAsAt()))
	}
	series := make([]price.Series, len(m.GetSeries()))
	for i, in := range m.GetSeries() {
		if series[i], err = seriesOf(in); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("series %s: %w", in.GetSymbol(), err))
		}
	}
	run, refused, err := s.uploader.Upload(ctx, e, asAt, series)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminpricev1.UploadPricesResponse{Run: to.ProtoRun(run)}
	for _, r := range refused {
		out.Refused = append(out.Refused, &adminpricev1.RefusedSeries{ListingId: r.Listing.String(), Reason: reasonOf(r.Err)})
	}
	return connect.NewResponse(out), nil
}

// seriesOf converts a validated series. It fails on a date that the pattern
// admits and the calendar lacks.
func seriesOf(in *adminpricev1.PriceSeries) (price.Series, error) {
	id, err := uuid.Parse(in.GetListingId())
	if err != nil {
		return price.Series{}, err
	}
	out := price.Series{Listing: id, Symbol: in.GetSymbol(), Currency: in.GetCurrency()}
	for _, p := range in.GetCoverage() {
		from, err := time.Parse(time.DateOnly, p.GetFrom())
		if err != nil {
			return price.Series{}, err
		}
		before, err := time.Parse(time.DateOnly, p.GetBefore())
		if err != nil {
			return price.Series{}, err
		}
		out.Coverage = append(out.Coverage, types.Period{From: from, Before: before})
	}
	for _, p := range in.GetPrices() {
		day, err := time.Parse(time.DateOnly, p.GetDay())
		if err != nil {
			return price.Series{}, err
		}
		v, err := decimal.NewFromString(p.GetPrice())
		if err != nil {
			return price.Series{}, err
		}
		out.Closes = append(out.Closes, price.Close{Day: day, Price: v})
	}
	return out, nil
}

func reasonOf(err error) adminpricev1.RefusalReason {
	switch {
	case errors.Is(err, price.ErrCurrency):
		return adminpricev1.RefusalReason_REFUSAL_REASON_CURRENCY
	case errors.Is(err, price.ErrInvalid):
		return adminpricev1.RefusalReason_REFUSAL_REASON_INVALID_PRICE
	case errors.Is(err, price.ErrListing):
		return adminpricev1.RefusalReason_REFUSAL_REASON_UNKNOWN_LISTING
	}
	return adminpricev1.RefusalReason_REFUSAL_REASON_UNSPECIFIED
}
