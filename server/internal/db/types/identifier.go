// Package types holds the Go types of columns sqlc does not map itself,
// named in the overrides in sqlc.yaml. It imports nothing of the server, so
// generated code may depend on it without a cycle.
package types

import (
	"fmt"
	"slices"
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
}

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
)

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

func (e IdentifierType) Valid() bool { return slices.Contains(IdentifierTypes, e) }

// Identifier is one identifier triple: a type, a value, and a domain empty
// where the type has none.
//
// It is also the element of the identifiers column of a stated key. That
// column is compared whole by the unique index, so a writer sorts the array
// by type, domain and value, and an absent domain is empty and omitted
// rather than written as null. A key stating no identifiers holds an empty
// slice, since nil encodes as null and the column refuses it.
type Identifier struct {
	Type   IdentifierType `json:"type"`
	Domain string         `json:"domain,omitempty"`
	Value  string         `json:"value"`
}
