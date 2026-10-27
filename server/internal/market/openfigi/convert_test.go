package openfigi

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

func listing(ticker, exch string) result {
	return result{
		Ticker: ticker, ExchCode: exch,
		SecurityType: "Common Stock", SecurityType2: "Common Stock", MarketSector: "Equity",
		ShareClassFIGI: ptr.To("BBG001S6XK38"), CompositeFIGI: ptr.To("BBG000DWG505"),
	}
}

func TestToListing(t *testing.T) {
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
			got := toListing(tc.in, mics)
			if diff := cmp.Diff(tc.want, got.Identifiers); diff != "" {
				t.Errorf("toListing(%+v) identifiers (-want +got):\n%s", tc.in, diff)
			}
		})
	}
}

func TestIdentity(t *testing.T) {
	data := []result{listing("AAPL", "US"), listing("AAPL", "UW"), listing("AAPL", "UN"), listing("AAPL", "LN")}
	tests := []struct {
		name     string
		sent     types.Identifier
		currency string
		// filtered is the call's filter, sent where empty.
		filtered types.Identifier
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
			name:     "ticker at a venue keeps every listing",
			sent:     types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"},
			filtered: types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "AAPL"},
			exchs:    []string{"US", "UW", "UN", "LN"},
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
			got := identity(tc.sent, tc.currency, data, mics)
			if tc.filtered == (types.Identifier{}) {
				tc.filtered = tc.sent
			}
			if diff := cmp.Diff([]types.Identifier{tc.filtered}, got.Filtered); diff != "" {
				t.Errorf("identity(%v) filtered (-want +got):\n%s", tc.sent, diff)
			}
			var exchs []string
			for _, c := range got.Listings {
				exchs = append(exchs, exchOf(c))
				if c.Class != types.AssetClassStock || c.Currency != tc.currency {
					t.Errorf("identity(%v) listing class %s, currency %q, want stock and %q", tc.sent, c.Class, c.Currency, tc.currency)
				}
			}
			if diff := cmp.Diff(tc.exchs, exchs); diff != "" {
				t.Errorf("identity(%v) listings (-want +got):\n%s", tc.sent, diff)
			}
		})
	}
}

func exchOf(c market.Listing) string {
	for _, id := range c.Identifiers {
		if id.Type == types.IdentifierTypeOpenfigiTicker {
			return id.Domain
		}
	}
	return ""
}

// venueMICs is a MIC map holding the venues TestVenue names.
var venueMICs = mic.Of(map[string]string{
	"XNYS": "XNYS", "ARCX": "XNYS", "XNAS": "XNAS", "XNGS": "XNAS",
	"XLON": "XLON", "XTAI": "XTAI", "ROCO": "ROCO",
})

func TestVenue(t *testing.T) {
	tests := []struct {
		code   string
		want   string
		wantOK bool
	}{
		{code: "UN", want: "XNYS", wantOK: true},
		{code: "UW", want: "XNAS", wantOK: true},
		{code: "UP", want: "XNYS", wantOK: true},
		{code: "LN", want: "XLON", wantOK: true},
		{code: "US"},
		{code: "TT"},
		{code: "GY"},
		{code: ""},
	}
	for _, tc := range tests {
		got, ok := venue(tc.code, venueMICs)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("venue(%q) = %q, %v, want %q, %v", tc.code, got, ok, tc.want, tc.wantOK)
		}
	}
}
