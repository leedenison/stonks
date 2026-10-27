package resolve

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

func TestGlobalIDs(t *testing.T) {
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
			if diff := cmp.Diff(tc.want, globalAndDescriptionIDs(k)); diff != "" {
				t.Errorf("globalAndDescriptionIDs mismatch (-want +got):\n%s", diff)
			}
			if got := statesBareTicker(k); got != tc.bare {
				t.Errorf("bare = %v, want %v", got, tc.bare)
			}
		})
	}
}

// TestGlobalAndDescriptionIDs checks that globalAndDescriptionIDs returns
// every global identifier and every broker description, strongest first, and
// nothing else.
func TestGlobalAndDescriptionIDs(t *testing.T) {
	isin := id(types.IdentifierTypeIsin, "", "GB00BH4HKS39")
	venue := id(types.IdentifierTypeMicTicker, "XLON", "VOD")
	ticker := id(types.IdentifierTypeMicTicker, "", "VOD")
	broker := id(types.IdentifierTypeBrokerID, "ibkr", "12345")
	a := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GROUP PLC")
	b := id(types.IdentifierTypeBrokerDescription, "ibkr", "VODAFONE GRP")
	k := gen.StatedKey{Identifiers: []types.Identifier{a, venue, ticker, broker, b, isin}}
	want := []types.Identifier{isin, venue, a, b}
	if diff := cmp.Diff(want, globalAndDescriptionIDs(k)); diff != "" {
		t.Errorf("globalAndDescriptionIDs mismatch (-want +got):\n%s", diff)
	}
}
