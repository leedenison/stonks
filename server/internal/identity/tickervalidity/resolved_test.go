package tickervalidity

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/types"
)

// ticker returns value at the market US with the facts as, derived.
func ticker(value string, as ...Assertion) *Ticker {
	f := Facts{Assertions: as}
	return &Ticker{Ticker: types.MarketTicker{Market: "US", Value: value}, Facts: f, Derived: Derive(f)}
}

// stating returns a stating venue and currency.
func stating(a Assertion, venue, currency string) Assertion {
	a.Stated = Stated{Venue: venue, Currency: currency}
	return a
}

func TestResolve(t *testing.T) {
	us := func(v string) types.MarketTicker { return types.MarketTicker{Market: "US", Value: v} }
	tests := []struct {
		name string
		ts   []*Ticker
		want []Resolved
	}{
		{
			name: "the ticker whose last interval ends latest names the instrument",
			ts: []*Ticker{
				ticker("OLD", asserts(1, &a, alpha), asserts(5, &b, alpha)),
				ticker("NEW", asserts(6, &a, alpha)),
			},
			want: []Resolved{{Ticker: us("NEW"), Instr: a}},
		},
		{
			name: "a ticker that ends names the instrument when no other names it later",
			ts:   []*Ticker{ticker("OLD", asserts(1, &a, alpha), asserts(5, &b, alpha))},
			want: []Resolved{{Ticker: us("OLD"), Instr: a, Before: at(5)}},
		},
		{
			name: "a tie on an open interval goes to the latest winner",
			ts: []*Ticker{
				ticker("XX", asserts(5, &a, alpha)),
				ticker("YY", asserts(3, &a, alpha)),
			},
			want: []Resolved{{Ticker: us("XX"), Instr: a}},
		},
		{
			name: "a tie on the latest winner goes to the lower value",
			ts: []*Ticker{
				ticker("YY", asserts(3, &a, alpha)),
				ticker("XX", asserts(3, &a, alpha)),
			},
			want: []Resolved{{Ticker: us("XX"), Instr: a}},
		},
		{
			name: "with no venue stated, one row holds the market and the latest currency",
			ts: []*Ticker{ticker("FOO",
				stating(asserts(1, &a, alpha), "", "USD"),
				stating(asserts(2, &a, beta), "", "GBP"),
				asserts(3, &a, alpha),
			)},
			want: []Resolved{{Ticker: us("FOO"), Instr: a, Stated: Stated{Currency: "GBP"}}},
		},
		{
			name: "each venue gets a row with its own latest currency",
			ts: []*Ticker{ticker("FOO",
				stating(asserts(1, &a, alpha), "XNYS", "USD"),
				stating(asserts(2, &a, alpha), "XNAS", "USD"),
				stating(asserts(3, &a, alpha), "XNYS", ""),
				asserts(4, &a, beta),
			)},
			want: []Resolved{
				{Ticker: us("FOO"), Instr: a, Stated: Stated{Venue: "XNAS", Currency: "USD"}},
				{Ticker: us("FOO"), Instr: a, Stated: Stated{Venue: "XNYS", Currency: "USD"}},
			},
		},
		{
			name: "the span stops where another instrument broke it",
			ts:   []*Ticker{ticker("FOO", asserts(1, &a, alpha), asserts(5, &b, alpha), asserts(9, &a, alpha))},
			want: []Resolved{{Ticker: us("FOO"), Instr: a, From: at(9)}},
		},
		{
			name: "a ticker that never names the instrument gets no row",
			ts:   []*Ticker{ticker("FOO", asserts(1, &b, alpha))},
			want: nil,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if diff := cmp.Diff(tt.want, Resolve(a, tt.ts)); diff != "" {
				t.Errorf("Resolve mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
