package massive

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func TestServes(t *testing.T) {
	c, err := New(market.Config{Credential: "k", JSON: json.RawMessage(conf)}, mics, http.DefaultClient)
	require.NoError(t, err)
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	tests := []struct {
		name  string
		class *types.AssetClass
		ids   []types.Identifier
		want  types.Identifier
		fails bool
	}{
		{name: "a CUSIP before a ticker", ids: []types.Identifier{ticker("XNAS", "AAPL"), cusip}, want: cusip},
		{name: "a segment ticker at its operating MIC", ids: []types.Identifier{ticker("XNGS", "AAPL")}, want: ticker("XNAS", "AAPL")},
		{name: "a bare ticker", ids: []types.Identifier{ticker("", "AAPL")}, want: ticker("", "AAPL")},
		{name: "a ticker at a venue Massive does not list", ids: []types.Identifier{ticker("XLON", "VOD")}, fails: true},
		{name: "a CUSIP beside a ticker at a venue Massive does not list", ids: []types.Identifier{cusip, ticker("XLON", "AAPL")}, fails: true},
		{name: "an ISIN alone", ids: []types.Identifier{isin}, fails: true},
		{name: "a bare ticker beside an ISIN", ids: []types.Identifier{ticker("", "AAPL"), isin}, fails: true},
		{name: "a class above those served", class: ptr.To(types.AssetClassEquity), ids: []types.Identifier{cusip}, want: cusip},
		{name: "a class Massive does not serve", class: ptr.To(types.AssetClassOption), ids: []types.Identifier{cusip}, fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := c.Serves(gen.StatedKey{AssetClass: tc.class, Identifiers: tc.ids})
			if tc.fails {
				if err == nil {
					t.Errorf("Serves() = %v, want a refusal", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("Serves() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Serves() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
