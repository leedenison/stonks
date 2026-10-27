package eodhd

import (
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// domain is the namespace of the DATASOURCE_TICKER that a splits fetch sends,
// EODHD's symbol for a ticker.
const domain = "eodhd"

// preferred is the type of a preferred share. Sources spell its ticker
// differently, so a MIC_TICKER in EODHD's spelling would contradict theirs.
const preferred = "Preferred Stock"

// classes maps each type this integration converts to the class of its
// listing.
var classes = map[string]types.AssetClass{
	"Common Stock": types.AssetClassStock,
	preferred:      types.AssetClassStock,
	"ETF":          types.AssetClassEtf,
	"FUND":         types.AssetClassMutualFund,
	"Mutual Fund":  types.AssetClassMutualFund,
}

// identity converts what EODHD returned for the identifier sent. EODHD's
// search lists active tickers on its own exchanges only, so the answer is
// limited. The filter is a ticker without its venue, and resolution
// compares each listing with the stated venue.
func (c *Client) identity(sent types.Identifier, a answer) market.IdentityResult {
	out := market.Answer(sent, true)
	for _, r := range a.rows {
		if l, ok := c.toListing(r, a.mappings[r.symbol()], a.venues[r.Code]); ok {
			out.Listings = append(out.Listings, l)
		}
	}
	return out
}

// toListing converts one row. It reports false when the row's type or venue
// is not one this integration serves, and when no identifier is left. m and
// usVenue carry what id-mapping and the exchange symbol list say about the
// row.
//
// EODHD's code becomes a MIC_TICKER at the row's operating MIC. toListing
// omits the ticker of a preferred share. It also omits the ticker of a row at
// a venue that several EODHD codes list, because the code does not name the
// segment where the security trades.
func (c *Client) toListing(r row, m mapping, usVenue string) (market.Listing, bool) {
	class, ok := classes[r.Type]
	if !ok {
		return market.Listing{}, false
	}
	venue, ok := venueOf(r, usVenue)
	if !ok {
		return market.Listing{}, false
	}
	op, ok := c.mics.Operating(venue)
	if !ok {
		return market.Listing{}, false
	}
	l := market.Listing{Class: class, Currency: r.Currency}
	if r.IsPrimary {
		l.Primary = op
	}
	add := func(t types.IdentifierType, v string) {
		if v != "" {
			l.Identifiers = append(l.Identifiers, types.Identifier{Type: t, Value: v})
		}
	}
	add(types.IdentifierTypeIsin, r.ISIN)
	add(types.IdentifierTypeCusip, m.CUSIP)
	if ticker, ok := market.WithClassSep(r.Code, '.'); ok && r.Type != preferred && len(c.exchange[op]) == 1 {
		l.Identifiers = append(l.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: op, Value: ticker})
	}
	if len(l.Identifiers) == 0 {
		return market.Listing{}, false
	}
	return l, true
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
