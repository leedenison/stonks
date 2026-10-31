package corpact

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

func TestRestates(t *testing.T) {
	tests := []struct {
		d    gen.CorpActDecision
		want bool
	}{
		{gen.CorpActDecisionSplit, true},
		{gen.CorpActDecisionReverseSplit, true},
		{gen.CorpActDecisionStockDividend, true},
		{gen.CorpActDecisionSpinOffPriceFactor, false},
		{gen.CorpActDecisionRightsIssue, false},
		{gen.CorpActDecisionOther, false},
		{"", false},
	}
	for _, tc := range tests {
		t.Run(string(tc.d), func(t *testing.T) {
			if got := Restates(tc.d); got != tc.want {
				t.Errorf("Restates(%q) = %v, want %v", tc.d, got, tc.want)
			}
		})
	}
}

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

// TestReadEvidence checks what the evidence for an action holds: the other
// datasources' actions within the window, the distinct stated splits within
// it, and whether the calendar applies the action.
func TestReadEvidence(t *testing.T) {
	instr := uuid.New()
	spun := day(2024, 4, 2)
	var s canned
	s.names("US", "GE", instr, nil, nil)
	s.sources = map[string]string{"US": "alpha"}
	alpha := s.answer("alpha", "US", "GE", day(2012, 1, 1), today)
	beta := s.answer("beta", "US", "GE", day(2012, 1, 1), today)
	subject := s.act(alpha, spun, 1000, 1253, false)
	s.act(alpha, spun.AddDate(0, 0, 3), 1, 2, true)
	edge := s.act(beta, spun.AddDate(0, 0, window), 1, 2, true)
	s.act(beta, spun.AddDate(0, 0, window+1), 1, 3, true)
	s.states(instr, spun.AddDate(0, 0, -1), 1, 2)
	s.states(instr, spun.AddDate(0, 0, -1), 1, 2)
	s.states(instr, spun.AddDate(0, 0, 1), 0, 0)
	s.states(instr, spun.AddDate(0, 0, window+1), 1, 2)

	got, err := ReadEvidence(context.Background(), &s, subject)
	require.NoError(t, err)
	ge := types.MarketTicker{Market: "US", Value: "GE"}
	want := Evidence{
		Act: InstrAct{Act: Act{ID: subject, ExDate: spun, Ratio: ratio(1000, 1253)}, Datasource: "alpha", Ticker: ge, Applied: true},
		StmtSplits: []StmtSplit{
			{EffectiveDate: spun.AddDate(0, 0, -1), Ratio: &types.Ratio{From: decimal.NewFromInt(1), To: decimal.NewFromInt(2)}},
			{EffectiveDate: spun.AddDate(0, 0, 1)},
		},
		Others: []InstrAct{{Act: Act{ID: edge, ExDate: spun.AddDate(0, 0, window), Ratio: ratio(1, 2), Handled: true}, Datasource: "beta", Ticker: ge}},
	}
	if diff := cmp.Diff(want, got, cmpopts.EquateComparable(decimal.Decimal{})); diff != "" {
		t.Errorf("evidence mismatch (-want +got):\n%s", diff)
	}
}

// TestReadEvidenceUnnamed checks that an action of a ticker that named
// nothing on its ex-date comes back alone, and that a missing action is not
// found.
func TestReadEvidenceUnnamed(t *testing.T) {
	var s canned
	alpha := s.answer("alpha", "US", "GE", day(2012, 1, 1), today)
	subject := s.act(alpha, day(2024, 4, 2), 1000, 1253, false)
	got, err := ReadEvidence(context.Background(), &s, subject)
	require.NoError(t, err)
	want := Evidence{Act: InstrAct{Act: Act{ID: subject, ExDate: day(2024, 4, 2), Ratio: ratio(1000, 1253)}, Datasource: "alpha", Ticker: types.MarketTicker{Market: "US", Value: "GE"}}}
	if diff := cmp.Diff(want, got, cmpopts.EquateComparable(decimal.Decimal{})); diff != "" {
		t.Errorf("evidence mismatch (-want +got):\n%s", diff)
	}
	if _, err := ReadEvidence(context.Background(), &s, uuid.New()); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("ReadEvidence of no action: err = %v, want ErrNotFound", err)
	}
}
