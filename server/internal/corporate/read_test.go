package corporate

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/types"
)

func TestRemove(t *testing.T) {
	p := types.Period{From: day(2024, 1, 1), Before: day(2024, 2, 1)}
	tests := []struct {
		name string
		days []time.Time
		want []types.Period
	}{
		{name: "a day outside", days: []time.Time{day(2024, 3, 1)}, want: []types.Period{p}},
		{name: "the first day", days: []time.Time{day(2024, 1, 1)}, want: []types.Period{{From: day(2024, 1, 2), Before: day(2024, 2, 1)}}},
		{name: "the last day", days: []time.Time{day(2024, 1, 31)}, want: []types.Period{{From: day(2024, 1, 1), Before: day(2024, 1, 31)}}},
		{name: "two days in the middle, out of order", days: []time.Time{day(2024, 1, 20), day(2024, 1, 10)}, want: []types.Period{
			{From: day(2024, 1, 1), Before: day(2024, 1, 10)}, {From: day(2024, 1, 11), Before: day(2024, 1, 20)}, {From: day(2024, 1, 21), Before: day(2024, 2, 1)},
		}},
		{name: "one day twice", days: []time.Time{day(2024, 1, 10), day(2024, 1, 10)}, want: []types.Period{
			{From: day(2024, 1, 1), Before: day(2024, 1, 10)}, {From: day(2024, 1, 11), Before: day(2024, 2, 1)},
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, remove([]types.Period{p}, tc.days)); diff != "" {
				t.Errorf("remove(%v) mismatch (-want +got):\n%s", tc.days, diff)
			}
		})
	}
}
