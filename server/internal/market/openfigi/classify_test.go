package openfigi

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name                      string
		secType, secType2, sector string
		want                      gen.AssetClass
	}{
		{"common stock", "Common Stock", "Common Stock", "Equity", gen.AssetClassStock},
		{"equity option", "Equity Option", "Option", "Equity", gen.AssetClassOption},
		{"equity option with an equity securityType2", "Equity Option", "Equity", "Equity", gen.AssetClassOption},
		{"currency option", "Currency Option", "Option", "Curncy", gen.AssetClassOption},
		{"option on equity future", "Option on Equity Future", "Option", "Equity", gen.AssetClassOption},
		{"single stock future", "SINGLE STOCK FUTURE", "Future", "Equity", gen.AssetClassFuture},
		{"future by securityType2", "Some Type", "Future", "Equity", gen.AssetClassFuture},
		{"etp", "ETP", "ETP", "Equity", gen.AssetClassEtf},
		{"etp filed as a mutual fund", "ETP", "Mutual Fund", "Equity", gen.AssetClassEtf},
		{"corporate bond", "Bond", "Corp", "Corp", gen.AssetClassFixedIncome},
		{"mortgage pool", "ABS Auto", "Pool", "Mtge", gen.AssetClassFixedIncome},
		{"corp sector", "Some Type", "Some Type2", "Corp", gen.AssetClassFixedIncome},
		{"open-end fund", "Open-End Fund", "Fund", "Equity", gen.AssetClassMutualFund},
		{"fund by securityType2", "Some Type", "Fund", "Equity", gen.AssetClassMutualFund},
		{"equity sector alone", "Some Type", "Some Type2", "Equity", gen.AssetClassSecurity},
		{"index warrant in the equity sector", "Index WRT", "Warrant", "Equity", gen.AssetClassSecurity},
		{"index warrant elsewhere", "Index WRT", "Warrant", "Other", gen.AssetClassUnknown},
		{"cash", "CASH", "", "", gen.AssetClassCash},
		{"currency spot", "Currency spot", "Spot", "Curncy", gen.AssetClassUnknown},
		{"curncy sector alone", "Some Type", "Some Type2", "Curncy", gen.AssetClassUnknown},
		{"lower case", "common stock", "common stock", "equity", gen.AssetClassStock},
		{"upper case", "COMMON STOCK", "COMMON STOCK", "EQUITY", gen.AssetClassStock},
		{"padded", " ETP ", " ETP ", " Equity ", gen.AssetClassEtf},
		{"securityType alone", "Common Stock", "", "", gen.AssetClassStock},
		{"empty", "", "", "", gen.AssetClassUnknown},
		{"unrecognised", "Something Exotic", "Weird", "Mars", gen.AssetClassUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.secType, tc.secType2, tc.sector); got != tc.want {
				t.Errorf("classify(%q, %q, %q) = %s, want %s", tc.secType, tc.secType2, tc.sector, got, tc.want)
			}
		})
	}
}
