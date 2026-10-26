package validity

import (
	"context"
	"time"

	"github.com/leedenison/stonks/server/internal/types"
)

// Ticker is one ticker's validity, as one read of its facts found it. A
// reader that asks many questions of one ticker reads its facts once.
type Ticker struct {
	Identifier types.Identifier
	Derived    Derived
	asserted   map[time.Time]bool
}

// ReadTicker reads the facts of v and derives its validity.
func ReadTicker(ctx context.Context, q FactQueries, v types.Identifier) (*Ticker, error) {
	tickers, err := ReadTickers(ctx, q, []types.Identifier{v})
	if err != nil {
		return nil, err
	}
	return tickers[v], nil
}

// ReadTickers reads the facts of each of vs and derives its validity, with
// three queries however many tickers there are.
func ReadTickers(ctx context.Context, q FactQueries, vs []types.Identifier) (map[types.Identifier]*Ticker, error) {
	rows, err := ReadRowsOf(ctx, q, vs)
	if err != nil {
		return nil, err
	}
	out := make(map[types.Identifier]*Ticker, len(rows))
	for v, r := range rows {
		f := r.Facts()
		asserted := map[time.Time]bool{}
		for _, a := range f.Assertions {
			asserted[types.Day(a.At)] = true
		}
		out[v] = &Ticker{Identifier: v, Derived: Derive(f), asserted: asserted}
	}
	return out, nil
}

// HasValidity reports whether the facts give the ticker an instrument on any
// day.
func (t *Ticker) HasValidity() bool { return len(t.Derived.Intervals) > 0 }

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
