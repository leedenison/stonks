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

// TestTrusted checks that trusted returns every GUID and every broker
// description, strongest first, and nothing else.
func TestTrusted(t *testing.T) {
	isin := id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	venue := id(types.IdentifierTypeMicTicker, "XLON", "VOD")
	ticker := id(types.IdentifierTypeMicTicker, "", "VOD")
	broker := id(types.IdentifierTypeBrokerID, "ibkr", "12345")
	a := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GROUP PLC")
	b := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GRP")
	k := gen.StatedKey{Identifiers: []types.Identifier{a, venue, ticker, broker, b, isin}}
	want := []types.Identifier{isin, venue, a, b}
	if diff := cmp.Diff(want, trusted(k)); diff != "" {
		t.Errorf("trusted mismatch (-want +got):\n%s", diff)
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

// TestStrength holds the order identifiers are tried in: every party's
// identifiers before an issuer's, stable before reassignable, and an
// instrument's before a listing's.
func TestStrength(t *testing.T) {
	want := []types.Identifier{
		id(types.IdentifierTypeIsin, "", "GB00BH4HKS39"),
		id(types.IdentifierTypeSedol, "", "BH4HKS3"),
		id(types.IdentifierTypeOcc, "", "VOD   260116C00010000"),
		id(types.IdentifierTypeMicTicker, "XLON", "VOD"),
		id(types.IdentifierTypeBrokerID, "ibkr", "12345"),
		id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GROUP PLC"),
		id(types.IdentifierTypeDatasourceTicker, "alpha", "VOD.L"),
	}
	for i := 1; i < len(want); i++ {
		if a, b := strength(want[i-1]), strength(want[i]); a >= b {
			t.Errorf("strength(%s) = %d, strength(%s) = %d, want the first stronger", name(want[i-1]), a, name(want[i]), b)
		}
	}
}

// TestExclusive checks that a second value of an exclusive type contradicts
// the first, and a second description of one broker does not.
func TestExclusive(t *testing.T) {
	isin := id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	other := id(types.IdentifierTypeIsin, "", "US0378331005")
	descr := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GROUP PLC")
	renamed := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GRP")
	g := &group{instrument: []types.Identifier{isin, descr}, listings: map[string][]types.Identifier{}}
	if got, ok := g.contradicts(other, ""); !ok || got != isin {
		t.Errorf("contradicts(%s) = %v, %v, want the isin", name(other), got, ok)
	}
	if got, ok := g.contradicts(renamed, ""); ok {
		t.Errorf("contradicts(%s) = %v, want no contradiction between two descriptions", name(renamed), got)
	}
}
