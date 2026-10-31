package corpact

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// TestActEffect checks that a decision overrules the system's judgment in
// either direction, and which actions are unknown.
func TestActEffect(t *testing.T) {
	tests := []struct {
		name     string
		act      Act
		restates bool
		unknown  bool
	}{
		{name: "handled and undecided", act: Act{Handled: true}, restates: true},
		{name: "unhandled and undecided", act: Act{}, unknown: true},
		{name: "handled, decided a price factor", act: Act{Handled: true, Decision: gen.CorpActDecisionSpinOffPriceFactor}},
		{name: "unhandled, decided a split", act: Act{Decision: gen.CorpActDecisionSplit}, restates: true},
		{name: "unhandled, decided other", act: Act{Decision: gen.CorpActDecisionOther}, unknown: true},
		{name: "handled, decided other", act: Act{Handled: true, Decision: gen.CorpActDecisionOther}, unknown: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.act.Restates(); got != tc.restates {
				t.Errorf("Restates() = %v, want %v", got, tc.restates)
			}
			if got := tc.act.Unknown(); got != tc.unknown {
				t.Errorf("Unknown() = %v, want %v", got, tc.unknown)
			}
		})
	}
}
