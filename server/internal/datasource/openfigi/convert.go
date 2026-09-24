package openfigi

import (
	"strings"

	"github.com/leedenison/stonks/server/internal/datasource"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/mic"
)

// classSeps separate a ticker's root from its share class, as in BRK.B, BRK/B,
// BRK-B and "BRK B".
const classSeps = ".-/ "

// withClassSep writes ticker with every share class separator as sep.
func withClassSep(ticker string, sep rune) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(classSeps, r) {
			return sep
		}
		return r
	}, ticker)
}

// answer reads the listings OpenFIGI mapped sent to. Where sent is a
// MIC_TICKER naming a venue, OpenFIGI filtered on the ticker alone and the
// listings at other venues are dropped.
func answer(sent types.Identifier, data []result, mics mic.Table) datasource.IdentityResult {
	out := datasource.IdentityResult{Filtered: []types.Identifier{sent}}
	for _, r := range data {
		c := candidate(r, mics)
		if sent.Type == types.IdentifierTypeMicTicker && sent.Domain != "" && !atVenue(c, sent.Domain) {
			continue
		}
		out.Candidates = append(out.Candidates, c)
	}
	return out
}

func atVenue(c datasource.Candidate, venue string) bool {
	for _, id := range c.Identifiers {
		if id.Type == types.IdentifierTypeMicTicker && id.Domain == venue {
			return true
		}
	}
	return false
}

// candidate converts one listing, skipping the fields OpenFIGI left null.
func candidate(r result, mics mic.Table) datasource.Candidate {
	c := datasource.Candidate{Class: classify(r.SecurityType, r.SecurityType2, r.MarketSector)}
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
	if m, ok := venue(r.ExchCode, mics); ok {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: m, Value: withClassSep(r.Ticker, '.')})
	}
	return c
}
