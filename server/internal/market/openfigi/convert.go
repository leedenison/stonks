package openfigi

import (
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
)

// identity converts the listings OpenFIGI returned for the identifier sent,
// each listing one candidate. A MIC_TICKER is filtered on its ticker alone,
// so the listings at every venue are returned and resolution chooses among
// them by the stated venue.
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
	m, ok := market.OpenFIGIVenue(r.ExchCode, mics)
	if !ok {
		return c
	}
	if t, ok := market.WithClassSep(r.Ticker, '.'); ok {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: m, Value: t})
	}
	return c
}
