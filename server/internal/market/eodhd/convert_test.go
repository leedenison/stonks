package eodhd

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

func TestCandidate(t *testing.T) {
	c := client(t)
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	symbol := func(v string) types.Identifier {
		return types.Identifier{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: v}
	}
	mapped := mapping{Symbol: "AAPL.US", ISIN: "US0378331005", CUSIP: "037833100"}
	cusip := types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"}
	us := row{Code: "AAPL", Exchange: "US", Type: "Common Stock", Currency: "USD", ISIN: "US0378331005"}
	tests := []struct {
		name    string
		row     row
		mapping mapping
		venue   string
		want    market.Candidate
		ok      bool
	}{
		{
			name: "a US common stock with its mapping", row: us, mapping: mapped, venue: "NASDAQ", ok: true,
			want: market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{symbol("AAPL.US"), isin, cusip, ticker("XNAS", "AAPL")}},
		},
		{
			name: "a class share at a segment", row: row{Code: "BRK-B", Exchange: "US", Type: "Common Stock", Currency: "USD"}, venue: "NYSE ARCA", ok: true,
			want: market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{symbol("BRK-B.US"), ticker("XNYS", "BRK.B")}},
		},
		{
			name: "a listing outside the US", row: row{Code: "VOD", Exchange: "LSE", Type: "Common Stock", Currency: "GBX", ISIN: "GB00BH4HKS39"}, ok: true,
			want: market.Candidate{Class: gen.AssetClassStock, Currency: "GBX", Identifiers: []types.Identifier{
				symbol("VOD.LSE"), {Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}, ticker("XLON", "VOD"),
			}},
		},
		{
			name: "a preferred share has no ticker", row: row{Code: "BAC-PL", Exchange: "US", Type: "Preferred Stock", Currency: "USD"}, venue: "NYSE", ok: true,
			want: market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{symbol("BAC-PL.US")}},
		},
		{
			name: "an ETF", row: row{Code: "AAAU", Exchange: "US", Type: "ETF", Currency: "USD"}, venue: "BATS", ok: true,
			want: market.Candidate{Class: gen.AssetClassEtf, Currency: "USD", Identifiers: []types.Identifier{symbol("AAAU.US"), ticker("BATS", "AAAU")}},
		},
		{
			name: "a fund", row: row{Code: "VUSA", Exchange: "LSE", Type: "FUND", Currency: "GBP"}, ok: true,
			want: market.Candidate{Class: gen.AssetClassMutualFund, Currency: "GBP", Identifiers: []types.Identifier{symbol("VUSA.LSE"), ticker("XLON", "VUSA")}},
		},
		{name: "a US row on an OTC market", row: us, venue: "PINK"},
		{name: "a US row the symbol list leaves out", row: us},
		{name: "a US mutual fund", row: row{Code: "VFIAX", Exchange: "US", Type: "FUND", Currency: "USD"}, venue: "NMFQS"},
		{name: "a warrant", row: row{Code: "ABCW", Exchange: "US", Type: "Warrant", Currency: "USD"}, venue: "NASDAQ"},
		{name: "a virtual exchange", row: row{Code: "VWRL", Exchange: "EUFUND", Type: "FUND", Currency: "EUR"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := c.candidate(tc.row, tc.mapping, tc.venue)
			if ok != tc.ok {
				t.Fatalf("candidate(%+v) ok = %v, want %v", tc.row, ok, tc.ok)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("candidate(%+v) mismatch (-want +got):\n%s", tc.row, diff)
			}
		})
	}
}
