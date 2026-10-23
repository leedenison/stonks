package massive

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/types"
)

func change(ticker, date string) event {
	e := event{Type: "ticker_change", Date: date}
	e.TickerChange.Ticker = ticker
	return e
}

func period(from, before time.Time) types.Period { return types.Period{From: from, Before: before} }

// TestChainFacts checks the identifier facts of an entity's chain at one
// venue.
func TestChainFacts(t *testing.T) {
	today := day(2026, 10, 22)
	p := period(since, today.AddDate(0, 0, 1))
	asserted := func(value string, on time.Time) market.Assertion {
		return market.Assertion{Value: ticker("XNAS", value), Of: market.Listing{Identifiers: []types.Identifier{meta}}, At: on}
	}
	covered := func(value string, from, before time.Time) market.Coverage {
		return market.Coverage{Identifier: ticker("XNAS", value), Period: period(from, before)}
	}
	tests := []struct {
		name string
		evs  []event
		p    types.Period
		want market.IdentifierEventResult
	}{
		{
			name: "a chain of three tickers",
			evs:  []event{change("CCC", "2020-06-01"), change("BBB", "2015-03-02"), change("AAA", "2010-01-04")},
			p:    p,
			want: market.IdentifierEventResult{
				Events: []market.IdentifierEvent{
					{Value: ticker("XNAS", "AAA"), Action: gen.IdentifierEventActionAssigned, On: day(2010, 1, 4)},
					{Value: ticker("XNAS", "AAA"), Action: gen.IdentifierEventActionRenamed, ToValue: "BBB", On: day(2015, 3, 2)},
					{Value: ticker("XNAS", "BBB"), Action: gen.IdentifierEventActionRenamed, ToValue: "CCC", On: day(2020, 6, 1)},
				},
				Assertions: []market.Assertion{
					asserted("AAA", day(2010, 1, 4)), asserted("BBB", day(2015, 3, 2)),
					asserted("CCC", day(2020, 6, 1)), asserted("CCC", today),
				},
				Coverage: []market.Coverage{
					covered("AAA", day(2010, 1, 4), day(2015, 3, 2)),
					covered("BBB", day(2015, 3, 2), day(2020, 6, 1)),
					covered("CCC", day(2020, 6, 1), p.Before),
				},
			},
		},
		{
			name: "a ticker already trading when Massive's data starts",
			evs:  []event{change("AAPL", "2003-09-10")},
			p:    p,
			want: market.IdentifierEventResult{
				Assertions: []market.Assertion{asserted("AAPL", since), asserted("AAPL", today)},
				Coverage:   []market.Coverage{covered("AAPL", since, p.Before)},
			},
		},
		{
			name: "a chain trimmed to the period",
			evs:  []event{change("BBB", "2025-03-03"), change("AAA", "2024-10-22")},
			p:    period(day(2024, 10, 22), p.Before),
			want: market.IdentifierEventResult{
				Events: []market.IdentifierEvent{
					{Value: ticker("XNAS", "AAA"), Action: gen.IdentifierEventActionRenamed, ToValue: "BBB", On: day(2025, 3, 3)},
				},
				Assertions: []market.Assertion{asserted("AAA", day(2024, 10, 22)), asserted("BBB", day(2025, 3, 3)), asserted("BBB", today)},
				Coverage: []market.Coverage{
					covered("AAA", day(2024, 10, 22), day(2025, 3, 3)),
					covered("BBB", day(2025, 3, 3), p.Before),
				},
			},
		},
		{
			name: "coverage clipped to the period",
			evs:  []event{change("BBB", "2025-03-03"), change("AAA", "2010-01-04")},
			p:    period(day(2024, 10, 22), day(2025, 6, 2)),
			want: market.IdentifierEventResult{
				Events: []market.IdentifierEvent{
					{Value: ticker("XNAS", "AAA"), Action: gen.IdentifierEventActionRenamed, ToValue: "BBB", On: day(2025, 3, 3)},
				},
				Assertions: []market.Assertion{asserted("AAA", day(2010, 1, 4)), asserted("BBB", day(2025, 3, 3)), asserted("BBB", day(2025, 6, 1))},
				Coverage: []market.Coverage{
					covered("AAA", day(2024, 10, 22), day(2025, 3, 3)),
					covered("BBB", day(2025, 3, 3), day(2025, 6, 2)),
				},
			},
		},
		{
			name: "a newest ticker that began today",
			evs:  []event{change("AAA", "2026-10-22")},
			p:    p,
			want: market.IdentifierEventResult{
				Events:     []market.IdentifierEvent{{Value: ticker("XNAS", "AAA"), Action: gen.IdentifierEventActionAssigned, On: today}},
				Assertions: []market.Assertion{asserted("AAA", today)},
				Coverage:   []market.Coverage{covered("AAA", today, p.Before)},
			},
		},
		{
			name: "a chain holding a preferred share",
			evs:  []event{change("ABRpD", "2020-06-01"), change("AAA", "2010-01-04")},
			p:    p,
		},
		{
			name: "a chain holding a ticker with two separators",
			evs:  []event{change("A.B.C", "2020-06-01"), change("AAA", "2010-01-04")},
			p:    p,
		},
		{
			name: "a class share in another spelling",
			evs:  []event{change("BRK/B", "2003-09-10")},
			p:    p,
			want: market.IdentifierEventResult{
				Assertions: []market.Assertion{asserted("BRK.B", since), asserted("BRK.B", today)},
				Coverage:   []market.Coverage{covered("BRK.B", since, p.Before)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chainFacts(meta, tc.evs, tc.p, []string{"XNAS"})
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("chainFacts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestChainFactsVenues checks that each fact is stated at every venue.
func TestChainFactsVenues(t *testing.T) {
	got, err := chainFacts(meta, []event{change("META", "2022-06-09"), change("FB", "2012-05-18")}, history, []string{"XNAS", "XNYS"})
	require.NoError(t, err)
	if len(got.Events) != 4 || len(got.Assertions) != 6 || len(got.Coverage) != 4 {
		t.Errorf("chainFacts = %d events, %d assertions, %d coverage, want 4, 6 and 4", len(got.Events), len(got.Assertions), len(got.Coverage))
	}
	for _, e := range got.Events {
		if e.Value.Domain != "XNAS" && e.Value.Domain != "XNYS" {
			t.Errorf("event at %s, want XNAS or XNYS", e.Value.Domain)
		}
	}
}

// TestChainFactsDate checks that an event date in a form other than
// YYYY-MM-DD fails the conversion.
func TestChainFactsDate(t *testing.T) {
	if _, err := chainFacts(meta, []event{change("META", "June 9, 2022")}, history, []string{"XNAS"}); err == nil {
		t.Error("chainFacts() succeeded, want an error")
	}
}

// TestLookupFacts checks the assertion a point in time lookup makes.
func TestLookupFacts(t *testing.T) {
	on := day(2021, 6, 1)
	fb := &record{Ticker: "FB", Market: "stocks", PrimaryExchange: "XNAS", Type: "CS", CurrencyName: "usd", CompositeFIGI: "BBG000MM2P62", ShareClassFIGI: "BBG001SQCQC5"}
	tests := []struct {
		name    string
		rec     *record
		unknown bool
		want    market.IdentifierEventResult
	}{
		{
			name: "a record of the day",
			rec:  fb,
			want: market.IdentifierEventResult{Assertions: []market.Assertion{{
				Value: ticker("XNAS", "FB"), At: on, Of: market.Listing{
					Identifiers: []types.Identifier{
						{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001SQCQC5"},
						{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000MM2P62"},
						ticker("XNAS", "FB"),
					},
					Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS",
				},
			}}},
		},
		{
			name:    "a ticker not in use on the day",
			unknown: true,
			want:    market.IdentifierEventResult{Assertions: []market.Assertion{{Value: ticker("XNAS", "FB"), At: on, Negative: true}}},
		},
		{name: "a record of a class the integration does not convert", rec: &record{Ticker: "FB", Market: "stocks", Type: "WARRANT"}},
		{name: "a record of another market", rec: &record{Ticker: "FB", Market: "otc", Type: "CS"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lookupFacts("FB", tc.rec, tc.unknown, on, []string{"XNAS"}, mics)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("lookupFacts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestDelistingFacts checks that a delisting inside the period retires the
// ticker on its UTC day.
func TestDelistingFacts(t *testing.T) {
	on := time.Date(2022, time.October, 31, 4, 0, 0, 0, time.UTC)
	ds := []delisting{{Delisted: &on}, {}}
	retired := market.IdentifierEventResult{Events: []market.IdentifierEvent{
		{Value: ticker("XNYS", "TWTR"), Action: gen.IdentifierEventActionRetired, On: day(2022, 10, 31)},
	}}
	tests := []struct {
		name string
		p    types.Period
		want market.IdentifierEventResult
	}{
		{name: "inside the period", p: history, want: retired},
		{name: "after the period", p: period(since, day(2022, 10, 31))},
		{name: "before the period", p: period(day(2022, 11, 1), day(2026, 10, 23))},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, delistingFacts("TWTR", ds, tc.p, []string{"XNYS"})); diff != "" {
				t.Errorf("delistingFacts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// sorted compares identifier facts in any order, because Fetch emits them
// venue by venue.
var sorted = cmpopts.SortSlices(func(a, b any) bool { return fmt.Sprint(a) < fmt.Sprint(b) })

// TestEventsFetch checks what the identifier events server makes of each
// recorded reply, at every venue in the test's MIC map.
func TestEventsFetch(t *testing.T) {
	doms := []string{"BATS", "XNAS", "XNYS"}
	figi := func(id types.Identifier) []types.Identifier { return []types.Identifier{id} }
	tests := []struct {
		cassette string
		req      market.IdentifierEventRequest
		sent     types.Identifier
		want     func(dom string) market.IdentifierEventResult
	}{
		{
			cassette: "massive_events_ticker_change",
			req:      market.IdentifierEventRequest{Identifier: meta, Period: history},
			sent:     meta,
			want: func(d string) market.IdentifierEventResult {
				return market.IdentifierEventResult{
					Events: []market.IdentifierEvent{
						{Value: ticker(d, "FB"), Action: gen.IdentifierEventActionAssigned, On: day(2012, 5, 18)},
						{Value: ticker(d, "FB"), Action: gen.IdentifierEventActionRenamed, ToValue: "META", On: day(2022, 6, 9)},
					},
					Assertions: []market.Assertion{
						{Value: ticker(d, "FB"), Of: market.Listing{Identifiers: figi(meta)}, At: day(2012, 5, 18)},
						{Value: ticker(d, "META"), Of: market.Listing{Identifiers: figi(meta)}, At: day(2022, 6, 9)},
						{Value: ticker(d, "META"), Of: market.Listing{Identifiers: figi(meta)}, At: day(2026, 10, 22)},
					},
					Coverage: []market.Coverage{
						{Identifier: ticker(d, "FB"), Period: period(day(2012, 5, 18), day(2022, 6, 9))},
						{Identifier: ticker(d, "META"), Period: period(day(2022, 6, 9), history.Before)},
					},
				}
			},
		},
		{
			cassette: "massive_events_unchanged",
			req:      market.IdentifierEventRequest{Identifier: apple[1], Period: history},
			sent:     apple[1],
			want: func(d string) market.IdentifierEventResult {
				return market.IdentifierEventResult{
					Assertions: []market.Assertion{
						{Value: ticker(d, "AAPL"), Of: market.Listing{Identifiers: figi(apple[1])}, At: since},
						{Value: ticker(d, "AAPL"), Of: market.Listing{Identifiers: figi(apple[1])}, At: day(2026, 10, 22)},
					},
					Coverage: []market.Coverage{{Identifier: ticker(d, "AAPL"), Period: history}},
				}
			},
		},
		{
			cassette: "massive_events_unknown",
			req:      market.IdentifierEventRequest{Identifier: twitter, Period: history},
			sent:     twitter,
			want:     func(string) market.IdentifierEventResult { return market.IdentifierEventResult{} },
		},
		{
			cassette: "massive_point_in_time",
			req:      market.IdentifierEventRequest{Identifier: ticker("XNAS", "FB"), Period: oneDay},
			sent:     ticker("", "FB"),
			want: func(d string) market.IdentifierEventResult {
				return market.IdentifierEventResult{Assertions: []market.Assertion{{Value: ticker(d, "FB"), At: oneDay.From, Of: market.Listing{Identifiers: []types.Identifier{
					{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001SQCQC5"},
					{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000MM2P62"},
					ticker("XNAS", "FB"),
				}, Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS"}}}}
			},
		},
		{
			cassette: "massive_point_in_time_unknown",
			req:      market.IdentifierEventRequest{Identifier: ticker("XNAS", "META"), Period: oneDay},
			sent:     ticker("", "META"),
			want: func(d string) market.IdentifierEventResult {
				return market.IdentifierEventResult{Assertions: []market.Assertion{{Value: ticker(d, "META"), At: oneDay.From, Negative: true}}}
			},
		},
		{
			cassette: "massive_retired",
			req:      market.IdentifierEventRequest{Identifier: ticker("XNYS", "TWTR"), Period: history},
			sent:     ticker("", "TWTR"),
			want: func(d string) market.IdentifierEventResult {
				return market.IdentifierEventResult{Events: []market.IdentifierEvent{{Value: ticker(d, "TWTR"), Action: gen.IdentifierEventActionRetired, On: day(2022, 10, 31)}}}
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette))
			s := c.Events()
			sent, err := s.Serves(tc.req)
			require.NoError(t, err)
			if sent != tc.sent {
				t.Fatalf("Serves() = %v, want %v", sent, tc.sent)
			}
			got, err := s.Fetch(context.Background(), []market.Request[market.IdentifierEventRequest]{{Value: tc.req, Sent: sent}})
			require.NoError(t, err)
			var want market.IdentifierEventResult
			for _, d := range doms {
				w := tc.want(d)
				want.Events = append(want.Events, w.Events...)
				want.Assertions = append(want.Assertions, w.Assertions...)
				want.Coverage = append(want.Coverage, w.Coverage...)
			}
			if diff := cmp.Diff([]market.Response[market.IdentifierEventResult]{{Value: want}}, got, sorted); diff != "" {
				t.Errorf("Fetch mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
