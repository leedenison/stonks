package corpact

import (
	"cmp"
	"context"
	"fmt"
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
	tickervalidity.ValidityQueries
	ListTickersNaming(ctx context.Context, instrs []uuid.UUID) ([]gen.ListTickersNamingRow, error)
}

var _ CalendarQueries = (*gen.Queries)(nil)

// MarketAnswer is what one datasource reported at one market for one
// instrument, over the days that a ticker of the market named it. Coverage
// leaves out the ex-date of every unhandled action of the instrument,
// whichever market reported it. Acts holds an action once when two tickers
// of the market report it.
type MarketAnswer struct {
	Answer
	Market string
}

// Calendar is the market answers of one instrument, by datasource
// precedence, then by market.
type Calendar []MarketAnswer

// Calendars returns the calendar of each of instrs that a datasource has
// answered for. Ticker validity attributes each action and each day of
// coverage to the instrument that its ticker named on that day. Calendars
// reads with four queries however many instruments there are.
func Calendars(ctx context.Context, q CalendarQueries, instrs []uuid.UUID) (map[uuid.UUID]Calendar, error) {
	out := map[uuid.UUID]Calendar{}
	if len(instrs) == 0 {
		return out, nil
	}
	rows, err := q.ListTickersNaming(ctx, instrs)
	if err != nil {
		return nil, fmt.Errorf("list the tickers of the instruments: %w", err)
	}
	var ts []types.MarketTicker
	for _, row := range rows {
		if t := (types.MarketTicker{Market: row.Market, Value: row.Value}); !slices.Contains(ts, t) {
			ts = append(ts, t)
		}
	}
	read, err := tickervalidity.Load(ctx, q, ts)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the instruments' tickers: %w", err)
	}
	answers, err := Read(ctx, q, ts, always)
	if err != nil {
		return nil, err
	}
	type at struct {
		instr              uuid.UUID
		datasource, market string
	}
	built := map[at]*MarketAnswer{}
	unhandled := map[uuid.UUID][]time.Time{}
	for t, as := range answers {
		for _, a := range as {
			for _, iv := range read[t] {
				if !slices.Contains(instrs, iv.Instr) {
					continue
				}
				k := at{instr: iv.Instr, datasource: a.Datasource, market: t.Market}
				m := built[k]
				if m == nil {
					m = &MarketAnswer{Answer: Answer{Datasource: a.Datasource, Precedence: a.Precedence}, Market: t.Market}
					built[k] = m
				}
				for _, c := range a.Coverage {
					if p, ok := iv.Clip(c); ok {
						m.Coverage = append(m.Coverage, p)
					}
				}
				for _, e := range a.Acts {
					if !iv.Contains(e.ExDate) {
						continue
					}
					if !e.Handled {
						unhandled[iv.Instr] = append(unhandled[iv.Instr], e.ExDate)
					}
					if !slices.ContainsFunc(m.Acts, e.same) {
						m.Acts = append(m.Acts, e)
					}
				}
			}
		}
	}
	for k, m := range built {
		m.Coverage = union(remove(m.Coverage, unhandled[k.instr]))
		if len(m.Coverage) == 0 && len(m.Acts) == 0 {
			continue
		}
		slices.SortFunc(m.Acts, func(a, b Act) int { return a.ExDate.Compare(b.ExDate) })
		out[k.instr] = append(out[k.instr], *m)
	}
	for _, c := range out {
		slices.SortFunc(c, func(a, b MarketAnswer) int {
			return cmp.Or(cmp.Compare(a.Precedence, b.Precedence), cmp.Compare(a.Market, b.Market))
		})
	}
	return out, nil
}

// Factor returns the factor that restates a quantity as at asAt to today:
// the product of the ratios of the handled actions with an ex-date after
// asAt and on or before today. A quantity as at an ex-date already follows
// the action, because exchanges quote a trade on the ex-date in the new
// units. It takes the actions from the first market answer of c that covers
// every day of that span. It reports false when none does. A quantity as at
// today needs no action, so its factor is one.
func (c Calendar) Factor(asAt, today time.Time) (*big.Rat, bool) {
	need := types.Period{From: types.Day(asAt).AddDate(0, 0, 1), Before: types.Day(today).AddDate(0, 0, 1)}
	f := big.NewRat(1, 1)
	if !need.From.Before(need.Before) {
		return f, true
	}
	for _, m := range c {
		if !spans(m.Coverage, need) {
			continue
		}
		for _, e := range m.Acts {
			if e.Handled && need.Contains(e.ExDate) {
				f.Mul(f, new(big.Rat).Quo(e.Ratio.To.Rat(), e.Ratio.From.Rat()))
			}
		}
		return f, true
	}
	return nil, false
}

// Gaps returns the parts of p that no market answer of c covers, in order.
func (c Calendar) Gaps(p types.Period) []types.Period {
	var all []types.Period
	for _, m := range c {
		all = append(all, m.Coverage...)
	}
	var out []types.Period
	d := p.From
	for _, cov := range union(all) {
		cov, ok := clip(cov, p)
		if !ok {
			continue
		}
		if d.Before(cov.From) {
			out = append(out, types.Period{From: d, Before: cov.From})
		}
		if cov.Before.After(d) {
			d = cov.Before
		}
	}
	if d.Before(p.Before) {
		out = append(out, types.Period{From: d, Before: p.Before})
	}
	return out
}

// same reports whether e and o are the same action.
func (e Act) same(o Act) bool {
	return e.ExDate.Equal(o.ExDate) && e.Ratio.Equal(o.Ratio) && e.Handled == o.Handled
}

// union returns the days of ps as periods in order, joining those that
// overlap or touch.
func union(ps []types.Period) []types.Period {
	ps = slices.Clone(ps)
	slices.SortFunc(ps, func(a, b types.Period) int { return a.From.Compare(b.From) })
	var out []types.Period
	for _, p := range ps {
		if n := len(out); n > 0 && !p.From.After(out[n-1].Before) {
			if p.Before.After(out[n-1].Before) {
				out[n-1].Before = p.Before
			}
			continue
		}
		out = append(out, p)
	}
	return out
}

// spans reports whether one of ps, which union has joined, holds every day
// of p.
func spans(ps []types.Period, p types.Period) bool {
	return slices.ContainsFunc(ps, func(c types.Period) bool {
		return !c.From.After(p.From) && !c.Before.Before(p.Before)
	})
}
