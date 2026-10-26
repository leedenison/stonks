package corporate

import (
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// event returns a handled event with an id that orders by n.
func event(n byte, ex time.Time, r types.Ratio) Event {
	return Event{ID: uuid.UUID{15: n}, ExDate: ex, Ratio: r, Handled: true}
}

func ids(es []Event) []byte {
	var out []byte
	for _, e := range es {
		out = append(out, e.ID[15])
	}
	return out
}

func TestUnmatched(t *testing.T) {
	cov := []types.Period{{From: day(2020, 1, 1), Before: day(2026, 1, 1)}}
	june := day(2024, 6, 10)
	tests := []struct {
		name         string
		a, b         []Event
		bCov         []types.Period
		wantA, wantB []byte
	}{
		{name: "7 days before", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june.AddDate(0, 0, -7), ratio(1, 2))}},
		{name: "7 days after", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june.AddDate(0, 0, 7), ratio(1, 2))}},
		{name: "8 days before", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june.AddDate(0, 0, -8), ratio(1, 2))}, wantA: []byte{1}, wantB: []byte{2}},
		{name: "8 days after", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june.AddDate(0, 0, 8), ratio(1, 2))}, wantA: []byte{1}, wantB: []byte{2}},
		{name: "a ratio in other terms", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june, ratio(10, 20))}},
		{name: "another ratio", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june, ratio(1, 3))}, wantA: []byte{1}, wantB: []byte{2}},
		{name: "an event outside the other's coverage", a: []Event{event(1, june, ratio(1, 2))}, bCov: []types.Period{{From: day(2025, 1, 1), Before: day(2026, 1, 1)}}},
		{name: "a split reported twice", a: []Event{event(1, june, ratio(1, 2)), event(2, june, ratio(1, 2))}, b: []Event{event(3, june, ratio(1, 2))}, wantA: []byte{2}},
		{name: "a tie leaves the later unmatched", a: []Event{event(1, june, ratio(1, 2))}, b: []Event{event(2, june.AddDate(0, 0, -3), ratio(1, 2)), event(3, june.AddDate(0, 0, 3), ratio(1, 2))}, wantB: []byte{3}},
		{name: "an unhandled event takes part", a: []Event{{ID: uuid.UUID{15: 1}, ExDate: june, Ratio: ratio(1, 1)}}, wantA: []byte{1}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			bCov := cov
			if tc.bCov != nil {
				bCov = tc.bCov
			}
			ua, ub := unmatched(Answer{Events: tc.a, Coverage: cov}, Answer{Events: tc.b, Coverage: bCov})
			if diff := cmp.Diff([][]byte{tc.wantA, tc.wantB}, [][]byte{ids(ua), ids(ub)}); diff != "" {
				t.Errorf("unmatched mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDisagreements checks which pairs of answers are compared, which run
// receives each finding, and that an event is reported once.
func TestDisagreements(t *testing.T) {
	cov := []types.Period{{From: day(2020, 1, 1), Before: day(2026, 1, 1)}}
	ticker := types.MarketTicker{Market: "US", Value: "NVDA"}
	alpha := storedAnswer{Answer: Answer{Datasource: "alpha", Coverage: cov, Events: []Event{event(1, day(2024, 6, 10), ratio(1, 2))}}, fetchKey: uuid.New()}
	beta := storedAnswer{Answer: Answer{Datasource: "beta", Coverage: cov}, fetchKey: uuid.New()}
	gamma := storedAnswer{Answer: Answer{Datasource: "gamma", Coverage: cov}, fetchKey: uuid.New()}
	runA, runB := gen.Run{ID: uuid.New()}, gen.Run{ID: uuid.New()}
	tests := []struct {
		name string
		mine map[uuid.UUID]gen.Run
		want []uuid.UUID
	}{
		{name: "the event's own answer is mine", mine: map[uuid.UUID]gen.Run{alpha.fetchKey: runA, beta.fetchKey: runB}, want: []uuid.UUID{runA.ID}},
		{name: "only the other answer is mine", mine: map[uuid.UUID]gen.Run{beta.fetchKey: runB}, want: []uuid.UUID{runB.ID}},
		{name: "neither answer is mine"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fs := disagreements(ticker, []storedAnswer{alpha, beta, gamma}, tc.mine)
			var got []uuid.UUID
			for _, f := range fs {
				got = append(got, f.RunID)
				if f.Kind != gen.FindingKindCorporateEventDisagreement || *f.CorporateEventID != alpha.Events[0].ID {
					t.Errorf("finding = %+v, want a disagreement naming alpha's event", f)
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("finding runs mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
