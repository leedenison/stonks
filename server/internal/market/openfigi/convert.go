package openfigi

import (
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// identity converts the listings OpenFIGI returned for the identifier sent,
// each one a Listing. A MIC_TICKER is filtered on its ticker alone,
// so the listings at every venue are returned and resolution chooses among
// them by the stated venue.
func identity(sent types.Identifier, currency string, data []result, mics mic.Table) market.IdentityResult {
	out := market.Answer(sent, false)
	for _, r := range data {
		c := toListing(r, mics)
		c.Currency = currency
		out.Listings = append(out.Listings, c)
	}
	return out
}

// toListing converts one listing, skipping the fields OpenFIGI left null.
func toListing(r result, mics mic.Table) market.Listing {
	c := market.Listing{Class: classify(r.SecurityType, r.SecurityType2, r.MarketSector)}
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
	if t, ok := market.WithClassSep(r.Ticker, '.'); ok {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: m, Value: t})
	}
	return c
}

// venue returns the operating MIC an OpenFIGI exchange code names. It
// reports false for a code spanning several venues, such as a country's
// composite, and for a MIC that mics does not list.
func venue(code string, mics mic.Table) (string, bool) {
	m := codes[code]
	if len(m) != 1 {
		return "", false
	}
	return mics.Operating(m[0])
}
