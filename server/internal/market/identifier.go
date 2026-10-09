package market

import (
	"fmt"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/mic"
)

// traits holds each identifier type's row of identifier_type_traits. A test
// holds it equal to the table.
var traits = map[types.IdentifierType]gen.IdentifierTypeTrait{
	types.IdentifierTypeIsin:               {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeCusip:              {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeCins:               {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeWertpapier:         {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeOpenfigiShareClass: {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeSedol:              {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeOpenfigiComposite:  {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentStable, Exclusive: false},
	types.IdentifierTypeMicTicker:          {Domain: gen.IdentifierDomainVenue, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true},
	types.IdentifierTypeOpenfigiTicker:     {Domain: gen.IdentifierDomainVenue, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true},
	types.IdentifierTypeOcc:                {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true},
	types.IdentifierTypeCurrency:           {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeDatasourceTicker:   {Domain: gen.IdentifierDomainIssuer, Grain: gen.IdentifierGrainListing, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true},
	types.IdentifierTypeBrokerID:           {Domain: gen.IdentifierDomainIssuer, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentStable, Exclusive: true},
	types.IdentifierTypeBrokerDescription:  {Domain: gen.IdentifierDomainIssuer, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentUnverifiable, Exclusive: false},
	types.IdentifierTypeOption:             {Domain: gen.IdentifierDomainGlobal, Grain: gen.IdentifierGrainInstrument, Reassignment: gen.IdentifierReassignmentMicDerived, Exclusive: true},
}

// Trait returns the traits of t.
func Trait(t types.IdentifierType) gen.IdentifierTypeTrait {
	tr := traits[t]
	tr.Type = t
	return tr
}

// IsGUID reports whether id is recognised by every party: its domain is
// global, or venue with the venue stated. An issuer's identifier is known
// only to the broker or datasource that minted it, and a ticker without its
// venue names nothing.
func IsGUID(id types.Identifier) bool {
	switch traits[id.Type].Domain {
	case gen.IdentifierDomainGlobal:
		return true
	case gen.IdentifierDomainVenue:
		return id.Domain != ""
	}
	return false
}

// Bare returns the ticker bare to send for key, or an error when key
// states a GUID. A provider given only the ticker answers for whichever
// instrument matches it, which need not be the one the GUID names.
func Bare(key gen.StatedKey, bare types.Identifier) (types.Identifier, error) {
	for _, id := range key.Identifiers {
		if IsGUID(id) {
			return types.Identifier{}, fmt.Errorf("ticker '%s' without a venue declined: the key states %s '%s'", bare.Value, id.Type, id.Value)
		}
	}
	return bare, nil
}

// classSeps separate a ticker's root from its share class, as in BRK.B, BRK/B,
// BRK-B and "BRK B". A MIC_TICKER writes the separator as a dot.
const classSeps = ".-/ "

// WithClassSep writes ticker with its share class separator as sep. It
// reports false, returning ticker, when ticker has more than one separator.
func WithClassSep(ticker string, sep rune) (string, bool) {
	n := 0
	out := strings.Map(func(r rune) rune {
		if strings.ContainsRune(classSeps, r) {
			n++
			return sep
		}
		return r
	}, ticker)
	if n > 1 {
		return ticker, false
	}
	return out, true
}

// OpenFIGIUS is OpenFIGI's exchange code for a US composite listing.
const OpenFIGIUS = "US"

// OpenFIGIVenue returns the operating MIC an OpenFIGI exchange code names. It
// reports false for a code spanning several venues, such as a country's
// composite, and for a MIC that mics does not list.
func OpenFIGIVenue(code string, mics mic.Table) (string, bool) {
	m := openfigiCodes[code]
	if len(m) != 1 {
		return "", false
	}
	return mics.Operating(m[0])
}
