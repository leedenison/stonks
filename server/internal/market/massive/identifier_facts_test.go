package massive

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

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

// us is value in the market US.
func us(value string) types.MarketTicker { return types.MarketTicker{Market: "US", Value: value} }

// asserted is a period of assertion coverage of value in the market US over p.
func asserted(value string, p types.Period) market.AssertionCoverage {
	t := us(value)
	return market.AssertionCoverage{Market: "US", Ticker: &t, Period: p}
}

// TestChainFacts checks the identifier facts of an entity's chain.
func TestChainFacts(t *testing.T) {
	today := day(2026, 10, 22)
	p := period(since, today.AddDate(0, 0, 1))
	// link asserts value over [from, before), with an edge at each bound its
	// flags mark.
	link := func(value string, from, before time.Time, edgeFrom, edgeBefore bool) market.Assertion {
		return market.Assertion{
			Ticker: us(value), Of: market.Listing{Identifiers: []types.Identifier{meta}},
			Period: period(from, before), EdgeFrom: edgeFrom, EdgeBefore: edgeBefore,
		}
	}
	over := func(value string, from, before time.Time) market.AssertionCoverage {
		return asserted(value, period(from, before))
	}
	tests := []struct {
		name string
		evs  []event
		p    types.Period
		want market.IdentifierFactResult
	}{
		{
			name: "a chain of three tickers",
			evs:  []event{change("CCC", "2020-06-01"), change("BBB", "2015-03-02"), change("AAA", "2010-01-04")},
			p:    p,
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					link("AAA", day(2010, 1, 4), day(2015, 3, 2), true, true),
					link("BBB", day(2015, 3, 2), day(2020, 6, 1), true, true),
					link("CCC", day(2020, 6, 1), p.Before, true, false),
				},
				AssertionCoverage: []market.AssertionCoverage{
					over("AAA", day(2010, 1, 4), day(2015, 3, 2)),
					over("BBB", day(2015, 3, 2), day(2020, 6, 1)),
					over("CCC", day(2020, 6, 1), p.Before),
				},
			},
		},
		{
			name: "a ticker already trading when Massive's data starts",
			evs:  []event{change("AAPL", "2003-09-10")},
			p:    p,
			want: market.IdentifierFactResult{
				Assertions:        []market.Assertion{link("AAPL", since, p.Before, false, false)},
				AssertionCoverage: []market.AssertionCoverage{over("AAPL", since, p.Before)},
			},
		},
		{
			name: "a chain trimmed to the period",
			evs:  []event{change("BBB", "2025-03-03"), change("AAA", "2024-10-22")},
			p:    period(day(2024, 10, 22), p.Before),
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					link("AAA", day(2024, 10, 22), day(2025, 3, 3), false, true),
					link("BBB", day(2025, 3, 3), p.Before, true, false),
				},
				AssertionCoverage: []market.AssertionCoverage{
					over("AAA", day(2024, 10, 22), day(2025, 3, 3)),
					over("BBB", day(2025, 3, 3), p.Before),
				},
			},
		},
		{
			name: "assertion coverage clipped to the period",
			evs:  []event{change("BBB", "2025-03-03"), change("AAA", "2010-01-04")},
			p:    period(day(2024, 10, 22), day(2025, 6, 2)),
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					link("AAA", day(2010, 1, 4), day(2025, 3, 3), false, true),
					link("BBB", day(2025, 3, 3), day(2025, 6, 2), true, false),
				},
				AssertionCoverage: []market.AssertionCoverage{
					over("AAA", day(2024, 10, 22), day(2025, 3, 3)),
					over("BBB", day(2025, 3, 3), day(2025, 6, 2)),
				},
			},
		},
		{
			name: "a newest ticker that began today",
			evs:  []event{change("AAA", "2026-10-22")},
			p:    p,
			want: market.IdentifierFactResult{
				Assertions:        []market.Assertion{link("AAA", today, p.Before, true, false)},
				AssertionCoverage: []market.AssertionCoverage{over("AAA", today, p.Before)},
			},
		},
		{
			name: "a ticker held for one day",
			evs:  []event{change("CCC", "2020-06-02"), change("BBB", "2020-06-01"), change("AAA", "2010-01-04")},
			p:    p,
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					link("AAA", day(2010, 1, 4), day(2020, 6, 1), true, true),
					link("BBB", day(2020, 6, 1), day(2020, 6, 2), true, true),
					link("CCC", day(2020, 6, 2), p.Before, true, false),
				},
				AssertionCoverage: []market.AssertionCoverage{
					over("AAA", day(2010, 1, 4), day(2020, 6, 1)),
					over("BBB", day(2020, 6, 1), day(2020, 6, 2)),
					over("CCC", day(2020, 6, 2), p.Before),
				},
			},
		},
		{
			name: "an oldest ticker renamed on the day after the period starts",
			evs:  []event{change("BBB", "2024-10-23"), change("AAA", "2024-10-22")},
			p:    period(day(2024, 10, 22), p.Before),
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					link("AAA", day(2024, 10, 22), day(2024, 10, 23), false, true),
					link("BBB", day(2024, 10, 23), p.Before, true, false),
				},
				AssertionCoverage: []market.AssertionCoverage{
					over("AAA", day(2024, 10, 22), day(2024, 10, 23)),
					over("BBB", day(2024, 10, 23), p.Before),
				},
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
			want: market.IdentifierFactResult{
				Assertions:        []market.Assertion{link("BRK.B", since, p.Before, false, false)},
				AssertionCoverage: []market.AssertionCoverage{over("BRK.B", since, p.Before)},
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := chainFacts(meta, tc.evs, tc.p, "US")
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("chainFacts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestChainFactsMarket checks that each fact is stated once, at the market.
func TestChainFactsMarket(t *testing.T) {
	got, err := chainFacts(meta, []event{change("META", "2022-06-09"), change("FB", "2012-05-18")}, history, "US")
	require.NoError(t, err)
	if len(got.Assertions) != 2 || len(got.AssertionCoverage) != 2 {
		t.Errorf("chainFacts = %d assertions and %d rows of assertion coverage, want 2 and 2", len(got.Assertions), len(got.AssertionCoverage))
	}
	for _, a := range got.Assertions {
		if a.Ticker.Market != "US" {
			t.Errorf("assertion at %s, want US", a.Ticker.Market)
		}
	}
}

// TestChainFactsDate checks that an event date in a form other than
// YYYY-MM-DD fails the conversion.
func TestChainFactsDate(t *testing.T) {
	if _, err := chainFacts(meta, []event{change("META", "June 9, 2022")}, history, "US"); err == nil {
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
		want    market.IdentifierFactResult
	}{
		{
			name: "a record of the day",
			rec:  fb,
			want: market.IdentifierFactResult{Assertions: []market.Assertion{{
				Ticker: us("FB"), Period: types.OneDay(on), Of: market.Listing{
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
			want:    market.IdentifierFactResult{Assertions: []market.Assertion{{Ticker: us("FB"), Period: types.OneDay(on), Negative: true}}},
		},
		{name: "a record of a class the integration does not convert", rec: &record{Ticker: "FB", Market: "stocks", Type: "WARRANT"}},
		{name: "a record of another market", rec: &record{Ticker: "FB", Market: "otc", Type: "CS"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := lookupFacts("FB", tc.rec, tc.unknown, on, "US", mics)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("lookupFacts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestFactsFetch checks what the identifier facts server makes of each
// recorded reply, at the market of the test's MIC map.
func TestFactsFetch(t *testing.T) {
	figi := func(id types.Identifier) []types.Identifier { return []types.Identifier{id} }
	tests := []struct {
		cassette string
		req      market.IdentifierFactRequest
		sent     types.Identifier
		want     market.IdentifierFactResult
	}{
		{
			cassette: "massive_events_ticker_change",
			req:      market.IdentifierFactRequest{Identifier: meta, Period: history},
			sent:     meta,
			// The edges read Massive's date as the first day under the new
			// ticker. The recorded reply dates META on 2022-06-09, the day
			// the ticker began trading.
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					{Ticker: us("FB"), Of: market.Listing{Identifiers: figi(meta)}, Period: period(day(2012, 5, 18), day(2022, 6, 9)), EdgeFrom: true, EdgeBefore: true},
					{Ticker: us("META"), Of: market.Listing{Identifiers: figi(meta)}, Period: period(day(2022, 6, 9), history.Before), EdgeFrom: true},
				},
				AssertionCoverage: []market.AssertionCoverage{
					asserted("FB", period(day(2012, 5, 18), day(2022, 6, 9))),
					asserted("META", period(day(2022, 6, 9), history.Before)),
				},
			},
		},
		{
			cassette: "massive_events_unchanged",
			req:      market.IdentifierFactRequest{Identifier: apple[1], Period: history},
			sent:     apple[1],
			want: market.IdentifierFactResult{
				Assertions: []market.Assertion{
					{Ticker: us("AAPL"), Of: market.Listing{Identifiers: figi(apple[1])}, Period: period(since, history.Before)},
				},
				AssertionCoverage: []market.AssertionCoverage{asserted("AAPL", history)},
			},
		},
		{
			cassette: "massive_events_unknown",
			req:      market.IdentifierFactRequest{Identifier: twitter, Period: history},
			sent:     twitter,
			want:     market.IdentifierFactResult{},
		},
		{
			cassette: "massive_point_in_time",
			req:      market.IdentifierFactRequest{Identifier: ticker("XNAS", "FB"), Period: oneDay},
			sent:     ticker("", "FB"),
			want: market.IdentifierFactResult{Assertions: []market.Assertion{{Ticker: us("FB"), Period: types.OneDay(oneDay.From), Of: market.Listing{Identifiers: []types.Identifier{
				{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001SQCQC5"},
				{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000MM2P62"},
				ticker("XNAS", "FB"),
			}, Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS"}}}},
		},
		{
			cassette: "massive_point_in_time_unknown",
			req:      market.IdentifierFactRequest{Identifier: ticker("XNAS", "META"), Period: oneDay},
			sent:     ticker("", "META"),
			want:     market.IdentifierFactResult{Assertions: []market.Assertion{{Ticker: us("META"), Period: types.OneDay(oneDay.From), Negative: true}}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette))
			s := c.Facts()
			sent, err := s.Serves(tc.req)
			require.NoError(t, err)
			if sent != tc.sent {
				t.Fatalf("Serves() = %v, want %v", sent, tc.sent)
			}
			got, err := s.Fetch(context.Background(), []market.Request[market.IdentifierFactRequest]{{Value: tc.req, Sent: sent}})
			require.NoError(t, err)
			if diff := cmp.Diff([]market.Response[market.IdentifierFactResult]{{Value: tc.want}}, got); diff != "" {
				t.Errorf("Fetch mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
