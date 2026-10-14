package market

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

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
	option := types.AssetClassOption
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
		{name: "a bare ticker beside a global identifier the integration does not take", p: limited, key: gen.StatedKey{Identifiers: []types.Identifier{ticker("", "AAPL"), isin}}, fails: true},
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
