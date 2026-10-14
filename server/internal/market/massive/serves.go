package massive

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Serves returns the one identifier of key that Massive accepts: a CUSIP,
// then a ticker at a US venue, then a ticker without its venue. A venue
// ticker comes back at its operating MIC. Massive rejects an ISIN, SEDOL,
// CINS, Wertpapier and FIGI. A key that states a ticker at a venue outside
// Massive's table is declined whatever else it states, since Massive would
// answer with the US listing.
func (c *Client) Serves(key gen.StatedKey) (types.Identifier, error) {
	return c.prefer.Serves(key)
}
