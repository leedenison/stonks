package resolve

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// WrittenIdentifierFacts pairs a run with the tickers whose identifier facts
// it wrote, and the markets whose assertion coverage it wrote.
type WrittenIdentifierFacts struct {
	Run     gen.Run
	Tickers []types.MarketTicker
	Markets []string
}
