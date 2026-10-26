package validity

import (
	"time"

	"github.com/leedenison/stonks/server/internal/types"
)

// Gaps returns the parts of p that no confirmed interval of ivs holds, in
// order. ivs is in order, as Derive returns it.
func Gaps(ivs []Interval, p types.Period) []types.Period {
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

// firstUnconfirmed returns the earliest of dates that no confirmed interval of
// ivs holds. It reports false when every date is confirmed.
func firstUnconfirmed(ivs []Interval, dates []time.Time) (time.Time, bool) {
	var first time.Time
	found := false
	for _, d := range dates {
		if confirmedOn(ivs, d) || (found && !d.Before(first)) {
			continue
		}
		first, found = d, true
	}
	return first, found
}

// confirmedOn reports whether a confirmed interval of ivs holds day d.
func confirmedOn(ivs []Interval, d time.Time) bool {
	iv, ok := on(ivs, d)
	return ok && iv.Validity == Confirmed
}

// on returns the interval of ivs that holds day d. It reports false where
// none does.
func on(ivs []Interval, d time.Time) (Interval, bool) {
	for _, iv := range ivs {
		if iv.Contains(d) {
			return iv, true
		}
	}
	return Interval{}, false
}
