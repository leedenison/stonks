package massive

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func TestServes(t *testing.T) {
	c := &Client{mics: mics, venues: operating(mics)}
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	composite := types.Identifier{Type: types.IdentifierTypeOpenfigiTicker, Domain: "US", Value: "AAPL"}
	tests := []struct {
		name  string
		class *gen.AssetClass
		ids   []types.Identifier
		want  types.Identifier
		fails bool
	}{
		{name: "a CUSIP before a ticker", ids: []types.Identifier{ticker("XNAS", "AAPL"), cusip}, want: cusip},
		{name: "a segment ticker at its operating MIC", ids: []types.Identifier{ticker("XNGS", "AAPL")}, want: ticker("XNAS", "AAPL")},
		{name: "a venue ticker before a composite one", ids: []types.Identifier{composite, ticker("XNYS", "AAPL")}, want: ticker("XNYS", "AAPL")},
		{name: "a composite ticker before a bare one", ids: []types.Identifier{ticker("", "AAPL"), composite}, want: composite},
		{name: "a bare ticker", ids: []types.Identifier{ticker("", "AAPL")}, want: ticker("", "AAPL")},
		{name: "a ticker at a venue Massive does not list", ids: []types.Identifier{ticker("XLON", "VOD")}, fails: true},
		{name: "a CUSIP beside a ticker at a venue Massive does not list", ids: []types.Identifier{cusip, ticker("XLON", "AAPL")}, fails: true},
		{name: "a ticker under another composite", ids: []types.Identifier{{Type: types.IdentifierTypeOpenfigiTicker, Domain: "LN", Value: "VOD"}}, fails: true},
		{name: "an ISIN alone", ids: []types.Identifier{isin}, fails: true},
		{name: "a class above those served", class: ptr.To(gen.AssetClassEquity), ids: []types.Identifier{cusip}, want: cusip},
		{name: "a class Massive does not serve", class: ptr.To(gen.AssetClassOption), ids: []types.Identifier{cusip}, fails: true},
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
