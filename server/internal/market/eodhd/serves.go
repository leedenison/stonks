package eodhd

import (
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// served is the set of stated classes EODHD can answer: the classes it converts
// and the classes above them.
var served = map[gen.AssetClass]bool{
	gen.AssetClassUnknown:    true,
	gen.AssetClassSecurity:   true,
	gen.AssetClassEquity:     true,
	gen.AssetClassStock:      true,
	gen.AssetClassEtf:        true,
	gen.AssetClassMutualFund: true,
}

// Serves returns the one identifier of key to send to EODHD, preferring an
// ISIN, a CUSIP, a composite FIGI, a venue ticker, an OpenFIGI ticker and a
// bare ticker in that order. A venue ticker comes back at its operating MIC.
// Serves declines a key that states a ticker at a venue outside EODHD's
// exchanges, whatever else the key states, because EODHD would answer with
// listings elsewhere.
func (c *Client) Serves(key gen.StatedKey) (types.Identifier, error) {
	if key.AssetClass != nil && !served[*key.AssetClass] {
		return types.Identifier{}, fmt.Errorf("asset class '%s' rejected", *key.AssetClass)
	}
	var isin, cusip, figi, ticker, openfigi, bare *types.Identifier
	for _, id := range key.Identifiers {
		switch id.Type {
		case types.IdentifierTypeIsin:
			isin = &id
		case types.IdentifierTypeCusip:
			cusip = &id
		case types.IdentifierTypeOpenfigiComposite:
			figi = &id
		case types.IdentifierTypeOpenfigiTicker:
			if id.Domain == market.OpenFIGIUS {
				openfigi = &id
				continue
			}
			op, ok := market.OpenFIGIVenue(id.Domain, c.mics)
			if !ok {
				continue
			}
			if len(c.exchange[op]) == 0 {
				return types.Identifier{}, fmt.Errorf("venue '%s' rejected", id.Domain)
			}
			openfigi = &types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: op, Value: id.Value}
		case types.IdentifierTypeMicTicker:
			if id.Domain == "" {
				bare = &id
				continue
			}
			op, ok := c.mics.Operating(id.Domain)
			if !ok || len(c.exchange[op]) == 0 {
				return types.Identifier{}, fmt.Errorf("venue '%s' rejected", id.Domain)
			}
			id.Domain = op
			ticker = &id
		}
	}
	for _, id := range []*types.Identifier{isin, cusip, figi, ticker, openfigi, bare} {
		if id != nil {
			return *id, nil
		}
	}
	return types.Identifier{}, errors.New("no ISIN, CUSIP, composite FIGI or ticker")
}
