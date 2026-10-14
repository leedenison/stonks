package types_test

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/types"
)

func TestIsGlobal(t *testing.T) {
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
		if got := tc.id.IsGlobal(); got != tc.want {
			t.Errorf("%+v.IsGlobal() = %v, want %v", tc.id, got, tc.want)
		}
	}
}

// TestStrength checks that Strength ranks every party's identifiers before an
// issuer's, stable before reassignable, and an instrument's before a
// listing's.
func TestStrength(t *testing.T) {
	want := []types.Identifier{
		{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"},
		{Type: types.IdentifierTypeSedol, Value: "BH4HKS3"},
		{Type: types.IdentifierTypeOcc, Value: "VOD   260116C00010000"},
		{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"},
		{Type: types.IdentifierTypeBrokerID, Domain: "ibkr", Value: "12345"},
		{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "VODAFONE GROUP PLC"},
		{Type: types.IdentifierTypeDatasourceTicker, Domain: "alpha", Value: "VOD.L"},
	}
	for i := 1; i < len(want); i++ {
		if a, b := want[i-1].Strength(), want[i].Strength(); a >= b {
			t.Errorf("%s.Strength() = %d, %s.Strength() = %d, want the first stronger", want[i-1].Name(), a, want[i].Name(), b)
		}
	}
}
