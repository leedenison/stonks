package types

// MarketTicker is the key of a ticker's identifier facts: a market and a
// ticker's value. A MIC_TICKER names a listing at a venue, and its facts are
// read and written at the market of that venue.
type MarketTicker struct {
	Market string
	Value  string
}

// Name returns t in the form MARKET:VALUE.
func (t MarketTicker) Name() string { return t.Market + ":" + t.Value }
