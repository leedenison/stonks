package types

import typev1 "github.com/leedenison/stonks/proto/type/v1"

// MarketTicker is the key of a ticker's identifier facts: a market and a
// ticker's value. A MIC_TICKER names a listing at a venue, and its facts are
// read and written at the market of that venue.
type MarketTicker struct {
	Market string
	Value  string
}

// Name returns t in the form MARKET:VALUE.
func (t MarketTicker) Name() string { return t.Market + ":" + t.Value }

// ToProto converts t.
func (t MarketTicker) ToProto() *typev1.MarketTicker {
	return &typev1.MarketTicker{Market: t.Market, Value: t.Value}
}

// MarketTickerOf converts p.
func MarketTickerOf(p *typev1.MarketTicker) MarketTicker {
	return MarketTicker{Market: p.GetMarket(), Value: p.GetValue()}
}
