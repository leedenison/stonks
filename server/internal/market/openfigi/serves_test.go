package openfigi

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func TestServes(t *testing.T) {
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	cusip := types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"}
	bare := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "AAPL"}
	tests := []struct {
		name    string
		stated  []types.Identifier
		want    types.Identifier
		wantErr bool
	}{
		{name: "strongest first", stated: []types.Identifier{bare, cusip, isin}, want: isin},
		{name: "cusip", stated: []types.Identifier{bare, cusip}, want: cusip},
		{name: "bare ticker", stated: []types.Identifier{bare}, want: bare},
		{
			name:   "segment normalised",
			stated: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "xngs", Value: "AAPL"}},
			want:   types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"},
		},
		{
			name:    "unknown venue",
			stated:  []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "NASDAQ", Value: "AAPL"}},
			wantErr: true,
		},
		{
			name: "unknown venue, then a known one",
			stated: []types.Identifier{
				{Type: types.IdentifierTypeMicTicker, Domain: "NASDAQ", Value: "AAPL"},
				{Type: types.IdentifierTypeMicTicker, Domain: "XNGS", Value: "AAPL"},
			},
			want: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"},
		},
		{name: "option", stated: []types.Identifier{{Type: types.IdentifierTypeOcc, Value: "AAPL  250117C00150000"}}, wantErr: true},
		{name: "bare ticker beside an option", stated: []types.Identifier{bare, {Type: types.IdentifierTypeOcc, Value: "AAPL  250117C00150000"}}, wantErr: true},
		{name: "bare ticker beside a broker id, which is not a GUID", stated: []types.Identifier{bare, {Type: types.IdentifierTypeBrokerID, Domain: "ibkr", Value: "265598"}}, want: bare},
		{name: "broker id", stated: []types.Identifier{{Type: types.IdentifierTypeBrokerID, Domain: "ibkr", Value: "265598"}}, wantErr: true},
		{name: "nothing", wantErr: true},
	}
	c := &Client{mics: mics}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Serves(gen.StatedKey{Identifiers: tc.stated})
			if (err != nil) != tc.wantErr {
				t.Fatalf("Serves(%v) error = %v, wantErr %v", tc.stated, err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Serves(%v) (-want +got):\n%s", tc.stated, diff)
			}
		})
	}
}

func TestJobOf(t *testing.T) {
	tests := []struct {
		id       types.Identifier
		currency string
		want     job
	}{
		{id: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}, want: job{IDType: "ID_ISIN", IDValue: "US0378331005"}},
		{id: types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000B9XRY4"}, want: job{IDType: "COMPOSITE_ID_BB_GLOBAL", IDValue: "BBG000B9XRY4"}},
		{id: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "BRK.B"}, want: job{IDType: "TICKER", IDValue: "BRK/B"}},
		{id: types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "BRK B"}, want: job{IDType: "TICKER", IDValue: "BRK/B"}},
		{id: types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "BF-B"}, want: job{IDType: "TICKER", IDValue: "BF/B"}},
		{id: types.Identifier{Type: types.IdentifierTypeOpenfigiTicker, Domain: "UN", Value: "BRK.B"}, want: job{IDType: "TICKER", IDValue: "BRK/B", ExchCode: "UN"}},
		{id: types.Identifier{Type: types.IdentifierTypeOpenfigiTicker, Domain: "US", Value: "T 2 1/2 05/15/24"}, want: job{IDType: "TICKER", IDValue: "T 2 1/2 05/15/24", ExchCode: "US"}},
		{id: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}, currency: "USD", want: job{IDType: "ID_ISIN", IDValue: "US0378331005", Currency: "USD"}},
		{id: types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}, currency: "GBp", want: job{IDType: "ID_ISIN", IDValue: "GB00BH4HKS39", Currency: "GBp"}},
	}
	for _, tc := range tests {
		if got := jobOf(tc.id, tc.currency); got != tc.want {
			t.Errorf("jobOf(%v, %q) = %+v, want %+v", tc.id, tc.currency, got, tc.want)
		}
	}
}

// TestParams checks that the currency filter, as the job spells it, is the
// one parameter a key adds to its cache key.
func TestParams(t *testing.T) {
	tests := []struct {
		name     string
		currency *string
		want     []string
	}{
		{name: "no currency", currency: nil, want: nil},
		{name: "a major unit", currency: ptr.To("USD"), want: []string{"USD"}},
		{name: "a minor unit as OpenFIGI spells it", currency: ptr.To("GBX"), want: []string{"GBp"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			c := New(market.Config{}, nil)
			got := c.Params(gen.StatedKey{Currency: tc.currency})
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Params() (-want +got):\n%s", diff)
			}
		})
	}
}
