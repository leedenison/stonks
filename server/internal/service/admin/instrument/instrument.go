// Package instrument serves stonks.admin.instrument.v1.
package instrument

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	admininstrumentv1 "github.com/leedenison/stonks/proto/admin/instrument/v1"
	"github.com/leedenison/stonks/proto/admin/instrument/v1/admininstrumentv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListInstruments(ctx context.Context, arg gen.ListInstrumentsParams) ([]gen.ListInstrumentsRow, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=instrument.go -destination=instrument_mock_test.go -package=instrument

// Server implements InstrumentService.
type Server struct {
	queries Queries
}

var _ admininstrumentv1connect.InstrumentServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries) *Server {
	return &Server{queries: queries}
}

// ListInstruments lists every instrument by name then id. The page token is
// the id of the last instrument served.
func (s *Server) ListInstruments(ctx context.Context, req *connect.Request[admininstrumentv1.ListInstrumentsRequest]) (*connect.Response[admininstrumentv1.ListInstrumentsResponse], error) {
	m := req.Msg
	after, err := page.Before(m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	p := page.New(m.GetPageSize())
	rows, err := s.queries.ListInstruments(ctx, gen.ListInstrumentsParams{After: after, Lim: p.Lim()})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rows, more := page.Cut(p, rows)
	out := &admininstrumentv1.ListInstrumentsResponse{}
	if out.Instruments, err = s.instruments(ctx, rows); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if more {
		out.NextPageToken = rows[len(rows)-1].ID.String()
	}
	return connect.NewResponse(out), nil
}

// instruments assembles the page's instruments, each listing grain
// identifier on its listing.
func (s *Server) instruments(ctx context.Context, rows []gen.ListInstrumentsRow) ([]*admininstrumentv1.Instrument, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	instruments := make(map[uuid.UUID]*admininstrumentv1.Instrument, len(rows))
	out := make([]*admininstrumentv1.Instrument, 0, len(rows))
	for _, r := range rows {
		msg := &admininstrumentv1.Instrument{Id: r.ID.String(), Name: r.Name, AssetClass: types.ToProto[typev1.AssetClass](r.AssetClass)}
		ids = append(ids, r.ID)
		instruments[r.ID] = msg
		out = append(out, msg)
	}
	listings, err := s.queries.ListListingsOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	byListing := make(map[uuid.UUID]*admininstrumentv1.Listing, len(listings))
	for _, l := range listings {
		msg := &admininstrumentv1.Listing{Id: l.ID.String(), Currency: l.Currency}
		if l.PrimaryMic != nil {
			msg.PrimaryMic = *l.PrimaryMic
		}
		byListing[l.ID] = msg
		instruments[l.InstrumentID].Listings = append(instruments[l.InstrumentID].Listings, msg)
	}
	idents, err := s.queries.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, i := range idents {
		msg := to.Identifier(i).ToProto()
		if i.ListingID != nil {
			l := byListing[*i.ListingID]
			l.Identifiers = append(l.Identifiers, msg)
			continue
		}
		in := instruments[i.InstrumentID]
		in.Identifiers = append(in.Identifiers, msg)
	}
	return out, nil
}
