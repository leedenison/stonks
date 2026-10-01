package resolve

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

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

func TestGUIDs(t *testing.T) {
	isin := id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	sedol := id(types.IdentifierTypeSedol, "", "BH4HKS3")
	occ := id(types.IdentifierTypeOcc, "", "VOD   260116C00010000")
	venue := id(types.IdentifierTypeMicTicker, "XLON", "VOD")
	ticker := id(types.IdentifierTypeMicTicker, "", "VOD")
	broker := id(types.IdentifierTypeBrokerID, "ibkr", "12345")
	tests := []struct {
		name   string
		stated []types.Identifier
		want   []types.Identifier
		bare   bool
	}{
		{name: "strongest first", stated: []types.Identifier{venue, occ, sedol, isin}, want: []types.Identifier{isin, sedol, occ, venue}},
		{name: "a ticker needs its venue", stated: []types.Identifier{ticker, broker}, want: nil, bare: true},
		{name: "a venue ticker beside a bare one", stated: []types.Identifier{ticker, venue}, want: []types.Identifier{venue}, bare: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			k := gen.StatedKey{Identifiers: tc.stated}
			if diff := cmp.Diff(tc.want, guids(k)); diff != "" {
				t.Errorf("guids mismatch (-want +got):\n%s", diff)
			}
			if got := bare(k); got != tc.bare {
				t.Errorf("bare = %v, want %v", got, tc.bare)
			}
		})
	}
}

func TestFamily(t *testing.T) {
	if got := family(gen.StatedKey{Currency: ptr.To("GBX")}, families); got != "GBP" {
		t.Errorf("family(GBX) = %q, want GBP", got)
	}
	if got := family(gen.StatedKey{}, families); got != "" {
		t.Errorf("family(none) = %q, want none", got)
	}
}

func TestName(t *testing.T) {
	if got := name(id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")); got != "isin GB00BH4HKS39" {
		t.Errorf("name(isin) = %q", got)
	}
	if got := name(id(types.IdentifierTypeMicTicker, "XLON", "VOD")); got != "mic_ticker XLON:VOD" {
		t.Errorf("name(mic_ticker) = %q", got)
	}
}
