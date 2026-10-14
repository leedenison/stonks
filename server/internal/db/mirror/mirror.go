// Package mirror holds the schema's reference tables mirrored in code: the
// identifier traits and the asset class tree. A test holds each equal to its
// table.
package mirror

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// traits holds each identifier type's row of identifier_type_traits.
var traits = map[types.IdentifierType]gen.IdentifierTypeTrait{
	types.IdentifierTypeIsin:               {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 7},
	types.IdentifierTypeCusip:              {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 8},
	types.IdentifierTypeCins:               {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 10},
	types.IdentifierTypeWertpapier:         {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 11},
	types.IdentifierTypeOpenfigiShareClass: {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 14},
	types.IdentifierTypeSedol:              {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 9},
	types.IdentifierTypeOpenfigiComposite:  {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentStable, Exclusive: false, NameRank: 12},
	types.IdentifierTypeMicTicker:          {Domain: gen.IdentifierDomainVenue, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 4},
	types.IdentifierTypeOpenfigiTicker:     {Domain: gen.IdentifierDomainVenue, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 13},
	types.IdentifierTypeOcc:                {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 1},
	types.IdentifierTypeCurrency:           {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 3},
	types.IdentifierTypeDatasourceTicker:   {Domain: gen.IdentifierDomainIssuer, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 6},
	types.IdentifierTypeBrokerID:           {Domain: gen.IdentifierDomainIssuer, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true, NameRank: 15},
	types.IdentifierTypeBrokerDescription:  {Domain: gen.IdentifierDomainIssuer, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentUnverifiable, Exclusive: false, NameRank: 5},
	types.IdentifierTypeOption:             {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 2},
}

// Trait returns the traits of t.
func Trait(t types.IdentifierType) gen.IdentifierTypeTrait {
	tr := traits[t]
	tr.Type = t
	return tr
}

// IsGlobal reports whether id is a global identifier, one every party
// recognises. Its domain is the global domain, or a venue domain with the
// venue stated. An issuer's identifier is known only to the broker or
// datasource that minted it, and a ticker without its venue is ambiguous.
func IsGlobal(id types.Identifier) bool {
	switch traits[id.Type].Domain {
	case gen.IdentifierDomainGlobal:
		return true
	case gen.IdentifierDomainVenue:
		return id.Domain != ""
	}
	return false
}

// Joining reports whether id joins the keys that state it. A global
// identifier joins them, and so does one with its domain stated. A bare
// ticker leaves its domain empty and stays apart.
func Joining(id types.Identifier) bool {
	return traits[id.Type].Domain == gen.IdentifierDomainGlobal || id.Domain != ""
}

// parents is the asset class tree, the root mapping to "".
var parents = map[types.AssetClass]types.AssetClass{
	types.AssetClassUnknown:     "",
	types.AssetClassCash:        types.AssetClassUnknown,
	types.AssetClassSecurity:    types.AssetClassUnknown,
	types.AssetClassEquity:      types.AssetClassSecurity,
	types.AssetClassStock:       types.AssetClassEquity,
	types.AssetClassEtf:         types.AssetClassEquity,
	types.AssetClassMutualFund:  types.AssetClassEquity,
	types.AssetClassFixedIncome: types.AssetClassSecurity,
	types.AssetClassDerivative:  types.AssetClassSecurity,
	types.AssetClassOption:      types.AssetClassDerivative,
	types.AssetClassFuture:      types.AssetClassDerivative,
}

// under reports whether a is b or lies below it.
func under(a, b types.AssetClass) bool {
	for c := a; c != ""; c = parents[c] {
		if c == b {
			return true
		}
	}
	return false
}

// Disjoint reports whether no instrument can be of both classes. An unknown
// class overlaps every class.
func Disjoint(a, b types.AssetClass) bool { return !under(a, b) && !under(b, a) }
