package massive

import (
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// usComposite is OpenFIGI's exchange code for a US composite listing.
const usComposite = "US"

// served holds the stated classes Massive can answer: the classes it
// converts and the classes above them.
var served = map[gen.AssetClass]bool{
	gen.AssetClassUnknown:    true,
	gen.AssetClassSecurity:   true,
	gen.AssetClassEquity:     true,
	gen.AssetClassStock:      true,
	gen.AssetClassEtf:        true,
	gen.AssetClassMutualFund: true,
}

// Serves returns the one identifier of key that Massive accepts, preferring
// a CUSIP, then a ticker at a US venue, then a ticker under OpenFIGI's US
// composite, then a ticker without its venue. A venue ticker comes back at
// its operating MIC. Massive rejects an ISIN, SEDOL, CINS, Wertpapier and
// FIGI. A key that states a ticker at a venue outside Massive's table is
// declined whatever else it states, since Massive would answer with the US
// listing.
func (c *Client) Serves(key gen.StatedKey) (types.Identifier, error) {
	if key.AssetClass != nil && !served[*key.AssetClass] {
		return types.Identifier{}, fmt.Errorf("asset class '%s' rejected", *key.AssetClass)
	}
	var cusip, ticker, composite, bare *types.Identifier
	for _, id := range key.Identifiers {
		switch {
		case id.Type == types.IdentifierTypeCusip:
			cusip = &id
		case id.Type == types.IdentifierTypeOpenfigiTicker && id.Domain == usComposite:
			composite = &id
		case id.Type != types.IdentifierTypeMicTicker:
		case id.Domain == "":
			bare = &id
		default:
			op, ok := c.mics.Operating(id.Domain)
			if !ok || !c.venues[op] {
				return types.Identifier{}, fmt.Errorf("venue '%s' rejected", id.Domain)
			}
			id.Domain = op
			ticker = &id
		}
	}
	for _, id := range []*types.Identifier{cusip, ticker, composite, bare} {
		if id != nil {
			return *id, nil
		}
	}
	return types.Identifier{}, errors.New("no CUSIP or US ticker")
}
