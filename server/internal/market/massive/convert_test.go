package massive

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

func TestCandidate(t *testing.T) {
	figis := []types.Identifier{
		{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S5N8V8"},
		{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000B9XRY4"},
	}
	stock := record{Ticker: "AAPL", Market: "stocks", PrimaryExchange: "XNGS", Type: "CS", CurrencyName: "usd", ShareClassFIGI: "BBG001S5N8V8", CompositeFIGI: "BBG000B9XRY4"}
	tests := []struct {
		name string
		edit func(*record)
		want market.Candidate
		ok   bool
	}{
		{name: "a common stock at a segment", want: market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: append(figis, ticker("XNAS", "AAPL"))}, ok: true},
		{name: "an ETF", edit: func(r *record) { r.Type = "ETF" }, want: market.Candidate{Class: gen.AssetClassEtf, Currency: "USD", Primary: "XNAS", Identifiers: append(figis, ticker("XNAS", "AAPL"))}, ok: true},
		{name: "a fund", edit: func(r *record) { r.Type = "FUND" }, want: market.Candidate{Class: gen.AssetClassMutualFund, Currency: "USD", Primary: "XNAS", Identifiers: append(figis, ticker("XNAS", "AAPL"))}, ok: true},
		{name: "a preferred share has no ticker but states its primary venue", edit: func(r *record) { r.Type, r.Ticker = "PFD", "BACpL" }, want: market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: figis}, ok: true},
		{name: "an exchange outside the table has neither a ticker nor a primary venue", edit: func(r *record) { r.PrimaryExchange = "XXXX" }, want: market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Identifiers: figis}, ok: true},
		{name: "a warrant", edit: func(r *record) { r.Type = "WARRANT" }},
		{name: "an unknown type", edit: func(r *record) { r.Type = "NEW" }},
		{name: "an OTC listing", edit: func(r *record) { r.Market = "otc" }},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := stock
			if tc.edit != nil {
				tc.edit(&r)
			}
			got, ok := candidate(r, mics)
			if ok != tc.ok {
				t.Fatalf("candidate() ok = %v, want %v", ok, tc.ok)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("candidate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
