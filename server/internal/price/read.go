package price

import (
	"context"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// places is the least number of decimal places a restated price keeps. When
// a price is stored with more places, the restated price keeps that many.
const places = 6

// Point is the price of a listing on one day.
type Point struct {
	Day time.Time
	// Price is in the family currency of the listing. It is in the share
	// units of today for a restated class, and as stored for any other.
	Price decimal.Decimal
	// Datasource served the price. On a day without a session it is the
	// datasource of the close the day takes.
	Datasource string
	// Provisional means a split may be missing from the restatement.
	Provisional bool
}

// stated is a price in the share units of its as at day.
type stated struct {
	day, asAt  time.Time
	price      decimal.Decimal
	datasource string
}

// Read returns the price of each of listings on each day of p, by listing.
// A day is absent where its price is unknown: no datasource covers it, or
// the coverage back to the latest earlier close is broken.
func Read(ctx context.Context, q ReadQueries, listings []uuid.UUID, p types.Period, today time.Time) (map[uuid.UUID][]Point, error) {
	out := map[uuid.UUID][]Point{}
	if len(listings) == 0 {
		return out, nil
	}
	classes, err := q.ListListingClasses(ctx, listings)
	if err != nil {
		return nil, fmt.Errorf("list listings: %w", err)
	}
	var restated []uuid.UUID
	for _, c := range classes {
		if c.AssetClass.Restated() {
			restated = append(restated, c.InstrID)
		}
	}
	cals := map[uuid.UUID]corpact.Calendar{}
	if len(restated) > 0 {
		if cals, err = corpact.Calendars(ctx, q, restated); err != nil {
			return nil, fmt.Errorf("read the corporate actions calendars: %w", err)
		}
	}
	rows, err := q.ListPrices(ctx, gen.ListPricesParams{ListingIds: listings, FromDay: p.From, Before: p.Before})
	if err != nil {
		return nil, fmt.Errorf("list prices: %w", err)
	}
	prices := map[uuid.UUID][]stated{}
	for _, r := range rows {
		prices[r.ListingID] = append(prices[r.ListingID], stated{day: r.Day, asAt: r.AsAt, price: r.Price, datasource: r.Datasource})
	}
	earlier, err := q.ListPricesBefore(ctx, gen.ListPricesBeforeParams{ListingIds: listings, Day: p.From})
	if err != nil {
		return nil, fmt.Errorf("list earlier prices: %w", err)
	}
	before := map[uuid.UUID]stated{}
	for _, r := range earlier {
		before[r.ListingID] = stated{day: r.Day, asAt: r.AsAt, price: r.Price, datasource: r.Datasource}
	}
	covering, err := q.ListPriceCoverage(ctx, listings)
	if err != nil {
		return nil, fmt.Errorf("list price coverage: %w", err)
	}
	covered := map[uuid.UUID][]types.Period{}
	for _, r := range covering {
		covered[r.ListingID] = append(covered[r.ListingID], types.Period{From: r.CoveredFrom, Before: r.CoveredBefore})
	}
	for _, c := range classes {
		var cal *corpact.Calendar
		if c.AssetClass.Restated() {
			found := cals[c.InstrID]
			cal = &found
		}
		var last *stated
		if b, ok := before[c.ID]; ok {
			last = &b
		}
		if s := series(prices[c.ID], last, types.Union(covered[c.ID]), cal, p, today); len(s) > 0 {
			out[c.ID] = s
		}
	}
	return out, nil
}

// series returns the points of one listing over p. prices must be in day
// order. A day without a price takes the latest earlier close, provided one
// period of covered holds both days. last is that close from before p, if
// any. cal is nil for a listing whose prices are never restated.
func series(prices []stated, last *stated, covered []types.Period, cal *corpact.Calendar, p types.Period, today time.Time) []Point {
	var out []Point
	var carried *Point
	var from time.Time
	if last != nil {
		pt := restate(*last, cal, today)
		carried, from = &pt, last.day
	}
	for d := p.From; d.Before(p.Before); d = d.AddDate(0, 0, 1) {
		if len(prices) > 0 && prices[0].day.Equal(d) {
			pt := restate(prices[0], cal, today)
			carried, from, prices = &pt, d, prices[1:]
			out = append(out, pt)
			continue
		}
		if carried != nil && unbroken(covered, from, d) {
			pt := *carried
			pt.Day = d
			out = append(out, pt)
		}
	}
	return out
}

// unbroken reports whether the datasources together cover every day from
// from to d.
func unbroken(covered []types.Period, from, d time.Time) bool {
	for _, c := range covered {
		if c.Contains(from) && c.Contains(d) {
			return true
		}
	}
	return false
}

// restate returns s in the share units of today, provisional where cal does
// not cover the days between. The factor is the inverse of the one that
// restates a quantity over the same days, so a price and a holding follow the
// same corporate actions.
func restate(s stated, cal *corpact.Calendar, today time.Time) Point {
	out := Point{Day: s.day, Price: s.price, Datasource: s.datasource}
	if cal == nil {
		return out
	}
	f, ok := cal.Factor(s.asAt, today)
	out.Provisional = !ok
	if f.Cmp(big.NewRat(1, 1)) != 0 {
		out.Price = decimal.NewFromBigRat(new(big.Rat).Quo(s.price.Rat(), f), max(places, -s.price.Exponent()))
	}
	return out
}
