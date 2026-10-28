// Package holding serves stonks.holding.v1.
//
// Holdings are summed from the caller's own transactions through the
// caller's own keys, so another user's never contribute, and a caller with
// none is answered with empty lists rather than not found. A holding carries
// the keys it sums. An instrument holding also carries every listing of the
// instrument. A holding whose total is zero is left out.
//
// A dated key's transactions take the instrument its ticker names on their
// as-at dates, as the stored ticker validity states it; see package
// tickervalidity. A transaction dated when its ticker names nothing is summed
// into its key's group, beside the unresolved keys. One key may therefore sit
// under several holdings, with the sum of its transactions under each.
//
// Whether an association is confirmed or provisional shows on the statement,
// per transaction. A holding shows the same whichever its transactions are,
// because most associations would be provisional until coverage grows.
package holding

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// Reader is this package's view of the holdings queries.
type Reader interface {
	ListInstrsByID(ctx context.Context, ids []uuid.UUID) ([]gen.Instr, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListListingNames(ctx context.Context, instrIds []uuid.UUID) ([]gen.ListListingNamesRow, error)
	ListInstrNames(ctx context.Context, ids []uuid.UUID) ([]gen.InstrName, error)
	ListHoldingKeys(ctx context.Context, userID uuid.UUID) ([]gen.ListHoldingKeysRow, error)
}

var _ Reader = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=holding.go -destination=mock/holding_mock.go -package=mock

// Server implements HoldingService.
type Server struct {
	reader   Reader
	families currency.Families
	scales   currency.Scales
	mics     mic.MICMap
}

var _ holdingv1connect.HoldingServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader, families currency.Families, scales currency.Scales, mics mic.MICMap) *Server {
	return &Server{reader: reader, families: families, scales: scales, mics: mics}
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
	if res.Instrs, err = s.instrs(ctx, keys); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if res.Groups, err = s.groups(keys); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(res), nil
}

// instrs answers the user's holdings of instruments, each with its
// name, identifiers, listings and keys, by instrument id. Each key adds the
// sum of its transactions on an instrument to that instrument. Cash is summed
// in its family's unit, so pence count as hundredths of a pound. A security's
// quantity stays in shares whatever its currency.
func (s *Server) instrs(ctx context.Context, keys []gen.ListHoldingKeysRow) ([]*holdingv1.InstrHolding, error) {
	named := map[uuid.UUID]decimal.Decimal{}
	for _, k := range keys {
		if k.InstrID != nil {
			named[*k.InstrID] = decimal.Zero
		}
	}
	if len(named) == 0 {
		return nil, nil
	}
	instrs, err := s.reader.ListInstrsByID(ctx, sorted(named))
	if err != nil {
		return nil, err
	}
	classOf := make(map[uuid.UUID]types.AssetClass, len(instrs))
	for _, i := range instrs {
		classOf[i.ID] = i.AssetClass
	}
	total := make(map[uuid.UUID]decimal.Decimal, len(named))
	for _, k := range keys {
		id := k.InstrID
		if id == nil {
			continue
		}
		q := k.Quantity
		if classOf[*id] == types.AssetClassCash && k.StatedKey.Currency != nil {
			q = q.Mul(s.scales.Scale(*k.StatedKey.Currency))
		}
		total[*id] = total[*id].Add(q)
	}
	ids := held(total)
	if len(ids) == 0 {
		return nil, nil
	}
	idents, err := s.reader.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, err
	}
	identified := make(map[uuid.UUID][]gen.Identifier, len(ids))
	for _, i := range idents {
		identified[i.InstrID] = append(identified[i.InstrID], i)
	}
	listings, err := s.reader.ListListingNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	listed := make(map[uuid.UUID][]gen.ListListingNamesRow, len(ids))
	for _, l := range listings {
		listed[l.InstrID] = append(listed[l.InstrID], l)
	}
	names, err := s.reader.ListInstrNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[uuid.UUID]*typev1.Identifier, len(names))
	for _, n := range names {
		nameOf[n.InstrID] = to.InstrName(n).ToProto()
	}
	keysOf := map[uuid.UUID][]*holdingv1.HoldingKey{}
	for _, k := range keys {
		id := k.InstrID
		if id == nil {
			continue
		}
		hk := to.ProtoHoldingKey(k)
		if k.StatedKey.ViaTickerID != nil {
			if l := listing(s.families, k.StatedKey, listed[*id]); l != uuid.Nil {
				hk.ListingId = l.String()
			}
		}
		keysOf[*id] = append(keysOf[*id], hk)
	}
	out := make([]*holdingv1.InstrHolding, 0, len(ids))
	for _, id := range ids {
		h := &holdingv1.InstrHolding{
			InstrId:    id.String(),
			AssetClass: types.ToProto[typev1.AssetClass](classOf[id]),
			Name:       nameOf[id],
			Quantity:   total[id].String(),
			Keys:       keysOf[id],
		}
		for _, i := range identified[id] {
			h.Identifiers = append(h.Identifiers, to.Identifier(i).ToProto())
		}
		for _, l := range listed[id] {
			if l.Rank == nil || *l.Rank == 1 {
				h.Listings = append(h.Listings, to.ProtoHoldingListing(l))
			}
		}
		out = append(out, h)
	}
	return out, nil
}

// listing returns the listing a dated key's transactions sit under on one
// instrument: the listing in the family of the key's stated currency, as a
// fixed association takes, else the one that the key's ticker reaches at the
// key's venue. listings holds every ticker that reaches each listing of the
// instrument. It returns uuid.Nil when neither exists.
func listing(families currency.Families, k gen.StatedKey, listings []gen.ListListingNamesRow) uuid.UUID {
	carrier, family := uuid.Nil, ""
	if k.ViaTickerID != nil && k.ViaVenue != nil {
		for _, l := range listings {
			if ptr.Equal(l.TickerID, k.ViaTickerID) && ptr.Equal(l.Venue, k.ViaVenue) {
				carrier, family = l.ListingID, l.Currency
				break
			}
		}
	}
	if k.Currency != nil {
		if fam := families.Family(*k.Currency); fam != "" && fam != family {
			for _, l := range listings {
				if l.Currency == fam {
					return l.ListingID
				}
			}
		}
	}
	return carrier
}

// groups answers the user's holdings of groups, each with the classes and
// identifiers its keys state, once each, and the keys, by group id. An
// unresolved key is in its group whole. A dated key is in its group only for
// the transactions on days when its ticker names nothing.
func (s *Server) groups(keys []gen.ListHoldingKeysRow) ([]*holdingv1.GroupHolding, error) {
	total := map[uuid.UUID]decimal.Decimal{}
	stated := map[uuid.UUID]*stating{}
	for _, k := range keys {
		if k.InstrID != nil {
			continue
		}
		g := k.StatedKey.GroupID
		switch {
		case g == nil && k.StatedKey.ViaTickerID != nil:
			// Regroup gives every key without an instrument a group in the
			// transaction that writes its rows, so this is a defect.
			return nil, fmt.Errorf("dated key %s has unresolved transactions and no group", k.StatedKey.ID)
		case g == nil:
			continue
		}
		hk := to.ProtoHoldingKey(k)
		total[*g] = total[*g].Add(k.Quantity)
		st, ok := stated[*g]
		if !ok {
			st = &stating{h: &holdingv1.GroupHolding{}, classes: map[types.AssetClass]bool{}, ids: map[types.Identifier]bool{}}
			stated[*g] = st
		}
		st.fold(k.StatedKey)
		st.h.Keys = append(st.h.Keys, hk)
	}
	ids := held(total)
	out := make([]*holdingv1.GroupHolding, 0, len(ids))
	for _, id := range ids {
		h := &holdingv1.GroupHolding{}
		if st := stated[id]; st != nil {
			h = st.h
		}
		h.GroupId = id.String()
		h.Quantity = total[id].String()
		slices.Sort(h.AssetClasses)
		out = append(out, h)
	}
	return out, nil
}

// held returns the ids whose total is not zero, in id order.
func held(total map[uuid.UUID]decimal.Decimal) []uuid.UUID {
	return slices.DeleteFunc(sorted(total), func(id uuid.UUID) bool { return total[id].IsZero() })
}

// sorted returns the ids of m in id order.
func sorted(m map[uuid.UUID]decimal.Decimal) []uuid.UUID {
	ids := slices.Collect(maps.Keys(m))
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) })
	return ids
}

// stating is what the keys of one group state, each class and identifier
// once.
type stating struct {
	h       *holdingv1.GroupHolding
	classes map[types.AssetClass]bool
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
