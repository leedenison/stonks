package types_test

import (
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/types"
)

func TestInstrumentAllIDs(t *testing.T) {
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}
	xlon := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	xnas := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "VOD"}
	bare := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "VOD"}
	i := types.Instrument{
		Identifiers: []types.Identifier{isin},
		Listings: map[string]types.Listing{
			"USD": {Identifiers: []types.Identifier{xnas}},
			"GBP": {Identifiers: []types.Identifier{xlon}},
		},
	}
	if diff := cmp.Diff([]types.Identifier{isin, xlon, xnas}, slices.Collect(i.AllIDs())); diff != "" {
		t.Errorf("AllIDs() mismatch (-want +got):\n%s", diff)
	}
	if !i.IdentifiedBy(xnas) {
		t.Errorf("IdentifiedBy(%v) = false, want true", xnas)
	}
	if i.IdentifiedBy(bare) {
		t.Errorf("IdentifiedBy(%v) = true, want false", bare)
	}
}
