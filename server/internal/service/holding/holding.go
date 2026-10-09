// Package holding serves stonks.holding.v1.
//
// Holdings are summed by the queries from the caller's own transactions
// through the caller's own keys, so another user's never contribute, and a
// caller with none is answered with empty lists rather than not found. A
// resolved key is summed into its instrument, and an unresolved one into the
// group sharing its stated data. A holding carries the keys it sums. An
// instrument holding also carries every listing of the instrument.
package holding

import (
	"context"
	"slices"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Reader is this package's view of the holdings queries.
type Reader interface {
	ListInstrumentHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListInstrumentHoldingsRow, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListListingNames(ctx context.Context, instrumentIds []uuid.UUID) ([]gen.ListListingNamesRow, error)
	ListGroupHoldings(ctx context.Context, userID uuid.UUID) ([]gen.ListGroupHoldingsRow, error)
	ListHoldingKeys(ctx context.Context, userID uuid.UUID) ([]gen.ListHoldingKeysRow, error)
}

var _ Reader = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=holding.go -destination=mock/holding_mock.go -package=mock

// Server implements HoldingService.
type Server struct {
	reader Reader
}

var _ holdingv1connect.HoldingServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader) *Server {
	return &Server{reader: reader}
}

// ListHoldings lists the caller's holdings of both kinds.
func (s *Server) ListHoldings(ctx context.Context, _ *connect.Request[holdingv1.ListHoldingsRequest]) (*connect.Response[holdingv1.ListHoldingsResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	keys, err := s.reader.ListHoldingKeys(ctx, p.User.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := &holdingv1.ListHoldingsResponse{}
	if res.Instruments, err = s.instruments(ctx, p.User.ID, keys); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if res.Groups, err = s.groups(ctx, p.User.ID, keys); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(res), nil
}

// instruments answers the user's holdings of resolved instruments, each
// with its identifiers, listings and keys.
func (s *Server) instruments(ctx context.Context, user uuid.UUID, keys []gen.ListHoldingKeysRow) ([]*holdingv1.InstrumentHolding, error) {
	rows, err := s.reader.ListInstrumentHoldings(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, len(rows))
	for i, r := range rows {
		ids[i] = r.InstrumentID
	}
	idents, err := s.reader.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	named := make(map[uuid.UUID][]*typev1.Identifier, len(rows))
	for _, i := range idents {
		named[i.InstrumentID] = append(named[i.InstrumentID], to.Identifier(i).ToProto())
	}
	listings, err := s.reader.ListListingNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	listed := make(map[uuid.UUID][]*holdingv1.HoldingListing, len(rows))
	for _, l := range listings {
		listed[l.InstrumentID] = append(listed[l.InstrumentID], listing(l))
	}
	held := map[uuid.UUID][]*holdingv1.HoldingKey{}
	for _, k := range keys {
		if k.StatedKey.InstrumentID != nil {
			held[*k.StatedKey.InstrumentID] = append(held[*k.StatedKey.InstrumentID], key(k))
		}
	}
	out := make([]*holdingv1.InstrumentHolding, 0, len(rows))
	for _, r := range rows {
		out = append(out, &holdingv1.InstrumentHolding{
			InstrumentId: r.InstrumentID.String(),
			AssetClass:   types.ToProto[typev1.AssetClass](r.AssetClass),
			Identifiers:  named[r.InstrumentID],
			Quantity:     r.Quantity.String(),
			Listings:     listed[r.InstrumentID],
			Keys:         held[r.InstrumentID],
		})
	}
	return out, nil
}

// listing writes l as a named listing.
func listing(l gen.ListListingNamesRow) *holdingv1.HoldingListing {
	out := &holdingv1.HoldingListing{Id: l.ListingID.String(), Currency: l.Currency, Venue: l.Venue}
	if l.TickerDomain != nil && l.TickerValue != nil {
		out.Ticker = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: *l.TickerDomain, Value: *l.TickerValue}.ToProto()
	}
	return out
}

// key writes k as a key of a holding.
func key(k gen.ListHoldingKeysRow) *holdingv1.HoldingKey {
	out := &holdingv1.HoldingKey{StatedKey: to.ProtoStatedKey(k.StatedKey), Quantity: k.Quantity.String()}
	if k.StatedKey.ListingID != nil {
		out.ListingId = k.StatedKey.ListingID.String()
	}
	return out
}

// groups answers the user's holdings of unresolved groups, each with the
// classes and identifiers its keys state, once each, and the keys.
func (s *Server) groups(ctx context.Context, user uuid.UUID, keys []gen.ListHoldingKeysRow) ([]*holdingv1.GroupHolding, error) {
	rows, err := s.reader.ListGroupHoldings(ctx, user)
	if err != nil || len(rows) == 0 {
		return nil, err
	}
	stated := make(map[uuid.UUID]*stating, len(rows))
	for _, k := range keys {
		if k.StatedKey.GroupID == nil {
			continue
		}
		st, ok := stated[*k.StatedKey.GroupID]
		if !ok {
			st = &stating{h: &holdingv1.GroupHolding{}, classes: map[gen.AssetClass]bool{}, ids: map[types.Identifier]bool{}}
			stated[*k.StatedKey.GroupID] = st
		}
		st.fold(k.StatedKey)
		st.h.Keys = append(st.h.Keys, key(k))
	}
	out := make([]*holdingv1.GroupHolding, 0, len(rows))
	for _, r := range rows {
		h := &holdingv1.GroupHolding{}
		if st := stated[r.GroupID]; st != nil {
			h = st.h
		}
		h.GroupId = r.GroupID.String()
		h.Quantity = r.Quantity.String()
		slices.Sort(h.AssetClasses)
		out = append(out, h)
	}
	return out, nil
}

// stating is what the keys of one group state, each class and identifier
// once.
type stating struct {
	h       *holdingv1.GroupHolding
	classes map[gen.AssetClass]bool
	ids     map[types.Identifier]bool
}

// fold adds the asset class and identifiers of k that the group has not yet
// stated.
func (st *stating) fold(k gen.StatedKey) {
	if k.AssetClass != nil && !st.classes[*k.AssetClass] {
		st.classes[*k.AssetClass] = true
		st.h.AssetClasses = append(st.h.AssetClasses, types.ToProto[typev1.AssetClass](*k.AssetClass))
	}
	for _, id := range k.Identifiers {
		if !st.ids[id] {
			st.ids[id] = true
			st.h.Identifiers = append(st.h.Identifiers, id.ToProto())
		}
	}
}
