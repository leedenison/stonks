package market

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// TestCacheKey pins the key format, which the e2e suite holds as a contract.
func TestCacheKey(t *testing.T) {
	tests := []struct {
		name   string
		sent   types.Identifier
		params []string
		want   string
	}{
		{
			name: "a global identifier",
			sent: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"},
			want: "stonks:fetch:openfigi:identity:isin::US0378331005",
		},
		{
			name: "a venue ticker",
			sent: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"},
			want: "stonks:fetch:openfigi:identity:mic_ticker:XNAS:AAPL",
		},
		{
			name:   "a bare ticker with a currency filter",
			sent:   types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "INTC"},
			params: []string{"USD"},
			want:   "stonks:fetch:openfigi:identity:mic_ticker::INTC:USD",
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := cacheKey("openfigi", gen.FetchKindIdentity, tc.sent, tc.params); got != tc.want {
				t.Errorf("cacheKey() = %q, want %q", got, tc.want)
			}
		})
	}
	if got, want := dropPrefix("openfigi"), "stonks:fetch:openfigi:"; got != want {
		t.Errorf("dropPrefix() = %q, want %q", got, want)
	}
}
