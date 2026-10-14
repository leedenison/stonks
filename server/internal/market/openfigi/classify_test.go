package openfigi

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/types"
)

func TestClassify(t *testing.T) {
	tests := []struct {
		name                      string
		secType, secType2, sector string
		want                      types.AssetClass
	}{
		{"common stock", "Common Stock", "Common Stock", "Equity", types.AssetClassStock},
		{"equity option", "Equity Option", "Option", "Equity", types.AssetClassOption},
		{"equity option with an equity securityType2", "Equity Option", "Equity", "Equity", types.AssetClassOption},
		{"currency option", "Currency Option", "Option", "Curncy", types.AssetClassOption},
		{"option on equity future", "Option on Equity Future", "Option", "Equity", types.AssetClassOption},
		{"single stock future", "SINGLE STOCK FUTURE", "Future", "Equity", types.AssetClassFuture},
		{"future by securityType2", "Some Type", "Future", "Equity", types.AssetClassFuture},
		{"etp", "ETP", "ETP", "Equity", types.AssetClassEtf},
		{"etp filed as a mutual fund", "ETP", "Mutual Fund", "Equity", types.AssetClassEtf},
		{"corporate bond", "Bond", "Corp", "Corp", types.AssetClassFixedIncome},
		{"mortgage pool", "ABS Auto", "Pool", "Mtge", types.AssetClassFixedIncome},
		{"corp sector", "Some Type", "Some Type2", "Corp", types.AssetClassFixedIncome},
		{"open-end fund", "Open-End Fund", "Fund", "Equity", types.AssetClassMutualFund},
		{"fund by securityType2", "Some Type", "Fund", "Equity", types.AssetClassMutualFund},
		{"equity sector alone", "Some Type", "Some Type2", "Equity", types.AssetClassSecurity},
		{"index warrant in the equity sector", "Index WRT", "Warrant", "Equity", types.AssetClassSecurity},
		{"index warrant elsewhere", "Index WRT", "Warrant", "Other", types.AssetClassUnknown},
		{"cash", "CASH", "", "", types.AssetClassCash},
		{"currency spot", "Currency spot", "Spot", "Curncy", types.AssetClassUnknown},
		{"curncy sector alone", "Some Type", "Some Type2", "Curncy", types.AssetClassUnknown},
		{"lower case", "common stock", "common stock", "equity", types.AssetClassStock},
		{"upper case", "COMMON STOCK", "COMMON STOCK", "EQUITY", types.AssetClassStock},
		{"padded", " ETP ", " ETP ", " Equity ", types.AssetClassEtf},
		{"securityType alone", "Common Stock", "", "", types.AssetClassStock},
		{"empty", "", "", "", types.AssetClassUnknown},
		{"unrecognised", "Something Exotic", "Weird", "Mars", types.AssetClassUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := classify(tc.secType, tc.secType2, tc.sector); got != tc.want {
				t.Errorf("classify(%q, %q, %q) = %s, want %s", tc.secType, tc.secType2, tc.sector, got, tc.want)
			}
		})
	}
}
