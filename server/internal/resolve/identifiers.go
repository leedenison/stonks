package resolve

import (
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// classConflict returns why the class k states contradicts an instrument of
// class, if it does.
func classConflict(k gen.StatedKey, class types.AssetClass) (string, bool) {
	if k.AssetClass == nil || !k.AssetClass.Disjoint(class) {
		return "", false
	}
	return fmt.Sprintf("asset class %s contradicts the instrument's %s", *k.AssetClass, class), true
}

func grain(id types.Identifier) types.IdentifierGrain { return id.Type.Trait().Grain }

// exclusive reports whether a second value of id's type for one subject in
// one domain contradicts id.
func exclusive(id types.Identifier) bool { return id.Type.Trait().Exclusive }

func stable(id types.Identifier) bool {
	return id.Type.Trait().Reassignment == types.IdentifierReassignmentStable
}

// strength ranks id by how firmly it names its subject, the strongest
// first: an identifier every party reads before an issuer's own, a stable
// identifier before a reassignable one, and within each an instrument's
// before a listing's.
func strength(id types.Identifier) int {
	n := 0
	if id.Type.Trait().Domain == types.IdentifierDomainIssuer {
		n += 4
	}
	if !stable(id) {
		n += 2
	}
	if grain(id) == types.IdentifierGrainListing {
		n++
	}
	return n
}

// statesBareTicker reports whether k states a ticker without its venue.
func statesBareTicker(k gen.StatedKey) bool {
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeMicTicker && id.Domain == "" {
			return true
		}
	}
	return false
}

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

// name writes id as its type and value, the venue leading the value.
func name(id types.Identifier) string {
	if id.Domain == "" {
		return string(id.Type) + " " + id.Value
	}
	return string(id.Type) + " " + id.Domain + ":" + id.Value
}
