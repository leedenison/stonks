// Package instrument serves stonks.instrument.v1.
package instrument

import (
	"context"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	instrumentv1 "github.com/leedenison/stonks/proto/instrument/v1"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Reader is this package's view of the instrument queries.
type Reader interface {
	ListUserInstruments(ctx context.Context, userID uuid.UUID) ([]gen.Instrument, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
}

var _ Reader = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=instrument.go -destination=mock/instrument_mock.go -package=mock

// Server implements InstrumentService.
type Server struct {
	reader Reader
}

var _ instrumentv1connect.InstrumentServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader) *Server {
	return &Server{reader: reader}
}

// ListInstruments lists the instruments the caller's keys resolved to, each
// with its listings. Another user's never appear, and a caller with none gets
// an empty list.
func (s *Server) ListInstruments(ctx context.Context, _ *connect.Request[instrumentv1.ListInstrumentsRequest]) (*connect.Response[instrumentv1.ListInstrumentsResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	res := &instrumentv1.ListInstrumentsResponse{}
	if res.Instruments, err = s.instruments(ctx, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(res), nil
}

// instruments assembles the user's instruments. An identifier that names a
// listing goes on that listing, not on the instrument.
func (s *Server) instruments(ctx context.Context, user uuid.UUID) ([]*instrumentv1.Instrument, error) {
	rows, err := s.reader.ListUserInstruments(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(rows))
	instruments := make(map[uuid.UUID]*instrumentv1.Instrument, len(rows))
	out := make([]*instrumentv1.Instrument, 0, len(rows))
	for _, r := range rows {
		msg := &instrumentv1.Instrument{Id: r.ID.String(), AssetClass: types.ToProto[typev1.AssetClass](r.AssetClass)}
		ids = append(ids, r.ID)
		instruments[r.ID] = msg
		out = append(out, msg)
	}
	listings, err := s.reader.ListListingsOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	byListing := make(map[uuid.UUID]*instrumentv1.Listing, len(listings))
	for _, l := range listings {
		msg := &instrumentv1.Listing{Id: l.ID.String(), Currency: l.Currency}
		byListing[l.ID] = msg
		instruments[l.InstrumentID].Listings = append(instruments[l.InstrumentID].Listings, msg)
	}
	idents, err := s.reader.ListIdentifiersOf(ctx, ids)
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
