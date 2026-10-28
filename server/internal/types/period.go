package types

import "time"

// Period is a half open range of days: it holds From and the days after it,
// up to and not including Before. Each bound is a UTC midnight.
type Period struct {
	From, Before time.Time
}

// Contains reports whether p holds the day d.
func (p Period) Contains(d time.Time) bool {
	return !d.Before(p.From) && d.Before(p.Before)
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
