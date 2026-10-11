package market

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/mic"
)

func TestIsGUID(t *testing.T) {
	tests := []struct {
		id   types.Identifier
		want bool
	}{
		{types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}, true},
		{types.Identifier{Type: types.IdentifierTypeSedol, Value: "BH4HKS3"}, true},
		{types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}, true},
		{types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "VOD"}, false},
		{types.Identifier{Type: types.IdentifierTypeBrokerID, Domain: "ibkr", Value: "12345"}, false},
		{types.Identifier{Type: types.IdentifierTypeDatasourceTicker, Domain: "eodhd", Value: "VOD.LSE"}, false},
	}
	for _, tc := range tests {
		if got := IsGUID(tc.id); got != tc.want {
			t.Errorf("IsGUID(%+v) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestWithClassSep(t *testing.T) {
	tests := []struct {
		ticker string
		want   string
		ok     bool
	}{
		{"BRK.B", "BRK/B", true},
		{"BRK-B", "BRK/B", true},
		{"BRK B", "BRK/B", true},
		{"AAPL", "AAPL", true},
		{"A.B.C", "A.B.C", false},
	}
	for _, tc := range tests {
		if got, ok := WithClassSep(tc.ticker, '/'); got != tc.want || ok != tc.ok {
			t.Errorf("WithClassSep(%q) = %q, %v, want %q, %v", tc.ticker, got, ok, tc.want, tc.ok)
		}
	}
}

// venueMICs is a MIC table holding the venues TestOpenFIGIVenue names.
var venueMICs = mic.Table{
	"XNYS": "XNYS", "ARCX": "XNYS", "XNAS": "XNAS", "XNGS": "XNAS",
	"XLON": "XLON", "XTAI": "XTAI", "ROCO": "ROCO",
}

func TestOpenFIGIVenue(t *testing.T) {
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
		got, ok := OpenFIGIVenue(tc.code, venueMICs)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("OpenFIGIVenue(%q) = %q, %v, want %q, %v", tc.code, got, ok, tc.want, tc.wantOK)
		}
	}
}

// TestPreference checks the rule that picks which identifier of a key an
// integration sends.
func TestPreference(t *testing.T) {
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	cusip := types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"}
	ticker := func(venue, value string) types.Identifier {
		return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value}
	}
	mics := mic.Table{"XNYS": "XNYS", "XNGS": "XNAS", "XNAS": "XNAS", "XLON": "XLON"}
	us := func(op string) bool { return op == "XNYS" || op == "XNAS" }
	limited := Preference{Served: Equities, Order: []types.IdentifierType{types.IdentifierTypeCusip, types.IdentifierTypeMicTicker}, Lists: us, Mics: mics}
	complete := Preference{Order: []types.IdentifierType{types.IdentifierTypeIsin, types.IdentifierTypeCusip, types.IdentifierTypeMicTicker}, Mics: mics}
	option := gen.AssetClassOption
	tests := []struct {
		name  string
		p     Preference
		key   gen.StatedKey
		want  types.Identifier
		fails bool
	}{
		{name: "the first type of the order the key states", p: complete, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("", "AAPL"), cusip, isin}}, want: isin},
		{name: "a type outside the order is passed over", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{isin, cusip}}, want: cusip},
		{name: "a venue ticker at its operating MIC", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("XNGS", "AAPL")}}, want: ticker("XNAS", "AAPL")},
		{name: "a limited integration declines a venue outside its table whatever else is stated", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{cusip, ticker("XLON", "AAPL")}}, fails: true},
		{name: "a complete integration skips a venue that is not a MIC", p: complete, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("NASDAQ", "AAPL"), ticker("XNGS", "AAPL")}}, want: ticker("XNAS", "AAPL")},
		{name: "a venue that is not a MIC alone", p: complete, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("NASDAQ", "AAPL")}}, fails: true},
		{name: "a bare ticker last", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("", "AAPL")}}, want: ticker("", "AAPL")},
		{name: "a bare ticker beside a GUID the integration does not take", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("", "AAPL"), isin}}, fails: true},
		{name: "a class outside those served", p: limited, key: gen.StatedKey{AssetClass: &option, Identifiers: []types.Identifier{cusip}}, fails: true},
		{name: "nothing it accepts", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{isin}}, fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := tc.p.Serves(tc.key)
			if tc.fails {
				if err == nil {
					t.Errorf("Serves() = %v, want a refusal", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Serves() error = %v", err)
			}
			if got != tc.want {
				t.Errorf("Serves() = %v, want %v", got, tc.want)
			}
		})
	}
}
