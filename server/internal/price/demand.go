package price

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/types"
)

// lead is how many days before the first day held the demand starts. A
// holding can begin on a day without a session, and the read then needs an
// earlier close to carry.
const lead = 7

// base is the currency against which every rate is stored.
const base = "USD"

// Demand works out the days whose prices and rates the system needs.
type Demand struct {
	store    DemandQueries
	families currency.Families
	clock    func() time.Time
}

// NewDemand returns a Demand.
func NewDemand(store DemandQueries, families currency.Families, clock func() time.Time) *Demand {
	return &Demand{store: store, families: families, clock: clock}
}

// Gap is the days of one listing that are demanded and that no datasource
// covers.
type Gap struct {
	Listing uuid.UUID
	// Identifier is what a fetch sends: the current ticker of the listing at
	// its venue, or the currency of a rate.
	Identifier types.Identifier
	Periods    []types.Period
}

// demand is what the holdings of every user ask for.
type demand struct {
	// listings maps each demanded listing of a priced instrument to its days.
	listings map[uuid.UUID][]types.Period
	// asked holds the demanded listings that the user of the request holds.
	asked map[uuid.UUID]bool
	// names holds the tickers that reach each listing of a priced instrument,
	// best first.
	names map[uuid.UUID][]gen.ListListingNamesRow
	// codes holds each currency in use other than the base, sorted.
	codes []string
	// rates is the one period over which every rate is demanded. It is empty
	// where no user has a transaction.
	rates types.Period
}

// UseCurrencies creates the USD listing of every currency in use that lacks
// one. Gaps reads the rate of a currency through that listing, so a caller
// runs UseCurrencies first.
func (d *Demand) UseCurrencies(ctx context.Context) error {
	dem, err := d.demand(ctx, nil)
	if err != nil {
		return err
	}
	if len(dem.codes) == 0 {
		return nil
	}
	ids := make([]uuid.UUID, len(dem.codes))
	for i := range ids {
		ids[i] = db.NewID()
	}
	if err := d.store.CreateRateListings(ctx, gen.CreateRateListingsParams{Ids: ids, Codes: dem.codes}); err != nil {
		return fmt.Errorf("create rate listings: %w", err)
	}
	return nil
}

// Gaps returns each demanded day that no datasource covers, by listing. A
// nil user demands the prices of every user, and a user demands the prices of
// that user alone. Either demands every rate. unasked holds each listing that
// has such a day and lacks a current ticker to send in a fetch. Gaps fails where a
// currency in use lacks its USD listing; see UseCurrencies.
func (d *Demand) Gaps(ctx context.Context, user *uuid.UUID) (gaps []Gap, unasked []uuid.UUID, err error) {
	dem, err := d.demand(ctx, user)
	if err != nil {
		return nil, nil, err
	}
	need := map[uuid.UUID][]types.Period{}
	identifier := map[uuid.UUID]*types.Identifier{}
	for id, ps := range dem.listings {
		if dem.asked[id] {
			need[id] = types.Union(ps)
			identifier[id] = current(dem.names[id])
		}
	}
	if len(dem.codes) > 0 {
		rows, err := d.store.ListRateListings(ctx)
		if err != nil {
			return nil, nil, fmt.Errorf("list rate listings: %w", err)
		}
		rate := map[string]uuid.UUID{}
		for _, r := range rows {
			if r.Currency == base {
				rate[r.Code] = r.ID
			}
		}
		for _, code := range dem.codes {
			id, ok := rate[code]
			if !ok {
				return nil, nil, fmt.Errorf("the currency %s is in use and lacks its %s listing", code, base)
			}
			need[id] = []types.Period{dem.rates}
			identifier[id] = &types.Identifier{Type: types.IdentifierTypeCurrency, Value: code}
		}
	}
	ids := slices.SortedFunc(maps.Keys(need), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	rows, err := d.store.ListPriceCoverage(ctx, ids)
	if err != nil {
		return nil, nil, fmt.Errorf("list price coverage: %w", err)
	}
	covered := map[uuid.UUID][]types.Period{}
	for _, r := range rows {
		covered[r.ListingID] = append(covered[r.ListingID], types.Period{From: r.CoveredFrom, Before: r.CoveredBefore})
	}
	for _, id := range ids {
		var open []types.Period
		for _, p := range need[id] {
			open = append(open, types.Gaps(covered[id], p)...)
		}
		switch {
		case len(open) == 0:
		case identifier[id] == nil:
			unasked = append(unasked, id)
		default:
			gaps = append(gaps, Gap{Listing: id, Identifier: *identifier[id], Periods: open})
		}
	}
	return gaps, unasked, nil
}

// current returns the first current ticker among names, as the identifier a
// fetch sends. It returns nil where no ticker of the listing is current.
func current(names []gen.ListListingNamesRow) *types.Identifier {
	for _, n := range names {
		if n.Relation != nil && *n.Relation == gen.TickerRelationCurrent && n.Venue != nil && n.TickerValue != nil {
			return &types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: *n.Venue, Value: *n.TickerValue}
		}
	}
	return nil
}

// holding is the transactions of one user on one instrument.
type holding struct {
	user, instr uuid.UUID
}

// demand reads the holdings of every user. user narrows asked alone, because
// a currency is in use whoever holds it.
func (d *Demand) demand(ctx context.Context, user *uuid.UUID) (demand, error) {
	out := demand{listings: map[uuid.UUID][]types.Period{}, asked: map[uuid.UUID]bool{}, names: map[uuid.UUID][]gen.ListListingNamesRow{}}
	rows, err := d.store.ListHoldingKeys(ctx, nil)
	if err != nil {
		return out, fmt.Errorf("list holding keys: %w", err)
	}
	if len(rows) == 0 {
		return out, nil
	}
	today := types.Day(d.clock())
	first := rows[0].AsAt
	days := map[holding][]txnDay{}
	keys := map[holding]map[uuid.UUID]gen.StatedKey{}
	instrs := map[uuid.UUID]bool{}
	for _, r := range rows {
		if r.AsAt.Before(first) {
			first = r.AsAt
		}
		if r.InstrID == nil {
			continue
		}
		h := holding{user: r.StatedKey.UserID, instr: *r.InstrID}
		days[h] = append(days[h], txnDay{asAt: r.AsAt, quantity: r.Quantity})
		if keys[h] == nil {
			keys[h] = map[uuid.UUID]gen.StatedKey{}
		}
		keys[h][r.StatedKey.ID] = r.StatedKey
		instrs[*r.InstrID] = true
	}
	if from := types.Day(first).AddDate(0, 0, -lead); from.Before(today) {
		out.rates = types.Period{From: from, Before: today}
	}
	classes, err := d.store.ListInstrsByID(ctx, sortedIDs(instrs))
	if err != nil {
		return out, fmt.Errorf("list instruments: %w", err)
	}
	classOf := map[uuid.UUID]types.AssetClass{}
	var restated, priced []uuid.UUID
	for _, i := range classes {
		classOf[i.ID] = i.AssetClass
		if i.AssetClass.Restated() {
			restated = append(restated, i.ID)
		}
		if i.AssetClass.Priced() {
			priced = append(priced, i.ID)
		}
	}
	cals, err := corpact.Calendars(ctx, d.store, restated)
	if err != nil {
		return out, fmt.Errorf("read the corporate actions calendars: %w", err)
	}
	names, err := d.store.ListListingNames(ctx, priced)
	if err != nil {
		return out, fmt.Errorf("list listings: %w", err)
	}
	listed := map[uuid.UUID][]gen.ListListingNamesRow{}
	family := map[uuid.UUID]string{}
	for _, n := range names {
		listed[n.InstrID] = append(listed[n.InstrID], n)
		out.names[n.ListingID] = append(out.names[n.ListingID], n)
		family[n.ListingID] = n.Currency
	}
	inUse := map[string]bool{}
	for h, ds := range days {
		class := classOf[h.instr]
		if class == types.AssetClassCash {
			for _, k := range keys[h] {
				if k.Currency != nil {
					inUse[d.families.Family(*k.Currency)] = true
				}
			}
			continue
		}
		if !class.Priced() {
			continue
		}
		periods := held(cals[h.instr], class.Restated(), ds, today)
		if len(periods) == 0 {
			continue
		}
		for id := range named(d.families, keys[h], listed[h.instr]) {
			out.listings[id] = append(out.listings[id], periods...)
			inUse[family[id]] = true
			if user == nil || *user == h.user {
				out.asked[id] = true
			}
		}
	}
	delete(inUse, base)
	delete(inUse, "")
	out.codes = slices.Sorted(maps.Keys(inUse))
	return out, nil
}

// named returns the listings of one instrument that keys demand: the listing
// each key names, and every listing for a key that names only the instrument.
func named(families currency.Families, keys map[uuid.UUID]gen.StatedKey, listed []gen.ListListingNamesRow) map[uuid.UUID]bool {
	out := map[uuid.UUID]bool{}
	for _, k := range keys {
		if id, ok := tickers.Listing(families, k, listed); ok {
			out[id] = true
			continue
		}
		for _, l := range listed {
			out[l.ListingID] = true
		}
	}
	return out
}

// txnDay is the transactions of one key as at one day.
type txnDay struct {
	asAt     time.Time
	quantity decimal.Decimal
}

// held returns the days on which a holding with the transactions ds was
// open, each period starting lead days early, up to the day before today. A
// holding is open while its quantities sum to a value other than zero. Each
// quantity is first restated to the day of the last transaction through cal,
// where restated is set. The sum is rounded to the most decimal places a
// transaction states, so a fractional share sold for cash in lieu cancels.
// Where cal does not cover the days between two transactions, the sum cannot
// be trusted, and the holding counts as open from its first day.
func held(cal corpact.Calendar, restated bool, ds []txnDay, today time.Time) []types.Period {
	if len(ds) == 0 {
		return nil
	}
	slices.SortFunc(ds, func(a, b txnDay) int { return a.asAt.Compare(b.asAt) })
	last := ds[len(ds)-1].asAt
	var places int32
	for _, d := range ds {
		places = max(places, -d.quantity.Exponent())
	}
	var out []types.Period
	sum := new(big.Rat)
	open := false
	for i, d := range ds {
		q := d.quantity.Rat()
		if restated {
			f, ok := cal.Factor(d.asAt, last)
			if !ok {
				return early([]types.Period{{From: ds[0].asAt, Before: today}}, today)
			}
			q.Mul(q, f)
		}
		sum.Add(sum, q)
		if i+1 < len(ds) && ds[i+1].asAt.Equal(d.asAt) {
			continue
		}
		zero := decimal.NewFromBigRat(sum, places).IsZero()
		switch {
		case !open && !zero:
			out, open = append(out, types.Period{From: d.asAt, Before: today}), true
		case open && zero:
			out[len(out)-1].Before, open = d.asAt.AddDate(0, 0, 1), false
		}
	}
	return early(out, today)
}

// early starts each of ps lead days early and ends it by today. It drops a
// period that this leaves empty.
func early(ps []types.Period, today time.Time) []types.Period {
	var out []types.Period
	for _, p := range ps {
		p.From = p.From.AddDate(0, 0, -lead)
		if p.Before.After(today) {
			p.Before = today
		}
		if p.From.Before(p.Before) {
			out = append(out, p)
		}
	}
	return out
}

func sortedIDs(ids map[uuid.UUID]bool) []uuid.UUID {
	return slices.SortedFunc(maps.Keys(ids), func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
}
