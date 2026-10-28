package tickervalidity

import (
	"time"

	"github.com/leedenison/stonks/server/internal/types"
)

// Intervals is the ticker validity of one ticker, in order of its days.
type Intervals []Interval

// Any reports whether the ticker names an instrument on any day.
func (ivs Intervals) Any() bool { return len(ivs) > 0 }

// On returns the interval that holds day d. It reports false where the ticker
// names nothing on d.
func (ivs Intervals) On(d time.Time) (Interval, bool) {
	for _, iv := range ivs {
		if iv.Contains(d) {
			return iv, true
		}
	}
	return Interval{}, false
}

// FirstUnconfirmed returns the earliest of dates that no confirmed interval
// holds. It reports false when every date is confirmed.
func (ivs Intervals) FirstUnconfirmed(dates []time.Time) (time.Time, bool) {
	var first time.Time
	found := false
	for _, d := range dates {
		if ivs.confirmedOn(d) || (found && !d.Before(first)) {
			continue
		}
		first, found = d, true
	}
	return first, found
}

func (ivs Intervals) confirmedOn(d time.Time) bool {
	iv, ok := ivs.On(d)
	return ok && iv.Validity == Confirmed
}

// Gaps returns the parts of p that no confirmed interval holds, in order.
func (ivs Intervals) Gaps(p types.Period) []types.Period {
	var out []types.Period
	d := p.From
	for _, iv := range ivs {
		c, ok := iv.Clip(p)
		if iv.Validity != Confirmed || !ok {
			continue
		}
		if d.Before(c.From) {
			out = append(out, types.Period{From: d, Before: c.From})
		}
		d = c.Before
	}
	if d.Before(p.Before) {
		out = append(out, types.Period{From: d, Before: p.Before})
	}
	return out
}
