package types

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"google.golang.org/protobuf/testing/protocmp"

	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

func TestProtoPeriod(t *testing.T) {
	from := time.Date(2024, time.February, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2024, time.March, 1, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		from, before *time.Time
		want         *typev1.Period
	}{
		{name: "bounded", from: &from, before: &before, want: &typev1.Period{From: ptr.To("2024-02-01"), Before: ptr.To("2024-03-01")}},
		{name: "open start", before: &before, want: &typev1.Period{Before: ptr.To("2024-03-01")}},
		{name: "open end", from: &from, want: &typev1.Period{From: ptr.To("2024-02-01")}},
		{name: "unbounded", want: &typev1.Period{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ProtoPeriod(tc.from, tc.before), protocmp.Transform()); diff != "" {
				t.Errorf("ProtoPeriod(%v, %v) mismatch (-want +got):\n%s", tc.from, tc.before, diff)
			}
		})
	}
	got := Period{From: from, Before: before}.ToProto()
	if diff := cmp.Diff(&typev1.Period{From: ptr.To("2024-02-01"), Before: ptr.To("2024-03-01")}, got, protocmp.Transform()); diff != "" {
		t.Errorf("Period.ToProto mismatch (-want +got):\n%s", diff)
	}
}

// mar is the nth day of March 2026.
func mar(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

// days is the period of March 2026 from day from up to day before.
func days(from, before int) Period { return Period{From: mar(from), Before: mar(before)} }

func TestClip(t *testing.T) {
	tests := []struct {
		name   string
		a, b   Period
		want   Period
		wantOK bool
	}{
		{name: "overlap", a: days(1, 10), b: days(5, 20), want: days(5, 10), wantOK: true},
		{name: "inside", a: days(1, 20), b: days(5, 10), want: days(5, 10), wantOK: true},
		{name: "meet", a: days(1, 5), b: days(5, 10)},
		{name: "apart", a: days(1, 3), b: days(5, 10)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tc.a.Clip(tc.b)
			if ok != tc.wantOK || (ok && got != tc.want) {
				t.Errorf("%v.Clip(%v) = %v, %v, want %v, %v", tc.a, tc.b, got, ok, tc.want, tc.wantOK)
			}
		})
	}
}

func TestUnion(t *testing.T) {
	tests := []struct {
		name string
		ps   []Period
		want []Period
	}{
		{name: "none"},
		{name: "out of order and apart", ps: []Period{days(10, 12), days(1, 3)}, want: []Period{days(1, 3), days(10, 12)}},
		{name: "meet", ps: []Period{days(1, 5), days(5, 10)}, want: []Period{days(1, 10)}},
		{name: "overlap and inside", ps: []Period{days(1, 8), days(2, 4), days(6, 10)}, want: []Period{days(1, 10)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, Union(tc.ps)); diff != "" {
				t.Errorf("Union(%v) mismatch (-want +got):\n%s", tc.ps, diff)
			}
		})
	}
}

func TestAnyContains(t *testing.T) {
	ps := []Period{days(1, 3), days(10, 12)}
	for _, tc := range []struct {
		day  int
		want bool
	}{{day: 1, want: true}, {day: 3}, {day: 11, want: true}, {day: 12}} {
		if got := AnyContains(ps, mar(tc.day)); got != tc.want {
			t.Errorf("AnyContains(%v, day %d) = %v, want %v", ps, tc.day, got, tc.want)
		}
	}
}

func TestGaps(t *testing.T) {
	tests := []struct {
		name    string
		covered []Period
		want    []Period
	}{
		{name: "nothing covered", want: []Period{days(5, 20)}},
		{name: "all covered", covered: []Period{days(1, 25)}},
		{name: "middle covered", covered: []Period{days(8, 12)}, want: []Period{days(5, 8), days(12, 20)}},
		{name: "overlapping covers out of order", covered: []Period{days(14, 25), days(1, 9), days(8, 12)}, want: []Period{days(12, 14)}},
		{name: "covers outside", covered: []Period{days(1, 3), days(22, 25)}, want: []Period{days(5, 20)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, Gaps(tc.covered, days(5, 20))); diff != "" {
				t.Errorf("Gaps(%v, %v) mismatch (-want +got):\n%s", tc.covered, days(5, 20), diff)
			}
		})
	}
}
