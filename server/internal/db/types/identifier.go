// Package types holds the Go types of columns sqlc does not map itself,
// named in the overrides in sqlc.yaml, each type's conversion to its proto
// message, and the conversions between a database enum and its proto
// counterpart. It imports nothing of the server, so generated code may
// depend on it without a cycle.
package types

import (
	"fmt"
	"slices"

	typev1 "github.com/leedenison/stonks/proto/type/v1"
)

// IdentifierType is the identifier_type enum. It is written here rather than
// generated so that it, and the identifiers it types, sit where generated
// code and its consumers can both name them. A test holds it equal to the
// database.
type IdentifierType string

// IdentifierTypes are the values the enum names, in the order the database
// declares them.
var IdentifierTypes = []IdentifierType{
	IdentifierTypeIsin,
	IdentifierTypeCusip,
	IdentifierTypeCins,
	IdentifierTypeWertpapier,
	IdentifierTypeOpenfigiShareClass,
	IdentifierTypeSedol,
	IdentifierTypeOpenfigiComposite,
	IdentifierTypeMicTicker,
	IdentifierTypeOpenfigiTicker,
	IdentifierTypeOcc,
	IdentifierTypeCurrency,
	IdentifierTypeDatasourceTicker,
	IdentifierTypeBrokerID,
	IdentifierTypeBrokerDescription,
	IdentifierTypeOption,
}

// The identifier types, spelled as the identifier_type enum spells them.
const (
	IdentifierTypeIsin               IdentifierType = "isin"
	IdentifierTypeCusip              IdentifierType = "cusip"
	IdentifierTypeCins               IdentifierType = "cins"
	IdentifierTypeWertpapier         IdentifierType = "wertpapier"
	IdentifierTypeOpenfigiShareClass IdentifierType = "openfigi_share_class"
	IdentifierTypeSedol              IdentifierType = "sedol"
	IdentifierTypeOpenfigiComposite  IdentifierType = "openfigi_composite"
	IdentifierTypeMicTicker          IdentifierType = "mic_ticker"
	IdentifierTypeOpenfigiTicker     IdentifierType = "openfigi_ticker"
	IdentifierTypeOcc                IdentifierType = "occ"
	IdentifierTypeCurrency           IdentifierType = "currency"
	IdentifierTypeDatasourceTicker   IdentifierType = "datasource_ticker"
	IdentifierTypeBrokerID           IdentifierType = "broker_id"
	IdentifierTypeBrokerDescription  IdentifierType = "broker_description"
	IdentifierTypeOption             IdentifierType = "option"
)

// Scan reads the enum from a text or bytea column.
func (e *IdentifierType) Scan(src any) error {
	switch s := src.(type) {
	case []byte:
		*e = IdentifierType(s)
	case string:
		*e = IdentifierType(s)
	default:
		return fmt.Errorf("unsupported scan type for IdentifierType: %T", src)
	}
	return nil
}

// Valid reports whether e is one of the enum's values.
func (e IdentifierType) Valid() bool { return slices.Contains(IdentifierTypes, e) }

// Identifier is one identifier triple: a type, a value, and a domain empty
// where the type has none. A key stating no identifiers carries an empty
// slice, since nil encodes as null and the identifiers column refuses it.
type Identifier struct {
	Type   IdentifierType `json:"type"`
	Domain string         `json:"domain,omitempty"`
	Value  string         `json:"value"`
}

// ToProto writes i as its proto message.
func (i Identifier) ToProto() *typev1.Identifier {
	return &typev1.Identifier{Type: ToProto[typev1.IdentifierType](i.Type), Domain: i.Domain, Value: i.Value}
}
