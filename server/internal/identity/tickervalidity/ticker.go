package tickervalidity

import (
	"context"
	"time"

	"github.com/leedenison/stonks/server/internal/types"
)

// Ticker is one ticker's validity, as one read of its facts found it. A
// reader that asks many questions of one ticker reads its facts once.
type Ticker struct {
	Ticker   types.MarketTicker
	Derived  Derived
	asserted map[time.Time]bool
}

// ReadTicker reads the facts of t and derives its ticker validity.
func ReadTicker(ctx context.Context, q FactQueries, t types.MarketTicker) (*Ticker, error) {
	tickers, err := ReadTickers(ctx, q, []types.MarketTicker{t})
	if err != nil {
		return nil, err
	}
	return tickers[t], nil
}

// ReadTickers reads the facts of each of ts and derives its ticker validity,
// with three queries however many tickers there are.
func ReadTickers(ctx context.Context, q FactQueries, ts []types.MarketTicker) (map[types.MarketTicker]*Ticker, error) {
	rows, err := ReadRowsOf(ctx, q, ts)
	if err != nil {
		return nil, err
	}
	out := make(map[types.MarketTicker]*Ticker, len(rows))
	for v, r := range rows {
		f := r.Facts()
		asserted := map[time.Time]bool{}
		for _, a := range f.Assertions {
			asserted[types.Day(a.At)] = true
		}
		out[v] = &Ticker{Ticker: v, Derived: Derive(f), asserted: asserted}
	}
	return out, nil
}

// HasTickerValidity reports whether the facts give the ticker an instrument on
// any day.
func (t *Ticker) HasTickerValidity() bool { return len(t.Derived.Intervals) > 0 }

// Asserted reports whether any source asserted the ticker on day d.
func (t *Ticker) Asserted(d time.Time) bool { return t.asserted[types.Day(d)] }

// On returns the interval of the ticker's validity that holds day d. It
// reports false where the ticker names nothing on d.
func (t *Ticker) On(d time.Time) (Interval, bool) { return on(t.Derived.Intervals, d) }

// FirstUnconfirmed returns the earliest of dates that the ticker's validity
// does not confirm. It reports false when every date is confirmed.
func (t *Ticker) FirstUnconfirmed(dates []time.Time) (time.Time, bool) {
	return firstUnconfirmed(t.Derived.Intervals, dates)
}
