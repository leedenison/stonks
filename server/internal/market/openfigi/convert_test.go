package openfigi

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func listing(ticker, exch string) result {
	return result{
		Ticker: ticker, ExchCode: exch,
		SecurityType: "Common Stock", SecurityType2: "Common Stock", MarketSector: "Equity",
		ShareClassFIGI: ptr.To("BBG001S6XK38"), CompositeFIGI: ptr.To("BBG000DWG505"),
	}
}

func TestCandidate(t *testing.T) {
	tests := []struct {
		name string
		in   result
		want []types.Identifier
	}{
		{
			name: "venue",
			in:   listing("BRK/B", "UN"),
			want: []types.Identifier{
				{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S6XK38"},
				{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000DWG505"},
				{Type: types.IdentifierTypeOpenfigiTicker, Domain: "UN", Value: "BRK/B"},
				{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "BRK.B"},
			},
		},
		{
			name: "composite",
			in:   listing("BRK/B", "US"),
			want: []types.Identifier{
				{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S6XK38"},
				{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000DWG505"},
				{Type: types.IdentifierTypeOpenfigiTicker, Domain: "US", Value: "BRK/B"},
			},
		},
		{
			name: "bond at a venue",
			in:   result{Ticker: "T 2 1/2 05/15/24", ExchCode: "UN", MarketSector: "Govt"},
			want: []types.Identifier{{Type: types.IdentifierTypeOpenfigiTicker, Domain: "UN", Value: "T 2 1/2 05/15/24"}},
		},
		{
			name: "null figis",
			in:   result{Ticker: "T 2 1/2 05/15/24", ExchCode: "", MarketSector: "Govt"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := candidate(tc.in, mics)
			if diff := cmp.Diff(tc.want, got.Identifiers); diff != "" {
				t.Errorf("candidate(%+v) identifiers (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

func TestAnswer(t *testing.T) {
	data := []result{listing("AAPL", "US"), listing("AAPL", "UW"), listing("AAPL", "UN"), listing("AAPL", "LN")}
	tests := []struct {
		name     string
		sent     types.Identifier
		currency string
		exchs    []string
	}{
		{
			name:  "isin keeps every listing",
			sent:  types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"},
			exchs: []string{"US", "UW", "UN", "LN"},
		},
		{
			name:  "bare ticker keeps every listing",
			sent:  types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "AAPL"},
			exchs: []string{"US", "UW", "UN", "LN"},
		},
		{
			name:  "ticker at a venue keeps its listings",
			sent:  types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"},
			exchs: []string{"UW"},
		},
		{
			name:     "currency filtered",
			sent:     types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"},
			currency: "USD",
			exchs:    []string{"US", "UW", "UN", "LN"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := answer(tc.sent, tc.currency, data, mics)
			if diff := cmp.Diff([]types.Identifier{tc.sent}, got.Filtered); diff != "" {
				t.Errorf("answer(%v) filtered (-want +got):\n%s", tc.sent, diff)
			}
			var exchs []string
			for _, c := range got.Candidates {
				exchs = append(exchs, exchOf(c))
				if c.Class != gen.AssetClassStock || c.Currency != tc.currency {
					t.Errorf("answer(%v) candidate class %s, currency %q, want stock and %q", tc.sent, c.Class, c.Currency, tc.currency)
				}
			}
			if diff := cmp.Diff(tc.exchs, exchs); diff != "" {
				t.Errorf("answer(%v) listings (-want +got):\n%s", tc.sent, diff)
			}
		})
	}
}

func exchOf(c market.Candidate) string {
	for _, id := range c.Identifiers {
		if id.Type == types.IdentifierTypeOpenfigiTicker {
			return id.Domain
		}
	}
	return ""
}
