package massive

import (
	"strings"
	"unicode"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
)

// record is one ticker as Massive describes it.
type record struct {
	Ticker          string `json:"ticker"`
	Market          string `json:"market"`
	PrimaryExchange string `json:"primary_exchange"`
	Type            string `json:"type"`
	CurrencyName    string `json:"currency_name"`
	CompositeFIGI   string `json:"composite_figi"`
	ShareClassFIGI  string `json:"share_class_figi"`
}

// classes maps each stock type code this integration converts to the class
// of its candidate.
var classes = map[string]gen.AssetClass{
	"CS":     gen.AssetClassStock,
	"PFD":    gen.AssetClassStock,
	"OS":     gen.AssetClassStock,
	"ADRC":   gen.AssetClassStock,
	"ADRP":   gen.AssetClassStock,
	"ADRR":   gen.AssetClassStock,
	"GDR":    gen.AssetClassStock,
	"NYRS":   gen.AssetClassStock,
	"ETF":    gen.AssetClassEtf,
	"ETN":    gen.AssetClassEtf,
	"ETV":    gen.AssetClassEtf,
	"ETS":    gen.AssetClassEtf,
	"FUND":   gen.AssetClassMutualFund,
	"BASKET": gen.AssetClassMutualFund,
}

// identity converts the records Massive returned for the identifier sent.
// A record names one listing, at the primary exchange, so the answer is
// limited. A ticker is filtered on without its venue, and resolution
// compares the listing with the stated venue. Massive never returns the
// CUSIP it was sent.
func identity(sent types.Identifier, recs []record, mics mic.Table) market.IdentityResult {
	filtered := sent
	if filtered.Type == types.IdentifierTypeMicTicker {
		filtered.Domain = ""
	}
	out := market.IdentityResult{Filtered: []types.Identifier{filtered}, Limited: true}
	for _, r := range recs {
		if c, ok := candidate(r, mics); ok {
			out.Candidates = append(out.Candidates, c)
		}
	}
	return out
}

// candidate converts one record, reporting false for a record of a market
// or type this integration does not serve.
func candidate(r record, mics mic.Table) (market.Candidate, bool) {
	class, ok := classes[r.Type]
	if !ok || r.Market != "stocks" {
		return market.Candidate{}, false
	}
	c := market.Candidate{Class: class, Currency: strings.ToUpper(r.CurrencyName)}
	if r.ShareClassFIGI != "" {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: r.ShareClassFIGI})
	}
	if r.CompositeFIGI != "" {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: r.CompositeFIGI})
	}
	op, ok := mics.Operating(r.PrimaryExchange)
	if ok && r.Ticker != "" && !preferred(r.Ticker) {
		c.Identifiers = append(c.Identifiers, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: op, Value: r.Ticker})
	}
	return c, true
}

// preferred reports whether ticker is a preferred share, which Massive
// spells with a lowercase p before the series, as in ABRpD. Other sources
// spell it differently, so a MIC_TICKER in Massive's spelling would
// contradict theirs.
func preferred(ticker string) bool {
	return strings.ContainsFunc(ticker, unicode.IsLower)
}
