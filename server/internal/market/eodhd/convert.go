package eodhd

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// domain is the namespace of EODHD's symbols as a DATASOURCE_TICKER.
const domain = "eodhd"

// preferred is the type of a preferred share. Sources spell its ticker
// differently, so a MIC_TICKER in EODHD's spelling would contradict theirs.
const preferred = "Preferred Stock"

// classes maps each type this integration converts to the class of its
// candidate.
var classes = map[string]gen.AssetClass{
	"Common Stock": gen.AssetClassStock,
	preferred:      gen.AssetClassStock,
	"ETF":          gen.AssetClassEtf,
	"FUND":         gen.AssetClassMutualFund,
	"Mutual Fund":  gen.AssetClassMutualFund,
}

// identity converts what EODHD returned for the identifier sent. EODHD's
// search lists active tickers on its own exchanges only, so the answer is
// limited. The filter is a ticker without its venue, and resolution
// compares each listing with the stated venue.
func (c *Client) identity(sent types.Identifier, a answer) market.IdentityResult {
	filtered := sent
	if filtered.Type == types.IdentifierTypeMicTicker {
		filtered.Domain = ""
	}
	out := market.IdentityResult{Filtered: []types.Identifier{filtered}, Limited: true}
	for _, r := range a.rows {
		if cand, ok := c.candidate(r, a.mappings[r.symbol()], a.venues[r.Code]); ok {
			out.Candidates = append(out.Candidates, cand)
		}
	}
	return out
}

// candidate converts one row. It reports false when the row's type or venue
// is not one this integration serves. m and usVenue carry what id-mapping
// and the exchange symbol list say about the row.
func (c *Client) candidate(r row, m mapping, usVenue string) (market.Candidate, bool) {
	class, ok := classes[r.Type]
	if !ok {
		return market.Candidate{}, false
	}
	venue, ok := venueOf(r, usVenue)
	if !ok {
		return market.Candidate{}, false
	}
	op, ok := c.mics.Operating(venue)
	if !ok {
		return market.Candidate{}, false
	}
	cand := market.Candidate{Class: class, Currency: r.Currency, Identifiers: []types.Identifier{
		{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: r.symbol()},
	}}
	if r.IsPrimary {
		cand.Primary = op
	}
	add := func(t types.IdentifierType, v string) {
		if v != "" {
			cand.Identifiers = append(cand.Identifiers, types.Identifier{Type: t, Value: v})
		}
	}
	add(types.IdentifierTypeIsin, r.ISIN)
	add(types.IdentifierTypeCusip, m.CUSIP)
	if ticker, ok := market.WithClassSep(r.Code, '.'); ok && r.Type != preferred {
		cand.Identifiers = append(cand.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: op, Value: ticker})
	}
	return cand, true
}

// venueOf returns the MIC of the venue a row trades on. EODHD's code US
// spans every US venue, so a US row's venue comes from the name the exchange
// symbol list gives it.
func venueOf(r row, usVenue string) (string, bool) {
	if r.Exchange == usCode {
		m, ok := usVenues[usVenue]
		return m, ok
	}
	if ms := codes[r.Exchange]; len(ms) == 1 {
		return ms[0], true
	}
	return "", false
}
