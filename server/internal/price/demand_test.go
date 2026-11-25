package price

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/types"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func on(d time.Time, quantity string) txnDay {
	return txnDay{asAt: d, quantity: decimal.RequireFromString(quantity)}
}

// TestHeld checks the days on which a holding is open. today is a Tuesday,
// so the last completed day is Monday 9 March 2026.
func TestHeld(t *testing.T) {
	today := day(2026, 3, 10)
	split := corpact.Act{ExDate: day(2024, 6, 10), Ratio: types.Ratio{From: decimal.NewFromInt(1), To: decimal.NewFromInt(2)}, Handled: true}
	thirds := corpact.Act{ExDate: day(2024, 6, 10), Ratio: types.Ratio{From: decimal.NewFromInt(2), To: decimal.NewFromInt(3)}, Handled: true}
	covering := func(acts ...corpact.Act) corpact.Calendar {
		return corpact.Calendar{Answer: corpact.Answer{Acts: acts, Coverage: []types.Period{{From: day(2020, 1, 1), Before: day(2025, 1, 1)}}}}
	}
	period := func(from, before time.Time) []types.Period { return []types.Period{{From: from, Before: before}} }
	tests := []struct {
		name     string
		cal      corpact.Calendar
		restated bool
		days     []txnDay
		want     []types.Period
	}{
		{
			name: "an open holding runs from a week before its first day to the last completed day",
			days: []txnDay{on(day(2026, 3, 7), "10")},
			want: period(day(2026, 2, 28), today),
		},
		{
			name: "a closed holding ends on its last day",
			days: []txnDay{on(day(2024, 6, 3), "10"), on(day(2024, 6, 12), "-4"), on(day(2024, 6, 14), "-6")},
			want: period(day(2024, 5, 27), day(2024, 6, 15)),
		},
		{
			name: "a holding sold and bought again is open twice",
			days: []txnDay{on(day(2024, 6, 3), "10"), on(day(2024, 6, 5), "-10"), on(day(2025, 2, 3), "1")},
			want: []types.Period{{From: day(2024, 5, 27), Before: day(2024, 6, 6)}, {From: day(2025, 1, 27), Before: today}},
		},
		{
			name: "a day whose transactions cancel opens nothing",
			days: []txnDay{on(day(2024, 6, 3), "10"), on(day(2024, 6, 3), "-10")},
		},
		{
			name: "a holding that begins today is demanded for its lead alone",
			days: []txnDay{on(today, "10")},
			want: period(day(2026, 3, 3), today),
		},
		{
			name: "bought before a split and half sold after it stays open", cal: covering(split), restated: true,
			days: []txnDay{on(day(2024, 6, 3), "100"), on(day(2024, 6, 12), "-100")},
			want: period(day(2024, 5, 27), today),
		},
		{
			name: "bought before a split and sold in full after it closes", cal: covering(split), restated: true,
			days: []txnDay{on(day(2024, 6, 3), "100"), on(day(2024, 6, 12), "-200")},
			want: period(day(2024, 5, 27), day(2024, 6, 13)),
		},
		{
			name: "the stated sum decides for a class that is not restated", cal: covering(split),
			days: []txnDay{on(day(2024, 6, 3), "100"), on(day(2024, 6, 12), "-100")},
			want: period(day(2024, 5, 27), day(2024, 6, 13)),
		},
		{
			name: "a fraction sold for cash cancels", cal: covering(thirds), restated: true,
			days: []txnDay{on(day(2024, 6, 3), "3"), on(day(2024, 6, 10), "-0.5"), on(day(2024, 6, 12), "-4")},
			want: period(day(2024, 5, 27), day(2024, 6, 13)),
		},
		{
			name: "a calendar that stops between two transactions leaves the holding open", cal: covering(split), restated: true,
			days: []txnDay{on(day(2024, 6, 3), "100"), on(day(2025, 6, 12), "-200")},
			want: period(day(2024, 5, 27), today),
		},
		{
			name: "one transaction needs no calendar", restated: true,
			days: []txnDay{on(day(2025, 6, 12), "5")},
			want: period(day(2025, 6, 5), today),
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, held(tc.cal, tc.restated, tc.days, today)); diff != "" {
				t.Errorf("held() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
