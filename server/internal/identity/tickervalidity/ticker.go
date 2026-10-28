package tickervalidity

import (
	"context"

	"github.com/leedenison/stonks/server/internal/types"
)

// Ticker is one ticker's facts and what they imply, as one read found them.
type Ticker struct {
	Ticker  types.MarketTicker
	Facts   Facts
	Derived Derived
}

// ReadTickers reads the facts of each of ts and derives its ticker validity,
// with two queries however many tickers there are.
func ReadTickers(ctx context.Context, q FactQueries, ts []types.MarketTicker) (map[types.MarketTicker]*Ticker, error) {
	rows, err := ReadRowsOf(ctx, q, ts)
	if err != nil {
		return nil, err
	}
	out := make(map[types.MarketTicker]*Ticker, len(rows))
	for v, r := range rows {
		f := r.Facts()
		out[v] = &Ticker{Ticker: v, Facts: f, Derived: Derive(f)}
	}
	return out, nil
}
