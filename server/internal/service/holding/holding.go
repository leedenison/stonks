// Package holding serves stonks.holding.v1.
//
// Holdings are summed from the caller's own transactions through the
// caller's own keys, so another user's never contribute, and a caller with
// none is answered with empty lists rather than not found. A holding carries
// the keys it sums. An instrument holding also carries every listing of the
// instrument. A holding whose total is zero is left out.
//
// A dated key's transactions take the instrument its ticker names on their
// as-at dates, derived from the ticker's facts at each read; see package
// validity. A transaction dated when its ticker names nothing is summed into
// its key's group, beside the unresolved keys. One key may therefore sit
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
	"github.com/leedenison/stonks/server/internal/identity/validity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// Reader is this package's view of the holdings queries. It embeds the fact
// queries, which give a dated key's transactions their instruments.
type Reader interface {
	validity.FactQueries
	ListInstrumentsByID(ctx context.Context, ids []uuid.UUID) ([]gen.Instrument, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListListingNames(ctx context.Context, instrumentIds []uuid.UUID) ([]gen.ListListingNamesRow, error)
	ListInstrumentNames(ctx context.Context, ids []uuid.UUID) ([]gen.InstrumentName, error)
	ListHoldingKeys(ctx context.Context, userID uuid.UUID) ([]gen.ListHoldingKeysRow, error)
	ListDatedTransactions(ctx context.Context, userID uuid.UUID) ([]gen.ListDatedTransactionsRow, error)
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
	d, err := s.dated(ctx, p.User.ID, keys)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	res := &holdingv1.ListHoldingsResponse{}
	if res.Instruments, err = s.instruments(ctx, keys, d); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if res.Groups, err = s.groups(keys, d); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(res), nil
}

// instruments answers the user's holdings of instruments, each with its
// name, identifiers, listings and keys, by instrument id. A fixed key adds
// its sum to its instrument. Cash is summed in its family's unit, so pence
// count as hundredths of a pound. A security's quantity stays in shares
// whatever its currency.
func (s *Server) instruments(ctx context.Context, keys []gen.ListHoldingKeysRow, d dated) ([]*holdingv1.InstrumentHolding, error) {
	named := map[uuid.UUID]decimal.Decimal{}
	for _, k := range keys {
		if id := k.StatedKey.InstrumentID; id != nil {
			named[*id] = decimal.Zero
		}
	}
	for _, on := range d.onInstrument {
		for id := range on {
			named[id] = decimal.Zero
		}
	}
	if len(named) == 0 {
		return nil, nil
	}
	instruments, err := s.reader.ListInstrumentsByID(ctx, sorted(named))
	if err != nil {
		return nil, err
	}
	classOf := make(map[uuid.UUID]types.AssetClass, len(instruments))
	for _, i := range instruments {
		classOf[i.ID] = i.AssetClass
	}
	total := make(map[uuid.UUID]decimal.Decimal, len(named))
	for _, k := range keys {
		id := k.StatedKey.InstrumentID
		if id == nil {
			continue
		}
		q := k.Quantity
		if classOf[*id] == types.AssetClassCash && k.StatedKey.Currency != nil {
			q = q.Mul(s.scales.Scale(*k.StatedKey.Currency))
		}
		total[*id] = total[*id].Add(q)
	}
	for _, on := range d.onInstrument {
		for id, q := range on {
			total[id] = total[id].Add(q)
		}
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
		identified[i.InstrumentID] = append(identified[i.InstrumentID], i)
	}
	listings, err := s.reader.ListListingNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	listed := make(map[uuid.UUID][]gen.ListListingNamesRow, len(ids))
	for _, l := range listings {
		listed[l.InstrumentID] = append(listed[l.InstrumentID], l)
	}
	names, err := s.reader.ListInstrumentNames(ctx, ids)
	if err != nil {
		return nil, err
	}
	nameOf := make(map[uuid.UUID]*typev1.Identifier, len(names))
	for _, n := range names {
		nameOf[n.InstrumentID] = to.InstrumentName(n).ToProto()
	}
	keysOf := map[uuid.UUID][]*holdingv1.HoldingKey{}
	for _, k := range keys {
		if id := k.StatedKey.InstrumentID; id != nil {
			keysOf[*id] = append(keysOf[*id], to.ProtoHoldingKey(k))
			continue
		}
		for id, q := range d.onInstrument[k.StatedKey.ID] {
			hk := &holdingv1.HoldingKey{StatedKey: to.ProtoStatedKey(k.StatedKey), Quantity: q.String()}
			if l := listing(s.families, k.StatedKey, identified[id], listed[id]); l != uuid.Nil {
				hk.ListingId = l.String()
			}
			keysOf[id] = append(keysOf[id], hk)
		}
	}
	out := make([]*holdingv1.InstrumentHolding, 0, len(ids))
	for _, id := range ids {
		h := &holdingv1.InstrumentHolding{
			InstrumentId: id.String(),
			AssetClass:   types.ToProto[typev1.AssetClass](classOf[id]),
			Name:         nameOf[id],
			Quantity:     total[id].String(),
			Keys:         keysOf[id],
		}
		for _, i := range identified[id] {
			h.Identifiers = append(h.Identifiers, to.Identifier(i).ToProto())
		}
		for _, l := range listed[id] {
			h.Listings = append(h.Listings, to.ProtoHoldingListing(l))
		}
		out = append(out, h)
	}
	return out, nil
}

// listing returns the listing a dated key's transactions sit under on one
// instrument. That is the instrument's listing in the family of the key's
// stated currency, as a fixed association takes. Where the key states no
// currency, or the instrument has no listing in that family, it is the
// listing that carries the key's ticker. It returns uuid.Nil where neither
// exists.
func listing(families currency.Families, k gen.StatedKey, idents []gen.Identifier, listings []gen.ListListingNamesRow) uuid.UUID {
	ticker, _ := to.AssociatedTicker(k)
	carrier, family := uuid.Nil, ""
	for _, i := range idents {
		if i.ListingID != nil && to.Identifier(i) == ticker {
			carrier = *i.ListingID
		}
	}
	for _, l := range listings {
		if l.ListingID == carrier {
			family = l.Currency
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
// the transactions dated when its ticker names nothing.
func (s *Server) groups(keys []gen.ListHoldingKeysRow, d dated) ([]*holdingv1.GroupHolding, error) {
	total := map[uuid.UUID]decimal.Decimal{}
	stated := map[uuid.UUID]*stating{}
	for _, k := range keys {
		unresolved, isDated := d.unresolved[k.StatedKey.ID]
		g := k.StatedKey.GroupID
		switch {
		case g == nil && isDated:
			// Regroup gives every key without an instrument a group in the
			// transaction that writes its rows, so this is a defect.
			return nil, fmt.Errorf("dated key %s has unresolved transactions and no group", k.StatedKey.ID)
		case g == nil:
			continue
		}
		hk := to.ProtoHoldingKey(k)
		if _, ok := to.AssociatedTicker(k.StatedKey); ok {
			if !isDated {
				continue
			}
			hk = &holdingv1.HoldingKey{StatedKey: to.ProtoStatedKey(k.StatedKey), Quantity: unresolved.String()}
			total[*g] = total[*g].Add(unresolved)
		} else {
			total[*g] = total[*g].Add(k.Quantity)
		}
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
