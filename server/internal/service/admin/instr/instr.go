// Package instr serves stonks.admin.instr.v1.
package instr

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	admininstrv1 "github.com/leedenison/stonks/proto/admin/instr/v1"
	"github.com/leedenison/stonks/proto/admin/instr/v1/admininstrv1connect"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListInstrs(ctx context.Context, arg gen.ListInstrsParams) ([]gen.ListInstrsRow, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
	ListListingNames(ctx context.Context, instrIds []uuid.UUID) ([]gen.ListListingNamesRow, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListViaOf(ctx context.Context, ids []uuid.UUID) ([]uuid.UUID, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=instr.go -destination=mock/instr_mock.go -package=mock

// Server implements InstrService.
type Server struct {
	queries Queries
}

var _ admininstrv1connect.InstrServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries) *Server {
	return &Server{queries: queries}
}

// ListInstrs lists the instruments the query and the class keep, by
// name then id. The page token names the last instrument served by its id
// and name, so a page continues from the right place when that instrument
// has gone or been renamed.
func (s *Server) ListInstrs(ctx context.Context, req *connect.Request[admininstrv1.ListInstrsRequest]) (*connect.Response[admininstrv1.ListInstrsResponse], error) {
	m := req.Msg
	afterID, afterName, err := parseCursor(m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	p := page.New(m.GetPageSize())
	arg := gen.ListInstrsParams{AfterID: afterID, AfterName: afterName, Lim: p.Lim()}
	if q := m.GetQuery(); q != "" {
		arg.Q = ptr.To(db.LikeEscape(q))
	}
	if m.AssetClass != nil {
		class, ok := types.FromProto[types.AssetClass](m.GetAssetClass())
		if !ok {
			return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("unknown asset class"))
		}
		arg.AssetClass = &class
	}
	rows, err := s.queries.ListInstrs(ctx, arg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	rows, more := page.Cut(p, rows)
	out := &admininstrv1.ListInstrsResponse{}
	if out.Instrs, err = s.instrs(ctx, rows); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if more {
		out.NextPageToken = cursor(rows[len(rows)-1])
	}
	return connect.NewResponse(out), nil
}

// cursor is the page token after row: its id, a colon, then its name. The
// id holds no colon, so the first one ends it.
func cursor(row gen.ListInstrsRow) string {
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

// instrs assembles the page's instruments, each listing grain identifier and
// each ticker on its listing. A listing with a primary venue precedes the
// rest. An instrument's Via holds the via identifiers of the keys that
// resolved to it.
func (s *Server) instrs(ctx context.Context, rows []gen.ListInstrsRow) ([]*admininstrv1.Instr, error) {
	if len(rows) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, 0, len(rows))
	instrs := make(map[uuid.UUID]*admininstrv1.Instr, len(rows))
	out := make([]*admininstrv1.Instr, 0, len(rows))
	for _, r := range rows {
		msg := to.ProtoInstr(r)
		ids = append(ids, r.ID)
		instrs[r.ID] = msg
		out = append(out, msg)
	}
	listings, err := s.queries.ListListingsOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	byListing := make(map[uuid.UUID]*admininstrv1.Listing, len(listings))
	for _, l := range listings {
		msg := to.ProtoListing(l)
		byListing[l.ID] = msg
		instrs[l.InstrID].Listings = append(instrs[l.InstrID].Listings, msg)
	}
	for _, in := range out {
		slices.SortStableFunc(in.Listings, func(a, b *admininstrv1.Listing) int {
			return primary(b) - primary(a)
		})
	}
	named, err := s.queries.ListListingNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	for _, n := range named {
		if n.TickerID != nil {
			l := byListing[n.ListingID]
			l.Tickers = append(l.Tickers, to.ProtoListingTicker(n))
		}
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
			in := instrs[i.InstrID]
			in.Via = append(in.Via, msg)
		}
		if i.ListingID != nil {
			l := byListing[*i.ListingID]
			l.Identifiers = append(l.Identifiers, msg)
			continue
		}
		in := instrs[i.InstrID]
		in.Identifiers = append(in.Identifiers, msg)
	}
	return out, nil
}

// primary ranks a listing for sorting, so one with a primary venue comes
// first.
func primary(l *admininstrv1.Listing) int {
	if l.GetPrimaryMic() != "" {
		return 1
	}
	return 0
}
