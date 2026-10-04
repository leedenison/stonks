package resolve

import (
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

// Disjoint reports whether no instrument can be of both classes. An unknown
// class is disjoint from none.
func Disjoint(a, b gen.AssetClass) bool { return !under(a, b) && !under(b, a) }

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

// guids returns the identifiers k states that are recognised across
// organizations, strongest first.
func guids(k gen.StatedKey) []types.Identifier {
	var out []types.Identifier
	for _, id := range k.Identifiers {
		if market.IsGUID(id) {
			out = append(out, id)
		}
	}
	slices.SortStableFunc(out, func(a, b types.Identifier) int { return strength(a) - strength(b) })
	return out
}

// trusted returns the identifiers k states that the lookup trusts a hit on
// to name the instrument: every GUID and every broker description, strongest
// first.
func trusted(k gen.StatedKey) []types.Identifier {
	out := guids(k)
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeBrokerDescription {
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

// family returns the family of the currency k states, mapped through
// families, and "" where k states none.
func family(k gen.StatedKey, families func(string) string) string {
	if k.Currency == nil {
		return ""
	}
	return families(*k.Currency)
}

// name writes id as its type and value, the venue leading the value.
func name(id types.Identifier) string {
	if id.Domain == "" {
		return string(id.Type) + " " + id.Value
	}
	return string(id.Type) + " " + id.Domain + ":" + id.Value
}
