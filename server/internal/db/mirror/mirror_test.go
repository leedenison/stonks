package mirror

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
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

func TestDisjoint(t *testing.T) {
	tests := []struct {
		a, b gen.AssetClass
		want bool
	}{
		{gen.AssetClassStock, gen.AssetClassEquity, false},
		{gen.AssetClassEquity, gen.AssetClassStock, false},
		{gen.AssetClassUnknown, gen.AssetClassOption, false},
		{gen.AssetClassStock, gen.AssetClassStock, false},
		{gen.AssetClassStock, gen.AssetClassEtf, true},
		{gen.AssetClassOption, gen.AssetClassEquity, true},
		{gen.AssetClassCash, gen.AssetClassSecurity, true},
	}
	for _, tc := range tests {
		if got := Disjoint(tc.a, tc.b); got != tc.want {
			t.Errorf("Disjoint(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
