// Package instrument serves stonks.admin.instrument.v1.
package instrument

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	admininstrumentv1 "github.com/leedenison/stonks/proto/admin/instrument/v1"
	"github.com/leedenison/stonks/proto/admin/instrument/v1/admininstrumentv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListInstruments(ctx context.Context, arg gen.ListInstrumentsParams) ([]gen.ListInstrumentsRow, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListViaOf(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
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

// ListInstruments lists the instruments the query and the class keep, by
// name then id. The page token names the last instrument served by its id
// and name, so a page continues from the right place when that instrument
// has gone or been renamed.
func (s *Server) ListInstruments(ctx context.Context, req *connect.Request[admininstrumentv1.ListInstrumentsRequest]) (*connect.Response[admininstrumentv1.ListInstrumentsResponse], error) {
	m := req.Msg
	afterID, afterName, err := parseCursor(m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	p := page.New(m.GetPageSize())
	arg := gen.ListInstrumentsParams{AfterID: afterID, AfterName: afterName, Lim: p.Lim()}
	if q := m.GetQuery(); q != "" {
		arg.Q = ptr.To(db.LikeEscape(q))
	}
	if m.AssetClass != nil {
		class, ok := types.FromProto[gen.AssetClass](m.GetAssetClass())
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown asset class"))
		}
		arg.AssetClass = &class
	}
	rows, err := s.queries.ListInstruments(ctx, arg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rows, more := page.Cut(p, rows)
	out := &admininstrumentv1.ListInstrumentsResponse{}
	if out.Instruments, err = s.instruments(ctx, rows); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if more {
		out.NextPageToken = cursor(rows[len(rows)-1])
	}
	return connect.NewResponse(out), nil
}

// cursor is the page token after row: its id, a colon, then its name. The
// id holds no colon, so the first one ends it.
func cursor(row gen.ListInstrumentsRow) string {
	return row.ID.String() + ":" + row.Name
}

// parseCursor reads a page token into the id and name it carries; an empty
// token is the first page.
func parseCursor(token string) (*uuid.UUID, *string, error) {
	if token == "" {
		return nil, nil, nil
	}
	text, name, ok := strings.Cut(token, ":")
	if !ok {
		return nil, nil, fmt.Errorf("page token %q: no name", token)
	}
	id, err := uuid.Parse(text)
	if err != nil {
		return nil, nil, fmt.Errorf("page token %q: %w", token, err)
	}
	return &id, &name, nil
}

// instruments assembles the page's instruments, each listing grain
// identifier on its listing. A listing with a primary venue precedes the
// rest. An instrument's Via holds the via identifiers of the keys that
// resolved to it.
func (s *Server) instruments(ctx context.Context, rows []gen.ListInstrumentsRow) ([]*admininstrumentv1.Instrument, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	instruments := make(map[uuid.UUID]*admininstrumentv1.Instrument, len(rows))
	out := make([]*admininstrumentv1.Instrument, 0, len(rows))
	for _, r := range rows {
		msg := &admininstrumentv1.Instrument{Id: r.ID.String(), AssetClass: types.ToProto[typev1.AssetClass](r.AssetClass)}
		if r.NameType != "" {
			msg.Name = types.Identifier{Type: types.IdentifierType(r.NameType), Domain: r.NameDomain, Value: r.Name}.ToProto()
		}
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
	for _, in := range out {
		slices.SortStableFunc(in.Listings, func(a, b *admininstrumentv1.Listing) int {
			return primary(b) - primary(a)
		})
	}
	idents, err := s.queries.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	viaIDs, err := s.queries.ListViaOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	via := make(map[uuid.UUID]bool, len(viaIDs))
	for _, id := range viaIDs {
		via[id] = true
	}
	for _, i := range idents {
		msg := to.Identifier(i).ToProto()
		if via[i.ID] {
			in := instruments[i.InstrumentID]
			in.Via = append(in.Via, msg)
		}
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

// primary ranks a listing for sorting, so one with a primary venue comes
// first.
func primary(l *admininstrumentv1.Listing) int {
	if l.GetPrimaryMic() != "" {
		return 1
	}
	return 0
}
