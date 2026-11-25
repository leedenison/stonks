package price

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// ErrCurrency is a refusal of an answer whose currency is outside the family
// of its listing, or is one the system lacks.
var ErrCurrency = errors.New("currency outside the family of the listing")

// ErrInvalid is a refusal of an answer that holds a price that is not
// positive, or one stated as at a day before its own.
var ErrInvalid = errors.New("invalid price")

// Answer is the input of a write: what one datasource served for one
// listing.
type Answer struct {
	Listing    uuid.UUID
	Datasource string
	FetchKeyID uuid.UUID
	// AnsweredAt is the moment the datasource answered. Coverage stops before
	// the day of this moment.
	AnsweredAt time.Time
	market.PriceResult
}

// Record stores a and reports whether it stored any coverage. It stores
// each price in the family currency of the listing, with the as at day the
// datasource stated. It refuses an answer with ErrCurrency or ErrInvalid and
// stores nothing. An answer without coverage is not an error. The caller runs
// it in a transaction.
func Record(ctx context.Context, q RecordQueries, families currency.Families, scales currency.Scales, a Answer) (bool, error) {
	coverage, ok := covered(a.Coverage, a.AnsweredAt)
	if !ok {
		return false, nil
	}
	listing, err := q.GetListingByID(ctx, a.Listing)
	if err != nil {
		return false, fmt.Errorf("get listing: %w", err)
	}
	if families.Family(a.Currency) != listing.Currency {
		return false, fmt.Errorf("%w: %q for a %s listing", ErrCurrency, a.Currency, listing.Currency)
	}
	for _, p := range a.Prices {
		if !p.Price.IsPositive() || types.Day(p.AsAt).Before(types.Day(p.Day)) {
			return false, fmt.Errorf("%w: %s on %s as at %s", ErrInvalid, p.Price, p.Day.Format(time.DateOnly), p.AsAt.Format(time.DateOnly))
		}
	}
	if err := db.LockPrices(ctx, q, a.Datasource, []uuid.UUID{a.Listing}); err != nil {
		return false, err
	}
	rows, err := q.ListPriceCoverage(ctx, []uuid.UUID{a.Listing})
	if err != nil {
		return false, fmt.Errorf("list price coverage: %w", err)
	}
	kept := types.Gaps(periods(rows, a.Datasource), coverage)
	scale := scales.Scale(a.Currency)
	var prices []gen.CreatePricesParams
	for _, k := range kept {
		id := db.NewID()
		if err := q.CreatePriceCoverage(ctx, gen.CreatePriceCoverageParams{
			ID: id, ListingID: a.Listing, Datasource: a.Datasource, FetchKeyID: a.FetchKeyID, CoveredFrom: k.From, CoveredBefore: k.Before,
		}); err != nil {
			return false, fmt.Errorf("create price coverage: %w", err)
		}
		for _, p := range a.Prices {
			if k.Contains(types.Day(p.Day)) {
				prices = append(prices, gen.CreatePricesParams{CoverageID: id, Day: types.Day(p.Day), Price: p.Price.Mul(scale), AsAt: types.Day(p.AsAt)})
			}
		}
	}
	if _, err := q.CreatePrices(ctx, prices); err != nil {
		return false, fmt.Errorf("create prices: %w", err)
	}
	return len(kept) > 0, nil
}

// covered cuts the coverage c of an answer given at the moment at to end on
// the day before at, which is the last day completed by then. A cached answer
// keeps the moment of the call that filled the cache, so it covers only days
// before that call.
func covered(c *types.Period, at time.Time) (types.Period, bool) {
	if c == nil {
		return types.Period{}, false
	}
	return types.Period{From: types.Day(c.From), Before: types.Day(c.Before)}.Clip(types.Period{From: types.Day(c.From), Before: types.Day(at)})
}

// periods returns the periods of rows that datasource covers.
func periods(rows []gen.PriceCoverage, datasource string) []types.Period {
	var out []types.Period
	for _, r := range rows {
		if r.Datasource == datasource {
			out = append(out, types.Period{From: r.CoveredFrom, Before: r.CoveredBefore})
		}
	}
	return out
}
