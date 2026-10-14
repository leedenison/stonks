package validity

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Gaps returns the parts of p that no confirmed interval of ivs holds, in
// order. ivs is in order, as Derive returns it.
func Gaps(ivs []Interval, p types.Period) []types.Period {
	var out []types.Period
	d := p.From
	for _, iv := range ivs {
		if iv.Validity != gen.ValidityConfirmed {
			continue
		}
		if iv.Before != nil && !d.Before(*iv.Before) {
			continue
		}
		if iv.From != nil && !iv.From.Before(p.Before) {
			break
		}
		if iv.From != nil && d.Before(*iv.From) {
			out = append(out, types.Period{From: d, Before: *iv.From})
		}
		if iv.Before == nil {
			return out
		}
		d = *iv.Before
	}
	if d.Before(p.Before) {
		out = append(out, types.Period{From: d, Before: p.Before})
	}
	return out
}

// gaps returns the parts of p that no period of cover holds. cover is a
// union, as union returns it.
func gaps(cover []types.Period, p types.Period) []types.Period {
	ivs := make([]Interval, len(cover))
	for i, c := range cover {
		from, before := c.From, c.Before
		ivs[i] = Interval{From: &from, Before: &before, Validity: gen.ValidityConfirmed}
	}
	return Gaps(ivs, p)
}
