package types

import (
	"iter"
	"maps"
	"slices"
)

// Instr is a security as one source describes it, whether a
// datasource or the database.
type Instr struct {
	// Class is empty where the source states none.
	Class AssetClass
	// Identifiers name the instrument itself, not one of its listings.
	Identifiers []Identifier
	// Listings is keyed by currency family. The empty family holds the
	// listing identifiers of an unknown currency.
	Listings map[string]Listing
}

// Listing is one listing of an instrument.
type Listing struct {
	Identifiers []Identifier
	// Primary is the operating MIC of the listing's primary venue, empty
	// where it is unknown.
	Primary string
}

// AllIDs yields every identifier of i, the instrument's first and then each
// listing's in family order.
func (i Instr) AllIDs() iter.Seq[Identifier] {
	return func(yield func(Identifier) bool) {
		for _, id := range i.Identifiers {
			if !yield(id) {
				return
			}
		}
		for _, f := range slices.Sorted(maps.Keys(i.Listings)) {
			for _, id := range i.Listings[f].Identifiers {
				if !yield(id) {
					return
				}
			}
		}
	}
}

// IdentifiedBy reports whether id is among the identifiers of i.
func (i Instr) IdentifiedBy(id Identifier) bool {
	for h := range i.AllIDs() {
		if h == id {
			return true
		}
	}
	return false
}
