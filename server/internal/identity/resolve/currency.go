package resolve

import (
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// noFamily is the family of a currency the system does not know, and of a
// key or returned listing that states no currency. A claim's listing of no
// family matches any family.
const noFamily = ""

// keyFamily returns the family of the currency k states.
func keyFamily(f currency.Families, k gen.StatedKey) string {
	if k.Currency == nil {
		return noFamily
	}
	return f.Family(*k.Currency)
}
