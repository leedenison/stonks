package tickervalidity

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/types"
)

var (
	a = uuid.MustParse("00000000-0000-0000-0000-00000000000a")
	b = uuid.MustParse("00000000-0000-0000-0000-00000000000b")

	alpha = Source{Datasource: "alpha", Precedence: 1}
	beta  = Source{Datasource: "beta", Precedence: 2}
	admin = Source{Attested: true}
)

// d is the nth day of March 2026.
func d(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

func at(n int) *time.Time {
	t := d(n)
	return &t
}

func asserts(n int, instr *uuid.UUID, src Source) Assertion {
	return Assertion{Period: oneDay(n), Instr: instr, Source: src}
}

// holds is an assertion that names instr on each day in cover(from, before).
func holds(from, before int, instr *uuid.UUID, src Source) Assertion {
	return Assertion{Period: cover(from, before), Instr: instr, Source: src}
}

// oneDay is the period of the nth day of March 2026.
func oneDay(n int) types.Period { return types.OneDay(d(n)) }

func confirmed(from, before *time.Time, instr uuid.UUID) Interval {
	return Interval{From: from, Before: before, Instr: instr, Validity: Confirmed}
}

func provisional(from, before *time.Time, instr uuid.UUID) Interval {
	return Interval{From: from, Before: before, Instr: instr, Validity: Provisional}
}

func cover(from, before int) types.Period { return types.Period{From: d(from), Before: d(before)} }

// coverageFetchKey is the fetch key of every period of assertion coverage a
// test states.
var coverageFetchKey = uuid.MustParse("00000000-0000-0000-0000-0000000000c0")

func covered(from, before int) AssertionCoverage {
	return AssertionCoverage{Period: cover(from, before), Source: Source{Datasource: "alpha", Precedence: 1, FetchKey: coverageFetchKey}}
}

// first asserts instr on the nth day, with an edge at the start of the day.
func first(n int, instr *uuid.UUID, src Source) Assertion {
	a := asserts(n, instr, src)
	a.EdgeFrom = true
	return a
}

// last asserts instr on the nth day, with an edge at the start of the next
// day.
func last(n int, instr *uuid.UUID, src Source) Assertion {
	a := asserts(n, instr, src)
	a.EdgeBefore = true
	return a
}

// edgeOf is the edge that a, made by first or last, states.
func edgeOf(a Assertion) Edge {
	if a.EdgeFrom {
		return Edge{At: a.Period.From, Assertion: a}
	}
	return Edge{At: a.Period.Before, Assertion: a}
}

func TestDerive(t *testing.T) {
	tests := []struct {
		name              string
		facts             Facts
		want              []Interval
		wantInferred      []Inferred
		wantDisagreements []Disagreement
		wantBroken        []BrokenBracket
		wantMissed        []MissedEvent
		wantOverruled     []OverruledEdge
	}{
		{
			name:  "an assertion confirms its own day and nothing either side",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), nil, a)},
		},
		{
			name:  "two assertions of one instrument bracket the days between them",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, &a, beta)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(6), a), provisional(at(6), nil, a)},
		},
		{
			name:  "a period of assertion coverage extends confirmation up to the nearest cut",
			facts: Facts{Assertions: []Assertion{asserts(5, &a, alpha), last(9, &a, alpha)}, AssertionCoverage: []AssertionCoverage{covered(1, 20)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(10), a)},
		},
		{
			name:  "a first cuts at the start of its day",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), first(5, &b, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(5), at(6), b), provisional(at(6), nil, b),
			},
		},
		{
			name:  "a last cuts at the start of the next day",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), last(4, &a, alpha)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(5), a)},
		},
		{
			name:  "a first and a last on one day hold the ticker for that day",
			facts: Facts{Assertions: []Assertion{first(5, &a, alpha), last(5, &a, alpha)}},
			want:  []Interval{confirmed(at(5), at(6), a)},
		},
		{
			name:  "an edge across an equal bracket cuts it",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), first(5, &a, alpha), asserts(9, &a, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(5), at(10), a), provisional(at(10), nil, a),
			},
			wantBroken: []BrokenBracket{{Edge: edgeOf(first(5, &a, alpha)), Instr: a, First: d(1), Last: d(5)}},
		},
		{
			name:          "a bracket of a stronger datasource overrules a weaker edge",
			facts:         Facts{Assertions: []Assertion{asserts(1, &a, alpha), first(5, &a, beta), asserts(9, &a, alpha)}},
			want:          []Interval{provisional(nil, at(1), a), confirmed(at(1), at(10), a), provisional(at(10), nil, a)},
			wantOverruled: []OverruledEdge{{Edge: edgeOf(first(5, &a, beta)), By: []Source{alpha, alpha}}},
		},
		{
			name:  "an overruled edge's assertion still wins its day",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), first(5, &b, beta), asserts(9, &a, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(5), at(6), b), provisional(at(6), at(9), b),
				confirmed(at(9), at(10), a), provisional(at(10), nil, a),
			},
			wantInferred: []Inferred{
				{Period: cover(2, 6), From: &a, To: &b, Earlier: asserts(1, &a, alpha), Later: first(5, &b, beta)},
				{Period: cover(6, 10), From: &b, To: &a, Earlier: first(5, &b, beta), Later: asserts(9, &a, alpha)},
			},
			wantOverruled: []OverruledEdge{{Edge: edgeOf(first(5, &b, beta)), By: []Source{alpha, alpha}}},
		},
		{
			name:          "a period of assertion coverage of a stronger datasource overrules a weaker edge",
			facts:         Facts{Assertions: []Assertion{asserts(1, &a, beta), first(5, &a, beta)}, AssertionCoverage: []AssertionCoverage{covered(1, 20)}},
			want:          []Interval{provisional(nil, at(1), a), confirmed(at(1), at(20), a), provisional(at(20), nil, a)},
			wantOverruled: []OverruledEdge{{Edge: edgeOf(first(5, &a, beta)), By: []Source{covered(1, 20).Source}}},
		},
		{
			name:  "a period of assertion coverage overrules nothing where its datasource states an edge at the cut",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, beta), first(5, &a, beta), first(5, &a, alpha)}, AssertionCoverage: []AssertionCoverage{covered(1, 20)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(20), a), provisional(at(20), nil, a)},
			wantBroken: []BrokenBracket{
				{Edge: edgeOf(first(5, &a, beta)), Instr: a, First: d(1), Last: d(5)},
				{Edge: edgeOf(first(5, &a, alpha)), Instr: a, First: d(1), Last: d(5)},
			},
		},
		{
			name:          "two adjoining periods of assertion coverage of one datasource overrule an edge where they meet",
			facts:         Facts{Assertions: []Assertion{asserts(1, &a, beta), first(5, &a, beta)}, AssertionCoverage: []AssertionCoverage{covered(1, 5), covered(5, 20)}},
			want:          []Interval{provisional(nil, at(1), a), confirmed(at(1), at(20), a), provisional(at(20), nil, a)},
			wantOverruled: []OverruledEdge{{Edge: edgeOf(first(5, &a, beta)), By: []Source{covered(5, 20).Source}}},
		},
		{
			name:          "an identifier attestation on both sides overrules a datasource edge",
			facts:         Facts{Assertions: []Assertion{asserts(1, &a, admin), last(4, &a, alpha), asserts(9, &a, admin)}},
			want:          []Interval{provisional(nil, at(1), a), confirmed(at(1), at(10), a), provisional(at(10), nil, a)},
			wantOverruled: []OverruledEdge{{Edge: edgeOf(last(4, &a, alpha)), By: []Source{admin, admin}}},
		},
		{
			name:  "two instruments on different days imply a change, the earlier keeping the grey zone",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, &b, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(5), at(6), b), provisional(at(6), nil, b),
			},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a, To: &b, Earlier: asserts(1, &a, alpha), Later: asserts(5, &b, alpha)}},
		},
		{
			name:  "a period of assertion coverage of the grey zone confirms the earlier instrument",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(9, &b, alpha)}, AssertionCoverage: []AssertionCoverage{covered(1, 20)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(9), a),
				confirmed(at(9), at(20), b), provisional(at(20), nil, b),
			},
			wantInferred: []Inferred{{Period: cover(9, 10), From: &a, To: &b, Earlier: asserts(1, &a, alpha), Later: asserts(9, &b, alpha)}},
			wantMissed: []MissedEvent{{
				Inferred: Inferred{Period: cover(9, 10), From: &a, To: &b, Earlier: asserts(1, &a, alpha), Later: asserts(9, &b, alpha)},
				By:       covered(1, 20),
			}},
		},
		{
			name:              "on one day the higher precedence wins, and the loser disagrees",
			facts:             Facts{Assertions: []Assertion{asserts(1, &b, beta), asserts(1, &a, alpha)}},
			want:              []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), nil, a)},
			wantDisagreements: []Disagreement{{Period: oneDay(1), Winner: asserts(1, &a, alpha), Loser: asserts(1, &b, beta)}},
		},
		{
			name:  "two datasources that name one instrument on one day agree",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, beta), asserts(1, &a, alpha)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), nil, a)},
		},
		{
			name:              "a datasource that knew the ticker and names nothing disagrees with one that names it",
			facts:             Facts{Assertions: []Assertion{asserts(1, &a, beta), asserts(3, &a, alpha), asserts(3, nil, beta)}},
			want:              []Interval{provisional(nil, at(1), a), confirmed(at(1), at(4), a), provisional(at(4), nil, a)},
			wantDisagreements: []Disagreement{{Period: oneDay(3), Winner: asserts(3, &a, alpha), Loser: asserts(3, nil, beta)}},
		},
		{
			name:  "on one day an administrator outranks every datasource, with no disagreement",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(1, &b, admin)}},
			want:  []Interval{provisional(nil, at(1), b), confirmed(at(1), at(2), b), provisional(at(2), nil, b)},
		},
		{
			name:         "a datasource's negative assertion after its own positive one retires the ticker",
			facts:        Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, alpha)}},
			want:         []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a)},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a, Earlier: asserts(1, &a, alpha), Later: asserts(5, nil, alpha)}},
		},
		{
			name:  "a negative assertion from a datasource that never asserted the ticker counts for nothing",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, beta)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), nil, a)},
		},
		{
			name:         "an administrator's negative assertion counts after any positive one",
			facts:        Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, admin)}},
			want:         []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a)},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a, Earlier: asserts(1, &a, alpha), Later: asserts(5, nil, admin)}},
		},
		{
			name:  "a ticker reassigned after it retired names nothing between",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, alpha), asserts(9, &b, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(9), at(10), b), provisional(at(10), nil, b),
			},
			wantInferred: []Inferred{
				{Period: cover(2, 6), From: &a, Earlier: asserts(1, &a, alpha), Later: asserts(5, nil, alpha)},
				{Period: cover(6, 10), To: &b, Earlier: asserts(5, nil, alpha), Later: asserts(9, &b, alpha)},
			},
		},
		{
			name:  "an assertion confirms every day of its period",
			facts: Facts{Assertions: []Assertion{holds(1, 5, &a, alpha)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(5), a), provisional(at(5), nil, a)},
		},
		{
			name:              "a weaker assertion inside a period loses each of its days, and disagrees once",
			facts:             Facts{Assertions: []Assertion{holds(1, 10, &a, alpha), holds(3, 5, &b, beta)}},
			want:              []Interval{provisional(nil, at(1), a), confirmed(at(1), at(10), a), provisional(at(10), nil, a)},
			wantDisagreements: []Disagreement{{Period: cover(3, 5), Winner: holds(1, 10, &a, alpha), Loser: holds(3, 5, &b, beta)}},
		},
		{
			name:          "a stronger assertion that holds the days around an edge overrules it by itself",
			facts:         Facts{Assertions: []Assertion{holds(1, 10, &a, alpha), first(5, &a, beta)}},
			want:          []Interval{provisional(nil, at(1), a), confirmed(at(1), at(10), a), provisional(at(10), nil, a)},
			wantOverruled: []OverruledEdge{{Edge: edgeOf(first(5, &a, beta)), By: []Source{alpha, alpha}}},
		},
		{
			name:  "a period of assertion coverage with no assertion names nothing",
			facts: Facts{AssertionCoverage: []AssertionCoverage{covered(1, 20)}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := Derive(tc.facts)
			if diff := cmp.Diff(tc.want, got.Intervals); diff != "" {
				t.Errorf("Derive(%+v) intervals mismatch (-want +got):\n%s", tc.facts, diff)
			}
			if diff := cmp.Diff(tc.wantInferred, got.Inferred); diff != "" {
				t.Errorf("Derive(%+v) inferred mismatch (-want +got):\n%s", tc.facts, diff)
			}
			if diff := cmp.Diff(tc.wantDisagreements, got.Disagreements); diff != "" {
				t.Errorf("Derive(%+v) disagreements mismatch (-want +got):\n%s", tc.facts, diff)
			}
			if diff := cmp.Diff(tc.wantBroken, got.Broken); diff != "" {
				t.Errorf("Derive(%+v) broken brackets mismatch (-want +got):\n%s", tc.facts, diff)
			}
			if diff := cmp.Diff(tc.wantMissed, got.Missed); diff != "" {
				t.Errorf("Derive(%+v) missed events mismatch (-want +got):\n%s", tc.facts, diff)
			}
			if diff := cmp.Diff(tc.wantOverruled, got.Overruled); diff != "" {
				t.Errorf("Derive(%+v) overruled edges mismatch (-want +got):\n%s", tc.facts, diff)
			}
		})
	}
}

func TestGaps(t *testing.T) {
	ivs := []Interval{
		provisional(nil, at(1), a), confirmed(at(1), at(3), a), provisional(at(3), at(5), a),
		confirmed(at(5), at(8), b), provisional(at(8), nil, b),
	}
	tests := []struct {
		name string
		p    types.Period
		want []types.Period
	}{
		{name: "the provisional days of a period", p: cover(1, 12), want: []types.Period{cover(3, 5), cover(8, 12)}},
		{name: "a period inside confirmed days", p: cover(1, 3)},
		{name: "a period before every interval", p: types.Period{From: d(1).AddDate(0, 0, -10), Before: d(2)}, want: []types.Period{{From: d(1).AddDate(0, 0, -10), Before: d(1)}}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, Gaps(ivs, tc.p)); diff != "" {
				t.Errorf("Gaps(%v) mismatch (-want +got):\n%s", tc.p, diff)
			}
		})
	}
}

func TestOn(t *testing.T) {
	ivs := []Interval{provisional(nil, at(1), a), confirmed(at(1), at(3), a), provisional(at(3), at(5), a), confirmed(at(7), nil, b)}
	tests := []struct {
		name   string
		day    time.Time
		want   Interval
		wantOK bool
	}{
		{name: "before every bound", day: d(0), want: ivs[0], wantOK: true},
		{name: "a confirmed day", day: d(2), want: ivs[1], wantOK: true},
		{name: "a provisional day", day: d(4), want: ivs[2], wantOK: true},
		{name: "the day an interval ends", day: d(5)},
		{name: "a day no interval holds", day: d(6)},
		{name: "after every bound", day: d(30), want: ivs[3], wantOK: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := on(ivs, tc.day)
			if ok != tc.wantOK || !cmp.Equal(got, tc.want) {
				t.Errorf("on(%s) = %+v, %v, want %+v, %v", tc.day.Format(time.DateOnly), got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestFirstUnconfirmed(t *testing.T) {
	ivs := []Interval{provisional(nil, at(1), a), confirmed(at(1), at(3), a), provisional(at(3), at(5), a), confirmed(at(5), at(8), b)}
	tests := []struct {
		name   string
		dates  []time.Time
		want   time.Time
		wantOK bool
	}{
		{name: "every date confirmed", dates: []time.Time{d(2), d(6)}},
		{name: "a provisional date", dates: []time.Time{d(6), d(4), d(2)}, want: d(4), wantOK: true},
		{name: "the earliest of two", dates: []time.Time{d(9), d(4)}, want: d(4), wantOK: true},
		{name: "a date outside every interval", dates: []time.Time{d(2), d(12)}, want: d(12), wantOK: true},
		{name: "no dates"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := firstUnconfirmed(ivs, tc.dates)
			if ok != tc.wantOK || !got.Equal(tc.want) {
				t.Errorf("firstUnconfirmed(%v) = %s, %v, want %s, %v", tc.dates, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}
