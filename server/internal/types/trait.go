package types

import "slices"

// IdentifierDomain is the identifier_domain enum. A test holds it equal to
// the database.
type IdentifierDomain string

// IdentifierDomains are the values the enum names, in the order the database
// declares them.
var IdentifierDomains = []IdentifierDomain{IdentifierDomainGlobal, IdentifierDomainVenue, IdentifierDomainIssuer}

// The identifier domains, spelled as the identifier_domain enum spells them.
const (
	IdentifierDomainGlobal IdentifierDomain = "global"
	IdentifierDomainVenue  IdentifierDomain = "venue"
	IdentifierDomainIssuer IdentifierDomain = "issuer"
)

// Scan reads the enum from a text or bytea column.
func (e *IdentifierDomain) Scan(src any) error { return scan(e, src) }

// Valid reports whether e is one of the enum's values.
func (e IdentifierDomain) Valid() bool { return slices.Contains(IdentifierDomains, e) }

// IdentifierGrain is the identifier_grain enum. A test holds it equal to the
// database.
type IdentifierGrain string

// IdentifierGrains are the values the enum names, in the order the database
// declares them.
var IdentifierGrains = []IdentifierGrain{IdentifierGrainInstr, IdentifierGrainListing}

// The identifier grains, spelled as the identifier_grain enum spells them.
const (
	IdentifierGrainInstr   IdentifierGrain = "instr"
	IdentifierGrainListing IdentifierGrain = "listing"
)

// Scan reads the enum from a text or bytea column.
func (e *IdentifierGrain) Scan(src any) error { return scan(e, src) }

// Valid reports whether e is one of the enum's values.
func (e IdentifierGrain) Valid() bool { return slices.Contains(IdentifierGrains, e) }

// IdentifierReassignment is the identifier_reassignment enum. A test holds it
// equal to the database.
type IdentifierReassignment string

// IdentifierReassignments are the values the enum names, in the order the
// database declares them.
var IdentifierReassignments = []IdentifierReassignment{
	IdentifierReassignmentStable,
	IdentifierReassignmentMicDerived,
	IdentifierReassignmentUnverifiable,
}

// The identifier reassignments, spelled as the identifier_reassignment enum
// spells them.
const (
	IdentifierReassignmentStable       IdentifierReassignment = "stable"
	IdentifierReassignmentMicDerived   IdentifierReassignment = "mic_derived"
	IdentifierReassignmentUnverifiable IdentifierReassignment = "unverifiable"
)

// Scan reads the enum from a text or bytea column.
func (e *IdentifierReassignment) Scan(src any) error { return scan(e, src) }

// Valid reports whether e is one of the enum's values.
func (e IdentifierReassignment) Valid() bool { return slices.Contains(IdentifierReassignments, e) }

// Trait is an identifier type's row of identifier_type_traits. A test holds
// the traits equal to the table.
type Trait struct {
	Domain       IdentifierDomain
	Grain        IdentifierGrain
	Reassignment IdentifierReassignment
	Exclusive    bool
	NameRank     int32
}

var traits = map[IdentifierType]Trait{
	IdentifierTypeIsin:               {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 7},
	IdentifierTypeCusip:              {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 8},
	IdentifierTypeCins:               {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 10},
	IdentifierTypeWertpapier:         {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 11},
	IdentifierTypeOpenfigiShareClass: {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 14},
	IdentifierTypeSedol:              {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainListing, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 9},
	IdentifierTypeOpenfigiComposite:  {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainListing, Reassignment: IdentifierReassignmentStable, Exclusive: false, NameRank: 12},
	IdentifierTypeMicTicker:          {Domain: IdentifierDomainVenue, Grain: IdentifierGrainListing, Reassignment: IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 4},
	IdentifierTypeOpenfigiTicker:     {Domain: IdentifierDomainVenue, Grain: IdentifierGrainListing, Reassignment: IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 13},
	IdentifierTypeOcc:                {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 1},
	IdentifierTypeCurrency:           {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 3},
	IdentifierTypeDatasourceTicker:   {Domain: IdentifierDomainIssuer, Grain: IdentifierGrainListing, Reassignment: IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 6},
	IdentifierTypeBrokerID:           {Domain: IdentifierDomainIssuer, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentStable, Exclusive: true, NameRank: 15},
	IdentifierTypeBrokerDescription:  {Domain: IdentifierDomainIssuer, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentUnverifiable, Exclusive: false, NameRank: 5},
	IdentifierTypeOption:             {Domain: IdentifierDomainGlobal, Grain: IdentifierGrainInstr, Reassignment: IdentifierReassignmentMicDerived, Exclusive: true, NameRank: 2},
}

// Trait returns the traits of e.
func (e IdentifierType) Trait() Trait { return traits[e] }

// IsGlobal reports whether i is a global identifier, one every party
// recognises. Its domain is the global domain, or a venue domain with the
// venue stated. An issuer's identifier is known only to the broker or
// datasource that minted it, and a ticker without its venue is ambiguous.
func (i Identifier) IsGlobal() bool {
	switch i.Type.Trait().Domain {
	case IdentifierDomainGlobal:
		return true
	case IdentifierDomainVenue:
		return i.Domain != ""
	}
	return false
}

// Joining reports whether i joins the keys that state it. A global
// identifier joins them, and so does one with its domain stated. A bare
// ticker leaves its domain empty and stays apart.
func (i Identifier) Joining() bool {
	return i.Type.Trait().Domain == IdentifierDomainGlobal || i.Domain != ""
}

// Grain returns the level i names, an instrument or one of its listings.
func (i Identifier) Grain() IdentifierGrain { return i.Type.Trait().Grain }

// Exclusive reports whether a second value of i's type in one domain, on the
// instrument or listing i identifies, contradicts i.
func (i Identifier) Exclusive() bool { return i.Type.Trait().Exclusive }

// Stable reports whether i is assumed never to be reassigned.
func (i Identifier) Stable() bool {
	return i.Type.Trait().Reassignment == IdentifierReassignmentStable
}

// Ticker reports whether i is a ticker: an identifier of a listing that is
// not stable. A ticker names different instruments on different days, so a
// key that associates through one is dated. An option symbol is reassignable too, but it
// names an instrument, and a key stays fixed through it.
func (i Identifier) Ticker() bool {
	return i.Grain() == IdentifierGrainListing && !i.Stable()
}

// Strength ranks i by how firmly it identifies its instrument or listing, the
// strongest first:
// an identifier every party reads before an issuer's own, a stable
// identifier before a reassignable one, and within each an instrument's
// before a listing's.
func (i Identifier) Strength() int {
	n := 0
	if i.Type.Trait().Domain == IdentifierDomainIssuer {
		n += 4
	}
	if !i.Stable() {
		n += 2
	}
	if i.Grain() == IdentifierGrainListing {
		n++
	}
	return n
}
