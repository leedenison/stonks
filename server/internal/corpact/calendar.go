package corpact

import (
	"context"
	"fmt"
	"maps"
	"math/big"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/types"
)

// CalendarQueries read the corporate actions of instruments.
type CalendarQueries interface {
	ReadQueries
	MarketQueries
	tickervalidity.NamingQueries
}

var _ CalendarQueries = (*gen.Queries)(nil)

// MarketQueries read the markets of instruments.
type MarketQueries interface {
	ListInstrMarkets(ctx context.Context, instrs []uuid.UUID) ([]gen.ListInstrMarketsRow, error)
}

var _ MarketQueries = (*gen.Queries)(nil)

// Markets returns the market of each of instrs: the market of the primary
// venues of its listings. Markets leaves out an instrument whose listings
// state no primary venue. It also leaves out one whose listings state
// primary venues in two markets.
func Markets(ctx context.Context, q MarketQueries, instrs []uuid.UUID) (map[uuid.UUID]string, error) {
	out := map[uuid.UUID]string{}
	if len(instrs) == 0 {
		return out, nil
	}
	rows, err := q.ListInstrMarkets(ctx, instrs)
	if err != nil {
		return nil, fmt.Errorf("list the markets of the instruments: %w", err)
	}
	for _, row := range rows {
		out[row.InstrID] = row.Market
	}
	return out, nil
}

// Calendar is the answer of the corporate actions source of an instrument's
// market, over the days that a ticker of the market named the instrument.
// Coverage leaves out the ex-date of every unhandled action. Acts holds an
// action once when two tickers of the market report it. The zero Calendar
// covers no day.
type Calendar struct {
	Market string
	Answer
}

// Calendars returns the calendar of each of instrs that the corporate
// actions source of its market has answered for. Its market is the one
// Markets gives, and an instrument without one has no calendar. Ticker
// validity attributes each action and each day of coverage to the instrument
// that its ticker named on that day. Calendars reads with five queries
// however many instruments there are.
func Calendars(ctx context.Context, q CalendarQueries, instrs []uuid.UUID) (map[uuid.UUID]Calendar, error) {
	out := map[uuid.UUID]Calendar{}
	markets, err := Markets(ctx, q, instrs)
	if err != nil || len(markets) == 0 {
		return out, err
	}
	read, err := tickervalidity.LoadNaming(ctx, q, slices.Collect(maps.Keys(markets)))
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the instruments' tickers: %w", err)
	}
	var ts []types.MarketTicker
	for t, ivs := range read {
		if slices.ContainsFunc(ivs, func(iv tickervalidity.Interval) bool { return markets[iv.Instr] == t.Market }) {
			ts = append(ts, t)
		}
	}
	answers, err := Read(ctx, q, ts, always)
	if err != nil {
		return nil, err
	}
	built := map[uuid.UUID]*Calendar{}
	unhandled := map[uuid.UUID][]time.Time{}
	for t, a := range answers {
		for _, iv := range read[t] {
			if markets[iv.Instr] != t.Market {
				continue
			}
			c := built[iv.Instr]
			if c == nil {
				c = &Calendar{Market: t.Market, Answer: Answer{Datasource: a.Datasource}}
				built[iv.Instr] = c
			}
			for _, cov := range a.Coverage {
				if p, ok := iv.Clip(cov); ok {
					c.Coverage = append(c.Coverage, p)
				}
			}
			for _, e := range a.Acts {
				if !iv.Contains(e.ExDate) {
					continue
				}
				if !e.Handled {
					unhandled[iv.Instr] = append(unhandled[iv.Instr], e.ExDate)
				}
				if !slices.ContainsFunc(c.Acts, e.same) {
					c.Acts = append(c.Acts, e)
				}
			}
		}
	}
	for id, c := range built {
		c.Coverage = types.Union(remove(c.Coverage, unhandled[id]))
		if len(c.Coverage) == 0 && len(c.Acts) == 0 {
			continue
		}
		slices.SortFunc(c.Acts, func(a, b Act) int { return a.ExDate.Compare(b.ExDate) })
		out[id] = *c
	}
	return out, nil
}

// Factor returns the factor that restates a quantity as at asAt to today:
// the product of the ratios of the handled actions of c with an ex-date
// after asAt and on or before today. A quantity as at an ex-date already
// follows the action, because exchanges quote a trade on the ex-date in the
// new units. Factor also reports whether c covers every day of that span.
// When c does not, the factor reflects only the actions known so far, so an
// action may be missing. A quantity as at today needs no action, so its factor is
// one and covered.
func (c Calendar) Factor(asAt, today time.Time) (*big.Rat, bool) {
	need := types.Period{From: types.Day(asAt).AddDate(0, 0, 1), Before: types.Day(today).AddDate(0, 0, 1)}
	f := big.NewRat(1, 1)
	if !need.From.Before(need.Before) {
		return f, true
	}
	for _, e := range c.Acts {
		if e.Handled && need.Contains(e.ExDate) {
			f.Mul(f, new(big.Rat).Quo(e.Ratio.To.Rat(), e.Ratio.From.Rat()))
		}
	}
	return f, len(types.Gaps(c.Coverage, need)) == 0
}

// Gaps returns the parts of p that c does not cover, in order.
func (c Calendar) Gaps(p types.Period) []types.Period {
	return types.Gaps(c.Coverage, p)
}

// same reports whether e and o are the same action.
func (e Act) same(o Act) bool {
	return e.ExDate.Equal(o.ExDate) && e.Ratio.Equal(o.Ratio) && e.Handled == o.Handled
}
