package openfigi

import (
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// rule matches a result whose fields each lie in the rule's set, where a nil
// set matches anything.
type rule struct {
	class          gen.AssetClass
	securityTypes  map[string]bool
	securityType2s map[string]bool
	marketSectors  map[string]bool
}

func set(vals ...string) map[string]bool {
	m := make(map[string]bool, len(vals))
	for _, v := range vals {
		m[strings.ToLower(v)] = true
	}
	return m
}

// rules are tried in order, the first match deciding. They run from what
// OpenFIGI named, the securityType, to what it implied, the market sector,
// which OpenFIGI's guidance reads only where securityType2 is absent.
//
// An FX answer matches no rule and is unknown: a currency pair, swap or
// forward is not a currency held.
var rules = []rule{
	{
		class: gen.AssetClassOption,
		securityTypes: set(
			"Equity Option", "Index Option", "Currency Option",
			"Physical index option", "Option on Equity Future",
			"OPTION", "OPTION VOLATILITY",
		),
	},
	{class: gen.AssetClassOption, securityType2s: set("Option")},
	{
		class: gen.AssetClassFuture,
		securityTypes: set(
			"SINGLE STOCK FUTURE", "SINGLE STOCK DIVIDEND FUTURE",
			"SINGLE STOCK FUTURE SPREAD", "DIVIDEND NEUTRAL STOCK FUTURE",
			"Financial commodity future", "Physical commodity future",
			"Financial commodity forward", "Physical commodity forward",
			"NON-DELIVERABLE FORWARD", "ONSHORE FORWARD",
		),
	},
	{class: gen.AssetClassFuture, securityType2s: set("Future")},
	{class: gen.AssetClassEtf, securityTypes: set("ETP")},
	{
		class: gen.AssetClassFixedIncome,
		securityTypes: set(
			"Bond", "MED TERM NOTE", "EURO MTN", "MEDIUM TERM CD",
			"COMMERCIAL PAPER", "EURO CP",
			"BANKERS ACCEPT", "BANKERS ACCEPTANCE",
			"DISCOUNT NOTES", "DEPOSIT NOTE", "BEARER DEP NOTE",
			"REPO", "FED FUNDS", "T-BILL", "PROV T-BILL", "MONETARY BILLS",
		),
	},
	{class: gen.AssetClassFixedIncome, securityType2s: set("Corp", "Pool")},
	{class: gen.AssetClassFixedIncome, marketSectors: set("Corp", "Govt", "Muni", "Mtge", "M-Mkt")},
	{
		class: gen.AssetClassMutualFund,
		securityTypes: set(
			"Open-End Fund", "Mutual Fund", "Closed-End Fund",
			"Unit Trust", "Savings Plan", "Savings Share",
			"Managed Account", "Pvt Eqty Fund", "MLP", "Ltd Part",
		),
	},
	{class: gen.AssetClassMutualFund, securityType2s: set("Fund")},
	{
		class: gen.AssetClassStock,
		securityTypes: set(
			"Common Stock", "Preference", "Preferred", "Pfd WRT",
			"ADR", "GDR", "BDR", "EDR", "NVDR", "SDR",
			"NY Reg Shrs", "Dutch Cert", "Austrian Crt",
			"Belgian Cert", "Participate Cert",
			"Depositary Receipt", "Receipt",
			"Stapled Security", "Right", "REIT",
			"Contract For Difference",
		),
	},
	{class: gen.AssetClassStock, securityType2s: set("Common Stock")},
	// The Equity sector holds options, futures, warrants and rights as readily
	// as shares, so on its own it rules out only debt, currency and
	// commodity.
	{class: gen.AssetClassSecurity, marketSectors: set("Equity")},
	{class: gen.AssetClassCash, securityTypes: set("CASH")},
}

// classify reads the asset class of a result.
func classify(securityType, securityType2, marketSector string) gen.AssetClass {
	st := strings.ToLower(strings.TrimSpace(securityType))
	st2 := strings.ToLower(strings.TrimSpace(securityType2))
	ms := strings.ToLower(strings.TrimSpace(marketSector))
	for _, r := range rules {
		if r.securityTypes != nil && !r.securityTypes[st] {
			continue
		}
		if r.securityType2s != nil && !r.securityType2s[st2] {
			continue
		}
		if r.marketSectors != nil && !r.marketSectors[ms] {
			continue
		}
		return r.class
	}
	return gen.AssetClassUnknown
}
