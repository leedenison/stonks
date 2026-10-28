package types_test

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/types"
)

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
		if got := tc.a.Disjoint(tc.b); got != tc.want {
			t.Errorf("%s.Disjoint(%s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestRestated(t *testing.T) {
	want := map[types.AssetClass]bool{
		types.AssetClassEquity:     true,
		types.AssetClassStock:      true,
		types.AssetClassEtf:        true,
		types.AssetClassMutualFund: true,
	}
	for _, c := range types.AssetClasses {
		if got := c.Restated(); got != want[c] {
			t.Errorf("%s.Restated() = %v, want %v", c, got, want[c])
		}
	}
}
