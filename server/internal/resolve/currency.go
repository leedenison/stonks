package resolve

import "github.com/leedenison/stonks/server/internal/db/gen"

// noFamily is the family of a currency the system does not know, and of a
// key or candidate that states no currency. A listing of no family matches
// any family.
const noFamily = ""

// currencies maps a currency code to its family, the key of a listing.
type currencies map[string]string

// family returns the family of the currency k states.
func (cs currencies) family(k gen.StatedKey) string {
	if k.Currency == nil {
		return noFamily
	}
	return cs[*k.Currency]
}
