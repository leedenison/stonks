// Package holding serves stonks.holding.v1.
//
// Holdings are summed from the caller's own transactions through the
// caller's own keys, so another user's never contribute, and a caller with
// none is answered with empty lists rather than not found. A holding carries
// the keys it sums. An instrument holding also carries every listing of the
// instrument.
//
// A dated key's transactions take the instrument its ticker names on their
// as-at dates, as the stored ticker validity states it; see package
// tickervalidity. A transaction dated when its ticker names nothing is summed
// into its key's group, beside the unresolved keys. One key may therefore sit
// under several holdings, with the sum of its transactions under each.
//
// Corporate actions restate a holding of an instrument whose class has the
// trait for it; see types.AssetClass.Restated. Each transaction is restated
// to today from the instrument's calendar; see package corpact. When every
// transaction is covered, the holding is adjusted. Otherwise the holding keeps
// the quantities as stated and is marked unadjusted. A key's quantity is in
// its holding's units, and a group keeps the quantities as stated. A holding
// whose shown quantity is zero is left out.
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
	"math/big"
	"slices"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// Reader is this package's view of the holdings queries.
type Reader interface {
	corpact.CalendarQueries
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
	clock    func() time.Time
}

var _ holdingv1connect.HoldingServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader, families currency.Families, scales currency.Scales, mics mic.MICMap, clock func() time.Time) *Server {
	return &Server{reader: reader, families: families, scales: scales, mics: mics, clock: clock}
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
// name, identifiers, listings and keys, by instrument id. Cash is summed
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
	var restated []uuid.UUID
	for _, i := range instrs {
		classOf[i.ID] = i.AssetClass
		if i.AssetClass.Restated() {
			restated = append(restated, i.ID)
		}
	}
	cals, err := corpact.Calendars(ctx, s.reader, restated)
	if err != nil {
		return nil, err
	}
	held := map[uuid.UUID][]keyDays{}
	for _, k := range keys {
		id := k.InstrID
		if id == nil {
			continue
		}
		q := k.Quantity
		if classOf[*id] == types.AssetClassCash && k.StatedKey.Currency != nil {
			q = q.Mul(s.scales.Scale(*k.StatedKey.Currency))
		}
		ks := held[*id]
		if n := len(ks); n == 0 || ks[n-1].key.ID != k.StatedKey.ID {
			ks = append(ks, keyDays{key: k.StatedKey})
		}
		n := len(ks) - 1
		ks[n].days, ks[n].raw = append(ks[n].days, day{asAt: k.AsAt, quantity: q}), append(ks[n].raw, k.Quantity)
		held[*id] = ks
	}
	today := types.Day(s.clock())
	sums := make(map[uuid.UUID]sum, len(held))
	total := make(map[uuid.UUID]decimal.Decimal, len(held))
	for id, ks := range held {
		r := rawSum(ks)
		if classOf[id].Restated() {
			r = restate(cals[id], ks, today)
		}
		sums[id], total[id] = r, r.quantity
	}
	ids := nonZero(total)
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
	out := make([]*holdingv1.InstrHolding, 0, len(ids))
	for _, id := range ids {
		r := sums[id]
		h := &holdingv1.InstrHolding{
			InstrId:    id.String(),
			AssetClass: types.ToProto[typev1.AssetClass](classOf[id]),
			Name:       nameOf[id],
			Quantity:   r.quantity.String(),
			Mark:       r.mark,
		}
		if r.uncoveredFrom != nil {
			h.UncoveredFrom = ptr.To(r.uncoveredFrom.Format(time.DateOnly))
		}
		for _, g := range r.gaps {
			h.Gaps = append(h.Gaps, &holdingv1.UncoveredDays{UncoveredFrom: g.From.Format(time.DateOnly), UncoveredBefore: g.Before.Format(time.DateOnly)})
		}
		for i, k := range held[id] {
			hk := to.ProtoHoldingKey(k.key, r.keys[i])
			if k.key.ViaTickerID != nil {
				if l := listing(s.families, k.key, listed[id]); l != uuid.Nil {
					hk.ListingId = l.String()
				}
			}
			h.Keys = append(h.Keys, hk)
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

// keyDays is one key's transactions on one instrument, summed by as-at date.
type keyDays struct {
	key  gen.StatedKey
	days []day
	// raw holds each day's sum in the key's own units, for cash in the
	// currency the key states.
	raw []decimal.Decimal
}

// day is the sum of a key's transactions as at one date, for cash in its
// family's unit.
type day struct {
	asAt     time.Time
	quantity decimal.Decimal
}

// sum is a holding's quantity and the quantity of each of its keys, in the
// order of its keys.
type sum struct {
	quantity      decimal.Decimal
	keys          []decimal.Decimal
	mark          holdingv1.HoldingMark
	uncoveredFrom *time.Time
	gaps          []types.Period
}

// rawSum sums ks as stated, for an instrument whose class is not restated.
func rawSum(ks []keyDays) sum {
	out := sum{mark: holdingv1.HoldingMark_HOLDING_MARK_RAW}
	for _, k := range ks {
		var key decimal.Decimal
		for i, d := range k.days {
			out.quantity = out.quantity.Add(d.quantity)
			key = key.Add(k.raw[i])
		}
		out.keys = append(out.keys, key)
	}
	return out
}

// restate sums ks restated to today by cal. It rounds the exact sum once to
// the most decimal places a transaction states, so a fraction sold for cash
// cancels to zero. If cal does not cover a transaction, it keeps the
// quantities as stated and marks the sum unadjusted from the day after the
// earliest such transaction.
func restate(cal corpact.Calendar, ks []keyDays, today time.Time) sum {
	out := rawSum(ks)
	out.mark = holdingv1.HoldingMark_HOLDING_MARK_ADJUSTED
	var scale int32
	factors := make([][]*big.Rat, len(ks))
	for i, k := range ks {
		for _, d := range k.days {
			scale = max(scale, -d.quantity.Exponent())
			f, ok := cal.Factor(d.asAt, today)
			if !ok && (out.uncoveredFrom == nil || d.asAt.Before(*out.uncoveredFrom)) {
				out.uncoveredFrom = &d.asAt
			}
			factors[i] = append(factors[i], f)
		}
	}
	if out.uncoveredFrom != nil {
		from := out.uncoveredFrom.AddDate(0, 0, 1)
		out.mark, out.uncoveredFrom = holdingv1.HoldingMark_HOLDING_MARK_UNADJUSTED, &from
		out.gaps = cal.Gaps(types.Period{From: from, Before: today.AddDate(0, 0, 1)})
		return out
	}
	total := new(big.Rat)
	for i, k := range ks {
		key := new(big.Rat)
		for j, d := range k.days {
			key.Add(key, new(big.Rat).Mul(d.quantity.Rat(), factors[i][j]))
		}
		total.Add(total, key)
		out.keys[i] = decimal.NewFromBigRat(key, scale)
	}
	out.quantity = decimal.NewFromBigRat(total, scale)
	return out
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
	var last uuid.UUID
	var key *gen.StatedKey
	var keyQty decimal.Decimal
	flush := func() {
		if key != nil {
			st := stated[*key.GroupID]
			st.h.Keys = append(st.h.Keys, to.ProtoHoldingKey(*key, keyQty))
		}
		key = nil
	}
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
		if key == nil || last != k.StatedKey.ID {
			flush()
			last, key, keyQty = k.StatedKey.ID, &k.StatedKey, decimal.Zero
			st, ok := stated[*g]
			if !ok {
				st = &stating{h: &holdingv1.GroupHolding{}, classes: map[types.AssetClass]bool{}, ids: map[types.Identifier]bool{}}
				stated[*g] = st
			}
			st.fold(k.StatedKey)
		}
		keyQty = keyQty.Add(k.Quantity)
		total[*g] = total[*g].Add(k.Quantity)
	}
	flush()
	ids := nonZero(total)
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

// nonZero returns the ids whose total is not zero, in id order.
func nonZero(total map[uuid.UUID]decimal.Decimal) []uuid.UUID {
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
