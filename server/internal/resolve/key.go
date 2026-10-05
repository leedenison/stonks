package resolve

import (
	"fmt"
	"slices"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// parents is the asset class tree, the root mapping to "". A test holds it
// equal to the asset_class_tree table.
var parents = map[gen.AssetClass]gen.AssetClass{
	gen.AssetClassUnknown:     "",
	gen.AssetClassCash:        gen.AssetClassUnknown,
	gen.AssetClassSecurity:    gen.AssetClassUnknown,
	gen.AssetClassEquity:      gen.AssetClassSecurity,
	gen.AssetClassStock:       gen.AssetClassEquity,
	gen.AssetClassEtf:         gen.AssetClassEquity,
	gen.AssetClassMutualFund:  gen.AssetClassEquity,
	gen.AssetClassFixedIncome: gen.AssetClassSecurity,
	gen.AssetClassDerivative:  gen.AssetClassSecurity,
	gen.AssetClassOption:      gen.AssetClassDerivative,
	gen.AssetClassFuture:      gen.AssetClassDerivative,
}

// under reports whether a is b or lies below it.
func under(a, b gen.AssetClass) bool {
	for c := a; c != ""; c = parents[c] {
		if c == b {
			return true
		}
	}
	return false
}

// disjoint reports whether no instrument can be of both classes. An unknown
// class is disjoint from none.
func disjoint(a, b gen.AssetClass) bool { return !under(a, b) && !under(b, a) }

// classConflict returns why the class k states contradicts an instrument of
// class, if it does.
func classConflict(k gen.StatedKey, class gen.AssetClass) (string, bool) {
	if k.AssetClass == nil || !disjoint(*k.AssetClass, class) {
		return "", false
	}
	return fmt.Sprintf("asset class %s contradicts the instrument's %s", *k.AssetClass, class), true
}

func grain(id types.Identifier) gen.IdentifierGrain { return market.Trait(id.Type).Grain }

// exclusive reports whether a second value of id's type for one subject in
// one domain contradicts id.
func exclusive(id types.Identifier) bool { return market.Trait(id.Type).Exclusive }

func stable(id types.Identifier) bool {
	return market.Trait(id.Type).Reassignment == gen.IdentifierReassignmentStable
}

// strength ranks id by how firmly it names its subject, the strongest
// first: an identifier every party reads before an issuer's own, a stable
// identifier before a reassignable one, and within each an instrument's
// before a listing's.
func strength(id types.Identifier) int {
	n := 0
	if market.Trait(id.Type).Domain == gen.IdentifierDomainIssuer {
		n += 4
	}
	if !stable(id) {
		n += 2
	}
	if grain(id) == gen.IdentifierGrainListing {
		n++
	}
	return n
}

// trusted returns the identifiers k states that the lookup trusts a hit on
// to name the instrument: every GUID and every broker description, strongest
// first.
func trusted(k gen.StatedKey) []types.Identifier {
	var out []types.Identifier
	for _, id := range k.Identifiers {
		if market.IsGUID(id) || id.Type == types.IdentifierTypeBrokerDescription {
			out = append(out, id)
		}
	}
	slices.SortStableFunc(out, func(a, b types.Identifier) int { return strength(a) - strength(b) })
	return out
}

// bare reports whether k states a ticker without its venue.
func bare(k gen.StatedKey) bool {
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeMicTicker && id.Domain == "" {
			return true
		}
	}
	return false
}

// families maps a currency code to its family, the key of a listing.
type families map[string]string

// family returns the family of the currency k states, "" where k states
// none.
func (fs families) family(k gen.StatedKey) string {
	if k.Currency == nil {
		return ""
	}
	return fs[*k.Currency]
}

// name writes id as its type and value, the venue leading the value.
func name(id types.Identifier) string {
	if id.Domain == "" {
		return string(id.Type) + " " + id.Value
	}
	return string(id.Type) + " " + id.Domain + ":" + id.Value
}
