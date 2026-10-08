package market

import (
	"testing"

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
