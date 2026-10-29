package types

import (
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
