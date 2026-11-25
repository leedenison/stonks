package market

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// PriceRequest asks for the end of day prices of one listing, or the rates
// of one currency against USD, over one period.
type PriceRequest struct {
	// Identifier is a MIC_TICKER at an operating MIC, or the currency whose
	// rate is asked.
	Identifier types.Identifier
	Period     types.Period
}

// DayPrice is the end of day price a provider reported for one day.
type DayPrice struct {
	Day   time.Time
	Price decimal.Decimal
	// AsAt is the day on which the price was true of a share. It is Day for an
	// as traded price and for a rate. It is the day of the answer for a price
	// that the provider adjusts for splits.
	AsAt time.Time
}

// PriceResult is what was returned for one request.
type PriceResult struct {
	Prices []DayPrice
	// Currency is the code in which the provider quoted every price. It is
	// empty where Coverage is nil.
	Currency string
	// Coverage is the period the provider answered for. Over it, the provider
	// reported every price it holds, so a day without one had no session. It
	// ends on the day of the last price the provider holds for the
	// identifier, so a close that is not yet published stays uncovered. It is
	// nil where the provider does not know the identifier or holds no price
	// for it.
	Coverage *types.Period
}

// Prices is an integration that reports end of day prices and rates. It
// states each price free of any dividend adjustment. It states a rate as the
// number of USD that one unit of the currency buys, with USD as the currency
// of the result. It leaves the days before its plan's first day out of the
// coverage. It may also leave out the days before the first price it holds.
type Prices interface {
	Integration
	Prices() Server[PriceRequest, PriceResult]
}

// pricesKind is the end of day prices of listings and the rates of
// currencies. No stated key asks for one. The cache key includes the period.
var pricesKind = Kind[PriceRequest, PriceResult]{
	name: gen.FetchKindPrices,
	server: func(e *Entry) Server[PriceRequest, PriceResult] {
		if s, ok := e.Integration.(Prices); ok {
			return s.Prices()
		}
		return nil
	},
	statedKey: func(PriceRequest) *uuid.UUID { return nil },
	count:     func(r PriceResult) int { return len(r.Prices) },
	params: func(r PriceRequest) []string {
		return []string{r.Period.From.Format(time.DateOnly), r.Period.Before.Format(time.DateOnly)}
	},
	asked: func(r PriceRequest) *types.Period { return &r.Period },
}

// Prices fetches the prices of reqs from e, as a child run of parent.
func (f *Fetcher) Prices(ctx context.Context, parent gen.Run, e *Entry, reqs []PriceRequest) ([]FetchKey[PriceRequest, PriceResult], error) {
	return fetch(ctx, f, parent, e, pricesKind, reqs)
}
