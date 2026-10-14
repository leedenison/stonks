package validity

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
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

func asserts(n int, inst *uuid.UUID, src Source) Assertion {
	return Assertion{At: d(n), Instrument: inst, Source: src}
}

func confirmed(from, before *time.Time, inst uuid.UUID) Interval {
	return Interval{From: from, Before: before, Instrument: inst, Validity: gen.ValidityConfirmed}
}

func provisional(from, before *time.Time, inst uuid.UUID) Interval {
	return Interval{From: from, Before: before, Instrument: inst, Validity: gen.ValidityProvisional}
}

func cover(from, before int) types.Period { return types.Period{From: d(from), Before: d(before)} }

func TestDerive(t *testing.T) {
	tests := []struct {
		name         string
		facts        Facts
		want         []Interval
		wantInferred []Inferred
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
			name:  "coverage extends confirmation up to the nearest event",
			facts: Facts{Assertions: []Assertion{asserts(5, &a, alpha)}, Events: []time.Time{d(10)}, Coverage: []types.Period{cover(1, 20)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(10), a)},
		},
		{
			name:  "provisional validity ends at the nearest event",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha)}, Events: []time.Time{d(5)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a)},
		},
		{
			name:  "an event inside a bracket breaks it",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(9, &a, alpha)}, Events: []time.Time{d(5)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(9), a),
				confirmed(at(9), at(10), a), provisional(at(10), nil, a),
			},
		},
		{
			name:  "an event precedes an assertion on its day",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, &b, alpha)}, Events: []time.Time{d(5)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(5), at(6), b), provisional(at(6), nil, b),
			},
		},
		{
			name:  "two instruments on different days imply an event, the earlier keeping the grey zone",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, &b, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(5), at(6), b), provisional(at(6), nil, b),
			},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a, To: &b}},
		},
		{
			name:  "coverage of the grey zone confirms the earlier instrument",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(9, &b, alpha)}, Coverage: []types.Period{cover(1, 20)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(9), a),
				confirmed(at(9), at(20), b), provisional(at(20), nil, b),
			},
			wantInferred: []Inferred{{Period: cover(9, 10), From: &a, To: &b}},
		},
		{
			name:  "on one day the higher precedence wins",
			facts: Facts{Assertions: []Assertion{asserts(1, &b, beta), asserts(1, &a, alpha)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), nil, a)},
		},
		{
			name:  "on one day an administrator outranks every datasource",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(1, &b, admin)}},
			want:  []Interval{provisional(nil, at(1), b), confirmed(at(1), at(2), b), provisional(at(2), nil, b)},
		},
		{
			name:         "a datasource's negative assertion after its own positive one retires the value",
			facts:        Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, alpha)}},
			want:         []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a)},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a}},
		},
		{
			name:  "a negative assertion from a datasource that never asserted the value counts for nothing",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, beta)}},
			want:  []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), nil, a)},
		},
		{
			name:         "an administrator's negative assertion counts after any positive one",
			facts:        Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, admin)}},
			want:         []Interval{provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a)},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a}},
		},
		{
			name:  "a value reassigned after it retired names nothing between",
			facts: Facts{Assertions: []Assertion{asserts(1, &a, alpha), asserts(5, nil, alpha), asserts(9, &b, alpha)}},
			want: []Interval{
				provisional(nil, at(1), a), confirmed(at(1), at(2), a), provisional(at(2), at(5), a),
				confirmed(at(9), at(10), b), provisional(at(10), nil, b),
			},
			wantInferred: []Inferred{{Period: cover(2, 6), From: &a}, {Period: cover(6, 10), To: &b}},
		},
		{
			name:  "events and coverage with no assertion name nothing",
			facts: Facts{Events: []time.Time{d(5)}, Coverage: []types.Period{cover(1, 20)}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, inferred := Derive(tc.facts)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Derive(%+v) intervals mismatch (-want +got):\n%s", tc.facts, diff)
			}
			if diff := cmp.Diff(tc.wantInferred, inferred); diff != "" {
				t.Errorf("Derive(%+v) inferred mismatch (-want +got):\n%s", tc.facts, diff)
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
