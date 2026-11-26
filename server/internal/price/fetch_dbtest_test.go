//go:build dbtest

package price

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// fund creates a fund that h holds from Monday 2 March 2026, with one listing
// in currency that a current ticker reaches at XNAS. It returns the listing
// and the identifier a fetch sends for it.
func (s *stack) fund(t *testing.T, h holder, currency string) (uuid.UUID, types.Identifier) {
	t.Helper()
	instr, listings := s.instr(t, types.AssetClassMutualFund, currency)
	value := s.ticker(t, instr, currency)
	l := listings[currency]
	s.txn(t, h, s.fixedKey(t, h, instr, &l), day(2026, 3, 2), "10")
	return l, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: value}
}

// fetch asks the datasource called source for every gap and returns how many
// answers stored coverage.
func (s *stack) fetch(t *testing.T, source string) int {
	t.Helper()
	ctx := context.Background()
	gaps, _ := s.gapsOf(t, nil)
	all := make([]Gap, 0, len(gaps))
	for _, g := range gaps {
		all = append(all, g)
	}
	n, err := s.recorder.Fetch(ctx, s.parent, s.sources.Entry(source), all)
	require.NoError(t, err)
	return n
}

// history is a provider's whole history of one identifier in currency, over
// p.
func history(currency string, p types.Period, prices ...market.DayPrice) market.PriceResult {
	return market.PriceResult{Prices: prices, Currency: currency, Coverage: &p}
}

// demanded is the gap of a fund before any fetch.
var demanded = []types.Period{span(day(2026, 2, 23), today)}

// TestFetchKeepsEachKeyApart checks that one pass stores the answer it can
// and leaves the days of a refused answer as gaps: one in a currency outside
// the family of its listing, and one with a price of zero.
func TestFetchKeepsEachKeyApart(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	good, goodID := s.fund(t, h, "USD")
	foreign, foreignID := s.fund(t, h, "GBP")
	zero, zeroID := s.fund(t, h, "USD")
	year := span(day(2026, 1, 1), day(2027, 1, 1))
	s.alphaPrices.Responses[goodID] = history("USD", year, at(day(2026, 3, 2), "10"), at(day(2026, 3, 9), "11"), at(day(2026, 3, 10), "12"))
	s.alphaPrices.Responses[foreignID] = history("USD", year, at(day(2026, 3, 2), "10"))
	s.alphaPrices.Responses[zeroID] = history("USD", year, at(day(2026, 3, 2), "0"))

	if n := s.fetch(t, s.alpha); n != 1 {
		t.Errorf("Fetch() = %d, want 1 answer stored", n)
	}
	want := []stored{{Datasource: s.alpha, Day: "2026-03-02", Price: "10", AsAt: "2026-03-02"}, {Datasource: s.alpha, Day: "2026-03-09", Price: "11", AsAt: "2026-03-09"}}
	if diff := cmp.Diff(want, s.prices(t, good)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}
	gaps, _ := s.gapsOf(t, nil)
	if _, ok := gaps[good]; ok {
		t.Errorf("gap of the stored listing = %v, want none", gaps[good].Periods)
	}
	for name, l := range map[string]uuid.UUID{"foreign currency": foreign, "zero price": zero} {
		if diff := cmp.Diff(demanded, gaps[l].Periods); diff != "" {
			t.Errorf("%s: gap mismatch (-want +got):\n%s", name, diff)
		}
	}
}

// TestFetchPassesGapsDown checks that the days one datasource leaves are
// asked of the next: the days before its window, the whole of a request it
// fails, and the whole of a request it declines.
func TestFetchPassesGapsDown(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	windowed, windowedID := s.fund(t, h, "USD")
	declined, declinedID := s.fund(t, h, "USD")
	s.alphaPrices.Responses[windowedID] = history("USD", span(day(2026, 3, 1), day(2027, 1, 1)), at(day(2026, 3, 2), "10"))
	s.alphaPrices.Declines = []string{"XNAS"}
	if n := s.fetch(t, s.alpha); n != 0 {
		t.Errorf("Fetch() = %d from a datasource that declines every request, want 0", n)
	}
	s.alphaPrices.Declines = nil
	s.alphaPrices.Down = true
	if n := s.fetch(t, s.alpha); n != 0 {
		t.Errorf("Fetch() = %d from a datasource that is down, want 0", n)
	}
	gaps, _ := s.gapsOf(t, nil)
	if diff := cmp.Diff(demanded, gaps[windowed].Periods); diff != "" {
		t.Errorf("gap after two failed passes mismatch (-want +got):\n%s", diff)
	}

	s.alphaPrices.Down = false
	if n := s.fetch(t, s.alpha); n != 1 {
		t.Errorf("Fetch() = %d, want the one listing alpha knows stored", n)
	}
	gaps, _ = s.gapsOf(t, nil)
	if diff := cmp.Diff([]types.Period{span(day(2026, 2, 23), day(2026, 3, 1))}, gaps[windowed].Periods); diff != "" {
		t.Errorf("gap after alpha mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(demanded, gaps[declined].Periods); diff != "" {
		t.Errorf("gap of the listing alpha does not know mismatch (-want +got):\n%s", diff)
	}

	year := span(day(2026, 1, 1), day(2027, 1, 1))
	s.betaPrices.Responses[windowedID] = history("USD", year, at(day(2026, 2, 27), "9"), at(day(2026, 3, 2), "10.5"))
	s.betaPrices.Responses[declinedID] = history("USD", year, at(day(2026, 3, 2), "20"))
	if n := s.fetch(t, s.beta); n != 2 {
		t.Errorf("Fetch() = %d from beta, want 2", n)
	}
	if gaps, _ := s.gapsOf(t, nil); len(gaps[windowed].Periods) != 0 || len(gaps[declined].Periods) != 0 {
		t.Errorf("gaps after beta = %v and %v, want none", gaps[windowed].Periods, gaps[declined].Periods)
	}
	want := []stored{
		{Datasource: s.alpha, Day: "2026-03-02", Price: "10", AsAt: "2026-03-02"},
		{Datasource: s.beta, Day: "2026-02-27", Price: "9", AsAt: "2026-02-27"},
		{Datasource: s.beta, Day: "2026-03-02", Price: "10.5", AsAt: "2026-03-02"},
	}
	if diff := cmp.Diff(want, s.prices(t, windowed)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}
}

// TestFetchSpansGaps checks that a listing with two gaps is asked once, for
// the days from the first gap to the end of the last, and that the whole
// answer is stored, including the days outside both gaps.
func TestFetchSpansGaps(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	l, id := s.fund(t, h, "USD")
	middle := span(day(2026, 3, 2), day(2026, 3, 5))
	s.record(t, s.answer(t, s.alpha, l, "USD", middle, at(day(2026, 3, 2), "10")))
	gaps, _ := s.gapsOf(t, nil)
	if diff := cmp.Diff([]types.Period{span(day(2026, 2, 23), day(2026, 3, 2)), span(day(2026, 3, 5), today)}, gaps[l].Periods); diff != "" {
		t.Fatalf("gaps before the fetch mismatch (-want +got):\n%s", diff)
	}

	s.betaPrices.Responses[id] = history("USD", span(day(2025, 1, 1), day(2027, 1, 1)), at(day(2025, 6, 2), "5"), at(day(2026, 2, 27), "9"), at(day(2026, 3, 3), "10.5"), at(day(2026, 3, 9), "11"))
	if n := s.fetch(t, s.beta); n != 1 {
		t.Errorf("Fetch() = %d, want 1", n)
	}
	if diff := cmp.Diff([]market.PriceRequest{{Identifier: id, Period: span(day(2026, 2, 23), today)}}, s.betaPrices.Sent); diff != "" {
		t.Errorf("requests mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]types.Period{span(day(2025, 1, 1), today)}, s.coverage(t, s.beta, l)); diff != "" {
		t.Errorf("coverage of beta mismatch (-want +got):\n%s", diff)
	}
	want := []stored{
		{Datasource: s.alpha, Day: "2026-03-02", Price: "10", AsAt: "2026-03-02"},
		{Datasource: s.beta, Day: "2025-06-02", Price: "5", AsAt: "2025-06-02"},
		{Datasource: s.beta, Day: "2026-02-27", Price: "9", AsAt: "2026-02-27"},
		{Datasource: s.beta, Day: "2026-03-03", Price: "10.5", AsAt: "2026-03-03"},
		{Datasource: s.beta, Day: "2026-03-09", Price: "11", AsAt: "2026-03-09"},
	}
	if diff := cmp.Diff(want, s.prices(t, l)); diff != "" {
		t.Errorf("prices mismatch (-want +got):\n%s", diff)
	}
}

// TestFetchWithoutCapability checks that Fetch skips a datasource without the
// price capability and leaves the fetches table empty for it.
func TestFetchWithoutCapability(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	s.fund(t, h, "USD")
	if n := s.fetch(t, s.plain); n != 0 {
		t.Errorf("Fetch() = %d, want 0", n)
	}
	var fetches int
	require.NoError(t, s.tx.QueryRow(context.Background(), "SELECT count(*) FROM fetches WHERE datasource = $1", s.plain).Scan(&fetches))
	if fetches != 0 {
		t.Errorf("%d fetches recorded for a datasource without prices, want none", fetches)
	}
}
