package eodhd

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Serves returns the one identifier of key to send to EODHD: an ISIN, a
// CUSIP, a composite FIGI, a venue ticker or a bare ticker, in that order of
// preference. A venue ticker comes back at its operating MIC. Serves declines
// a key that states a ticker at a venue outside EODHD's exchanges, whatever
// else the key states, because EODHD would answer with listings elsewhere.
func (c *Client) Serves(key gen.StatedKey) (types.Identifier, error) {
	return c.prefer.Serves(key)
}
