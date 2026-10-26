package corporate

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
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

// TestMerge checks that a stored event keeps its row only for an equal
// reported event, and that two equal events on one day pair one each.
func TestMerge(t *testing.T) {
	stored := func(ex time.Time, r types.Ratio, typ string, handled bool) gen.CorporateEvent {
		e := gen.CorporateEvent{ID: uuid.New(), ExDate: ex, RatioFrom: r.From, RatioTo: r.To, Handled: handled}
		if typ != "" {
			e.Type = &typ
		}
		return e
	}
	split := stored(day(2024, 6, 10), ratio(1, 10), "forward_split", true)
	twin := stored(day(2024, 6, 10), ratio(1, 10), "forward_split", true)
	spin := stored(day(2025, 1, 2), ratio(1, 1), "spin_off", false)
	reported := []market.CorporateEvent{
		{ExDate: time.Date(2024, 6, 10, 9, 0, 0, 0, time.UTC), Ratio: ratio(2, 20), Type: "forward_split", Handled: true},
		{ExDate: day(2025, 1, 2), Ratio: ratio(1, 1), Type: "spin_off", Handled: true},
		{ExDate: day(2025, 7, 1), Ratio: ratio(1, 2)},
	}
	gone, added := merge([]gen.CorporateEvent{split, twin, spin}, reported)
	if diff := cmp.Diff([]uuid.UUID{twin.ID, spin.ID}, gone); diff != "" {
		t.Errorf("gone mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(reported[1:], added); diff != "" {
		t.Errorf("added mismatch (-want +got):\n%s", diff)
	}
}
