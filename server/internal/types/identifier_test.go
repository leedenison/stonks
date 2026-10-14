package types_test

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/types"
)

func TestName(t *testing.T) {
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"}
	if got := isin.Name(); got != "isin GB00BH4HKS39" {
		t.Errorf("isin.Name() = %q", got)
	}
	venue := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	if got := venue.Name(); got != "mic_ticker XLON:VOD" {
		t.Errorf("mic_ticker.Name() = %q", got)
	}
}
