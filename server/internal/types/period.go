package types

import (
	"slices"
	"time"

	typev1 "github.com/leedenison/stonks/proto/type/v1"
)

// Period is a half open range of days: it holds From and the days after it,
// up to and not including Before. Each bound is a UTC midnight.
type Period struct {
	From, Before time.Time
}

// Contains reports whether p holds the day d.
func (p Period) Contains(d time.Time) bool {
	return !d.Before(p.From) && d.Before(p.Before)
}

// Clip returns the days that p and q both hold. It reports false when they
// share none.
func (p Period) Clip(q Period) (Period, bool) {
	if q.From.After(p.From) {
		p.From = q.From
	}
	if q.Before.Before(p.Before) {
		p.Before = q.Before
	}
	return p, p.From.Before(p.Before)
}

// Union returns the days of ps as periods in order, joining those that
// overlap or meet.
func Union(ps []Period) []Period {
	ps = slices.Clone(ps)
	slices.SortFunc(ps, func(a, b Period) int { return a.From.Compare(b.From) })
	var out []Period
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

// AnyContains reports whether one of ps holds the day d.
func AnyContains(ps []Period, d time.Time) bool {
	return slices.ContainsFunc(ps, func(p Period) bool { return p.Contains(d) })
}

// Gaps returns the parts of p that none of covered holds, in order.
func Gaps(covered []Period, p Period) []Period {
	var out []Period
	d := p.From
	for _, c := range Union(covered) {
		c, ok := c.Clip(p)
		if !ok {
			continue
		}
		if d.Before(c.From) {
			out = append(out, Period{From: d, Before: c.From})
		}
		d = c.Before
	}
	if d.Before(p.Before) {
		out = append(out, Period{From: d, Before: p.Before})
	}
	return out
}

// ToProto converts p.
func (p Period) ToProto() *typev1.Period {
	return ProtoPeriod(&p.From, &p.Before)
}

// ProtoPeriod converts the bounds of a period, either of which may be open.
func ProtoPeriod(from, before *time.Time) *typev1.Period {
	out := &typev1.Period{}
	if from != nil {
		s := from.Format(time.DateOnly)
		out.From = &s
	}
	if before != nil {
		s := before.Format(time.DateOnly)
		out.Before = &s
	}
	return out
}

// OneDay returns the period that holds only the day of t.
func OneDay(t time.Time) Period {
	d := Day(t)
	return Period{From: d, Before: d.AddDate(0, 0, 1)}
}

// Day returns the UTC day of t, as a UTC midnight.
func Day(t time.Time) time.Time {
	t = t.UTC()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}
