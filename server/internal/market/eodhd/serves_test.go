package eodhd

import (
	"testing"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func TestServes(t *testing.T) {
	c := client(t)
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	cusip := types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"}
	figi := types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000B9XRY4"}
	shareClass := types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S5N8V8"}
	openfigi := func(code, value string) types.Identifier {
		return types.Identifier{Type: types.IdentifierTypeOpenfigiTicker, Domain: code, Value: value}
	}
	tests := []struct {
		name  string
		class *gen.AssetClass
		ids   []types.Identifier
		want  types.Identifier
		fails bool
	}{
		{name: "an ISIN before a CUSIP", ids: []types.Identifier{cusip, isin}, want: isin},
		{name: "a CUSIP before a composite FIGI", ids: []types.Identifier{figi, cusip}, want: cusip},
		{name: "a composite FIGI before a ticker", ids: []types.Identifier{ticker("XNAS", "AAPL"), figi}, want: figi},
		{name: "a segment ticker at its operating MIC", ids: []types.Identifier{ticker("XNGS", "AAPL")}, want: ticker("XNAS", "AAPL")},
		{name: "a ticker at a US venue named only by the venue table", ids: []types.Identifier{ticker("BATS", "AAAU")}, want: ticker("BATS", "AAAU")},
		{name: "a ticker outside the US", ids: []types.Identifier{ticker("XLON", "VOD")}, want: ticker("XLON", "VOD")},
		{name: "a venue ticker before an OpenFIGI one", ids: []types.Identifier{openfigi("US", "AAPL"), ticker("XNYS", "AAPL")}, want: ticker("XNYS", "AAPL")},
		{name: "an OpenFIGI ticker at one venue", ids: []types.Identifier{openfigi("LN", "VOD")}, want: ticker("XLON", "VOD")},
		{name: "an OpenFIGI ticker under the US composite", ids: []types.Identifier{ticker("", "AAPL"), openfigi("US", "AAPL")}, want: openfigi("US", "AAPL")},
		{name: "an OpenFIGI ticker under another composite", ids: []types.Identifier{openfigi("GR", "SAP")}, fails: true},
		{name: "a bare ticker", ids: []types.Identifier{ticker("", "AAPL")}, want: ticker("", "AAPL")},
		{name: "a ticker at an OTC venue", ids: []types.Identifier{ticker("OTCM", "TCEHY")}, fails: true},
		{name: "an ISIN beside a ticker at a venue EODHD does not list", ids: []types.Identifier{isin, ticker("XXXX", "AAPL")}, fails: true},
		{name: "a share class FIGI alone", ids: []types.Identifier{shareClass}, fails: true},
		{name: "a class above those served", class: ptr.To(gen.AssetClassEquity), ids: []types.Identifier{isin}, want: isin},
		{name: "a class EODHD does not serve", class: ptr.To(gen.AssetClassOption), ids: []types.Identifier{isin}, fails: true},
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
