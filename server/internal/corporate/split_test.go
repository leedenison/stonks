package corporate

import (
	"testing"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/types"
)

func TestUnmatchedSplits(t *testing.T) {
	june := day(2024, 6, 7)
	inst := uuid.UUID{15: 1}
	stated := func(rs ...types.Ratio) splitGroup {
		return splitGroup{key: uuid.UUID{15: 2}, day: june, instrument: inst, ratios: rs}
	}
	tests := []struct {
		name   string
		group  splitGroup
		events []Event
		// want is the ratio the events lack, "none" for a group without a
		// stated ratio, or empty when the group matches.
		want string
	}{
		{name: "same day and ratio", group: stated(ratio(1, 10)), events: []Event{event(1, june, ratio(1, 10))}},
		{name: "3 days after, as a broker dates it on the last trading day before", group: stated(ratio(1, 10)), events: []Event{event(1, june.AddDate(0, 0, 3), ratio(1, 10))}},
		{name: "7 days before", group: stated(ratio(1, 10)), events: []Event{event(1, june.AddDate(0, 0, -7), ratio(1, 10))}},
		{name: "7 days after", group: stated(ratio(1, 10)), events: []Event{event(1, june.AddDate(0, 0, 7), ratio(1, 10))}},
		{name: "8 days apart", group: stated(ratio(1, 10)), events: []Event{event(1, june.AddDate(0, 0, 8), ratio(1, 10))}, want: "10 for 1"},
		{name: "another ratio", group: stated(ratio(1, 10)), events: []Event{event(1, june, ratio(1, 2))}, want: "10 for 1"},
		{name: "no events", group: stated(ratio(1, 10)), want: "10 for 1"},
		{name: "a ratio in other terms", group: stated(ratio(1, 4)), events: []Event{event(1, june, ratio(2, 8))}},
		{name: "no stated ratio matches any ratio", group: stated(), events: []Event{event(1, june, ratio(1, 3))}},
		{name: "no stated ratio and no event", group: stated(), events: []Event{event(1, june.AddDate(0, 0, 8), ratio(1, 3))}, want: "none"},
		{name: "each stated ratio needs an event", group: stated(ratio(1, 4), ratio(1, 3)), events: []Event{event(1, june, ratio(1, 3))}, want: "4 for 1"},
		{name: "an unhandled event matches", group: stated(ratio(1, 10)), events: []Event{{ID: uuid.UUID{15: 1}, ExDate: june, Ratio: ratio(1, 10)}}},
		{name: "another instrument's event", group: splitGroup{key: uuid.UUID{15: 2}, day: june, instrument: uuid.UUID{15: 9}, ratios: []types.Ratio{ratio(1, 10)}}, events: []Event{event(1, june, ratio(1, 10))}, want: "10 for 1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := unmatchedSplits([]splitGroup{tc.group}, map[uuid.UUID][]Event{inst: tc.events})
			var lacks string
			switch {
			case len(got) > 1:
				t.Fatalf("unmatchedSplits = %d groups, want at most 1", len(got))
			case len(got) == 1 && got[0].ratio == nil:
				lacks = "none"
			case len(got) == 1:
				lacks = ratioText(*got[0].ratio)
			}
			if lacks != tc.want {
				t.Errorf("unmatchedSplits lacks %q, want %q", lacks, tc.want)
			}
		})
	}
}
