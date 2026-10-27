package corpact

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func ratio(from, to int64) types.Ratio {
	return types.Ratio{From: decimal.NewFromInt(from), To: decimal.NewFromInt(to)}
}

func TestCovered(t *testing.T) {
	at := time.Date(2026, 3, 2, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name         string
		c            *types.Period
		from, before *time.Time
	}{
		{name: "coverage that ends before the answer", c: &types.Period{From: day(2020, 1, 1), Before: day(2025, 1, 1)}, from: ptr.To(day(2020, 1, 1)), before: ptr.To(day(2025, 1, 1))},
		{name: "coverage past the answer ends on its day", c: &types.Period{From: day(2020, 1, 1), Before: day(2027, 1, 1)}, from: ptr.To(day(2020, 1, 1)), before: ptr.To(day(2026, 3, 3))},
		{name: "coverage that starts after the answer", c: &types.Period{From: day(2026, 4, 1), Before: day(2027, 1, 1)}},
		{name: "no coverage"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			from, before := covered(tc.c, at)
			if diff := cmp.Diff([]*time.Time{tc.from, tc.before}, []*time.Time{from, before}); diff != "" {
				t.Errorf("covered(%v, %s) mismatch (-want +got):\n%s", tc.c, at, diff)
			}
		})
	}
}

// TestMerge checks that a stored action keeps its row only for an equal
// reported action, and that two equal actions on one day pair one each.
func TestMerge(t *testing.T) {
	stored := func(ex time.Time, r types.Ratio, typ string, handled bool) gen.CorpAct {
		e := gen.CorpAct{ID: uuid.New(), ExDate: ex, RatioFrom: r.From, RatioTo: r.To, Handled: handled}
		if typ != "" {
			e.Type = &typ
		}
		return e
	}
	split := stored(day(2024, 6, 10), ratio(1, 10), "forward_split", true)
	twin := stored(day(2024, 6, 10), ratio(1, 10), "forward_split", true)
	spin := stored(day(2025, 1, 2), ratio(1, 1), "spin_off", false)
	reported := []market.CorpAct{
		{ExDate: time.Date(2024, 6, 10, 9, 0, 0, 0, time.UTC), Ratio: ratio(2, 20), Type: "forward_split", Handled: true},
		{ExDate: day(2025, 1, 2), Ratio: ratio(1, 1), Type: "spin_off", Handled: true},
		{ExDate: day(2025, 7, 1), Ratio: ratio(1, 2)},
	}
	gone, added := merge([]gen.CorpAct{split, twin, spin}, reported)
	if diff := cmp.Diff([]uuid.UUID{twin.ID, spin.ID}, gone); diff != "" {
		t.Errorf("gone mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(reported[1:], added); diff != "" {
		t.Errorf("added mismatch (-want +got):\n%s", diff)
	}
}

func TestRestatesShares(t *testing.T) {
	dec := func(from, to string) types.Ratio {
		return types.Ratio{From: decimal.RequireFromString(from), To: decimal.RequireFromString(to)}
	}
	tests := []struct {
		name string
		r    types.Ratio
		want bool
	}{
		{name: "a forward split", r: ratio(1, 4), want: true},
		{name: "a three for two split", r: ratio(2, 3), want: true},
		{name: "a reverse split", r: ratio(300, 1), want: true},
		{name: "a five percent stock dividend", r: ratio(20, 21), want: true},
		{name: "the largest smaller side", r: ratio(100, 101), want: true},
		{name: "a large side beside a side of one", r: ratio(1000, 1), want: true},
		{name: "a ratio plain once reduced", r: ratio(2, 200), want: true},
		{name: "a reverse ratio plain once reduced", r: ratio(300, 3), want: true},
		{name: "whole sides written as decimals", r: dec("1.0", "50.0"), want: true},
		{name: "a spin-off factor", r: ratio(1000, 1281)},
		{name: "a small spin-off factor", r: ratio(1000, 1046)},
		{name: "a fractional stock dividend", r: dec("24.47", "25.47")},
		{name: "a fractional side", r: dec("25", "24.25")},
		{name: "a smaller side past the largest", r: ratio(101, 102)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := restatesShares(tc.r); got != tc.want {
				t.Errorf("restatesShares(%s) = %v, want %v", ratioText(tc.r), got, tc.want)
			}
		})
	}
}
