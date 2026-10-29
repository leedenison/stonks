package resolve

import (
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// noFamily is the family of a currency the system does not know, and of a
// key or returned listing that states no currency. A claim's listing of no
// family matches any family.
const noFamily = ""

// known returns code where the system has the currency. It returns the empty
// string for a code the system lacks, such as the ZAC of a JSE listing. The
// currency of an assertion references the currencies table, so an assertion
// stores only a code the system has.
func known(f currency.Families, code string) string {
	if !f.Known(code) {
		return ""
	}
	return code
}

// keyFamily returns the family of the currency k states.
func keyFamily(f currency.Families, k gen.StatedKey) string {
	if k.Currency == nil {
		return noFamily
	}
	return f.Family(*k.Currency)
}
