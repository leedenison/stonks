package openfigi

import (
	"strings"

	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
)

// classSeps separate a ticker's root from its share class, as in BRK.B, BRK/B,
// BRK-B and "BRK B".
const classSeps = ".-/ "

// withClassSep writes ticker with its share class separator as sep.
func withClassSep(ticker string, sep rune) (string, bool) {
	n := 0
	out := strings.Map(func(r rune) rune {
		if strings.ContainsRune(classSeps, r) {
			n++
			return sep
		}
		return r
	}, ticker)
	if n > 1 {
		return ticker, false
	}
	return out, true
}

// identity converts the listings OpenFIGI returned for the identifier sent,
// each listing one candidate. A MIC_TICKER is filtered on its ticker alone,
// so its venue is dropped from what was filtered on. Where the call filtered
// on a currency, every listing is in it.
func identity(sent types.Identifier, currency string, data []result, mics mic.Table) market.IdentityResult {
	filtered := sent
	if filtered.Type == types.IdentifierTypeMicTicker {
		filtered.Domain = ""
	}
	out := market.IdentityResult{Filtered: []types.Identifier{filtered}}
	for _, r := range data {
		c := candidate(r, mics)
		c.Currency = currency
		out.Candidates = append(out.Candidates, c)
	}
	return out
}

// candidate converts one listing, skipping the fields OpenFIGI left null.
func candidate(r result, mics mic.Table) market.Candidate {
	c := market.Candidate{Class: classify(r.SecurityType, r.SecurityType2, r.MarketSector)}
	if r.ShareClassFIGI != nil && *r.ShareClassFIGI != "" {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: *r.ShareClassFIGI})
	}
	if r.CompositeFIGI != nil && *r.CompositeFIGI != "" {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: *r.CompositeFIGI})
	}
	if r.Ticker == "" || r.ExchCode == "" {
		return c
	}
	c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeOpenfigiTicker, Domain: r.ExchCode, Value: r.Ticker})
	m, ok := venue(r.ExchCode, mics)
	if !ok {
		return c
	}
	if t, ok := withClassSep(r.Ticker, '.'); ok {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: m, Value: t})
	}
	return c
}
