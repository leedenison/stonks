package tickervalidity

import (
	"context"

	"github.com/leedenison/stonks/server/internal/types"
)

// tickerFacts is one ticker's facts and what they imply, as one read found
// them.
type tickerFacts struct {
	Ticker  types.MarketTicker
	Facts   facts
	Derived derived
}

// readTickers reads the facts of each of ts and derives its ticker validity,
// with two queries however many tickers there are.
func readTickers(ctx context.Context, q FactQueries, ts []types.MarketTicker) (map[types.MarketTicker]*tickerFacts, error) {
	rows, err := readRowsOf(ctx, q, ts)
	if err != nil {
		return nil, err
	}
	out := make(map[types.MarketTicker]*tickerFacts, len(rows))
	for v, r := range rows {
		f := r.facts()
		out[v] = &tickerFacts{Ticker: v, Facts: f, Derived: derive(f)}
	}
	return out, nil
}
