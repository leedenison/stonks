package mirror

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
		if got := IsGlobal(tc.id); got != tc.want {
			t.Errorf("IsGlobal(%+v) = %v, want %v", tc.id, got, tc.want)
		}
	}
}

func TestDisjoint(t *testing.T) {
	tests := []struct {
		a, b types.AssetClass
		want bool
	}{
		{types.AssetClassStock, types.AssetClassEquity, false},
		{types.AssetClassEquity, types.AssetClassStock, false},
		{types.AssetClassUnknown, types.AssetClassOption, false},
		{types.AssetClassStock, types.AssetClassStock, false},
		{types.AssetClassStock, types.AssetClassEtf, true},
		{types.AssetClassOption, types.AssetClassEquity, true},
		{types.AssetClassCash, types.AssetClassSecurity, true},
	}
	for _, tc := range tests {
		if got := Disjoint(tc.a, tc.b); got != tc.want {
			t.Errorf("Disjoint(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
