//go:build dbtest

package price

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestRecordScales checks that a price quoted in pence is stored in pounds,
// with the day its datasource stated it as at.
func TestRecordScales(t *testing.T) {
	s := newStack(t)
	l := s.listing(t, "GBP")
	adjusted := market.DayPrice{Day: day(2026, 3, 3), Price: decimal.RequireFromString("72.5"), AsAt: day(2026, 3, 9)}
	if !s.record(t, s.answer(t, s.alpha, l, "GBX", span(day(2026, 3, 1), day(2026, 3, 8)), at(day(2026, 3, 2), "7150"), adjusted)) {
		t.Error("Record() = false, want coverage stored")
	}
	want := []stored{
		{Datasource: s.alpha, Day: "2026-03-02", Price: "71.5", AsAt: "2026-03-02"},
		{Datasource: s.alpha, Day: "2026-03-03", Price: "0.725", AsAt: "2026-03-09"},
	}
	if diff := cmp.Diff(want, s.prices(t, l)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}
}

// TestRecordRefuses checks that a refused answer stores neither coverage nor
// a price, so its days stay unknown.
func TestRecordRefuses(t *testing.T) {
	good := at(day(2026, 3, 2), "143.00")
	tests := []struct {
		name     string
		currency string
		prices   []market.DayPrice
		want     error
	}{
		{name: "a code of another family", currency: "USD", prices: []market.DayPrice{good}, want: ErrCurrency},
		{name: "a code the system lacks", currency: "ZZZ", prices: []market.DayPrice{good}, want: ErrCurrency},
		{name: "a price of zero", currency: "GBP", prices: []market.DayPrice{good, at(day(2026, 3, 3), "0")}, want: ErrInvalid},
		{name: "a negative price", currency: "GBP", prices: []market.DayPrice{good, at(day(2026, 3, 3), "-1")}, want: ErrInvalid},
		{
			name: "a price as at a day before its own", currency: "GBP", want: ErrInvalid,
			prices: []market.DayPrice{good, {Day: day(2026, 3, 3), Price: decimal.NewFromInt(1), AsAt: day(2026, 3, 2)}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			l := s.listing(t, "GBP")
			a := s.answer(t, s.alpha, l, tc.currency, span(day(2026, 3, 1), day(2026, 3, 8)), tc.prices...)
			stored, err := Record(context.Background(), s.q, s.families, s.scales, a)
			if !errors.Is(err, tc.want) || stored {
				t.Errorf("Record() = %v, %v, want false and %v", stored, err, tc.want)
			}
			if got := s.coverage(t, s.alpha, l); len(got) != 0 {
				t.Errorf("coverage = %v, want none", got)
			}
			if got := s.prices(t, l); len(got) != 0 {
				t.Errorf("prices = %v, want none", got)
			}
		})
	}
}

// TestRecordCutsCoverage checks that an answer covers no day after the one
// before its moment, however far the datasource says it reaches. A cached
// answer is read days after its moment, and the days since stay unknown.
func TestRecordCutsCoverage(t *testing.T) {
	s := newStack(t)
	l := s.listing(t, "USD")
	a := s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 1), day(2026, 4, 1)), at(day(2026, 3, 9), "10"), at(day(2026, 3, 10), "11"))
	s.record(t, a)
	if diff := cmp.Diff([]types.Period{span(day(2026, 3, 1), day(2026, 3, 10))}, s.coverage(t, s.alpha, l)); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]stored{{Datasource: s.alpha, Day: "2026-03-09", Price: "10", AsAt: "2026-03-09"}}, s.prices(t, l)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}
}

// TestRecordWithoutCoverage checks that Record stores nothing and returns no
// error for an answer without coverage, and for one whose coverage is empty
// after the cut.
func TestRecordWithoutCoverage(t *testing.T) {
	s := newStack(t)
	l := s.listing(t, "USD")
	unknown := Answer{Listing: l, Datasource: s.alpha, FetchKeyID: s.key(t, s.alpha, answered), AnsweredAt: answered}
	future := s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 10), day(2026, 4, 1)))
	for name, a := range map[string]Answer{"unknown identifier": unknown, "only days not yet completed": future} {
		if s.record(t, a) {
			t.Errorf("%s: Record() = true, want nothing stored", name)
		}
	}
	if got := s.coverage(t, s.alpha, l); len(got) != 0 {
		t.Errorf("coverage = %v, want none", got)
	}
}

// TestRecordFirstAnswerStands checks that a later answer of a datasource
// adds only the days it has not covered, and that another datasource covers
// the same days beside it.
func TestRecordFirstAnswerStands(t *testing.T) {
	s := newStack(t)
	l := s.listing(t, "USD")
	s.record(t, s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 2), day(2026, 3, 5)), at(day(2026, 3, 3), "10")))
	if s.record(t, s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 2), day(2026, 3, 5)), at(day(2026, 3, 3), "99"), at(day(2026, 3, 4), "99"))) {
		t.Error("Record() = true for a covered period, want nothing stored")
	}
	later := time.Date(2026, time.March, 20, 0, 0, 0, 0, time.UTC)
	wide := s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 1), day(2026, 3, 7)), at(day(2026, 3, 1), "8"), at(day(2026, 3, 3), "99"), at(day(2026, 3, 6), "12"))
	wide.AnsweredAt = later
	if !s.record(t, wide) {
		t.Error("Record() = false for a period covered in part, want the new days stored")
	}
	s.record(t, s.answer(t, s.beta, l, "USD", span(day(2026, 3, 2), day(2026, 3, 5)), at(day(2026, 3, 3), "10.5")))

	wantCoverage := []types.Period{span(day(2026, 3, 1), day(2026, 3, 2)), span(day(2026, 3, 2), day(2026, 3, 5)), span(day(2026, 3, 5), day(2026, 3, 7))}
	if diff := cmp.Diff(wantCoverage, s.coverage(t, s.alpha, l)); diff != "" {
		t.Errorf("alpha coverage mismatch (-want +got):\n%s", diff)
	}
	want := []stored{
		{Datasource: s.alpha, Day: "2026-03-01", Price: "8", AsAt: "2026-03-01"},
		{Datasource: s.alpha, Day: "2026-03-03", Price: "10", AsAt: "2026-03-03"},
		{Datasource: s.alpha, Day: "2026-03-06", Price: "12", AsAt: "2026-03-06"},
		{Datasource: s.beta, Day: "2026-03-03", Price: "10.5", AsAt: "2026-03-03"},
	}
	if diff := cmp.Diff(want, s.prices(t, l)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}
}

// TestMove checks that a move takes the days the destination lacks for the
// same datasource, leaves the days it has, and splits a period that the
// destination covers in part.
func TestMove(t *testing.T) {
	s := newStack(t)
	from, to := s.listing(t, "USD"), s.listing(t, "USD")
	s.record(t, s.answer(t, s.alpha, to, "USD", span(day(2026, 3, 3), day(2026, 3, 5)), at(day(2026, 3, 3), "50")))
	s.record(t, s.answer(t, s.alpha, from, "USD", span(day(2026, 3, 2), day(2026, 3, 7)), at(day(2026, 3, 2), "10"), at(day(2026, 3, 3), "11"), at(day(2026, 3, 6), "12")))
	s.record(t, s.answer(t, s.beta, from, "USD", span(day(2026, 3, 2), day(2026, 3, 7)), at(day(2026, 3, 3), "11.5")))
	require.NoError(t, Move(context.Background(), s.q, from, to))

	want := []stored{
		{Datasource: s.alpha, Day: "2026-03-02", Price: "10", AsAt: "2026-03-02"},
		{Datasource: s.alpha, Day: "2026-03-03", Price: "50", AsAt: "2026-03-03"},
		{Datasource: s.alpha, Day: "2026-03-06", Price: "12", AsAt: "2026-03-06"},
		{Datasource: s.beta, Day: "2026-03-03", Price: "11.5", AsAt: "2026-03-03"},
	}
	if diff := cmp.Diff(want, s.prices(t, to)); diff != "" {
		t.Errorf("prices of the destination mismatch (-want +got):\n%s", diff)
	}
	wantCoverage := []types.Period{span(day(2026, 3, 2), day(2026, 3, 3)), span(day(2026, 3, 3), day(2026, 3, 5)), span(day(2026, 3, 5), day(2026, 3, 7))}
	if diff := cmp.Diff(wantCoverage, s.coverage(t, s.alpha, to)); diff != "" {
		t.Errorf("alpha coverage of the destination mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]types.Period{span(day(2026, 3, 2), day(2026, 3, 7))}, s.coverage(t, s.beta, to)); diff != "" {
		t.Errorf("beta coverage of the destination mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]stored{{Datasource: s.alpha, Day: "2026-03-03", Price: "11", AsAt: "2026-03-03"}}, s.prices(t, from)); diff != "" {
		t.Errorf("prices left on the source mismatch (-want +got):\n%s", diff)
	}
}
