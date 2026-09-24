package openfigi

import (
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// idTypes are the identifier types sent, strongest first, and the OpenFIGI
// idType each is sent as.
var idTypes = []struct {
	typ    types.IdentifierType
	idType string
}{
	{types.IdentifierTypeOpenfigiShareClass, "ID_BB_GLOBAL_SHARE_CLASS_LEVEL"},
	{types.IdentifierTypeOpenfigiComposite, "COMPOSITE_ID_BB_GLOBAL"},
	{types.IdentifierTypeIsin, "ID_ISIN"},
	{types.IdentifierTypeCusip, "ID_CUSIP"},
	{types.IdentifierTypeCins, "ID_CINS"},
	{types.IdentifierTypeSedol, "ID_SEDOL"},
	{types.IdentifierTypeWertpapier, "ID_WERTPAPIER"},
	{types.IdentifierTypeOpenfigiTicker, "TICKER"},
	{types.IdentifierTypeMicTicker, "TICKER"},
}

// Serves returns the strongest identifier of key OpenFIGI accepts.
func (c *Client) Serves(key market.StatedKey) (types.Identifier, error) {
	var unknown error
	for _, t := range idTypes {
		for _, id := range key.Identifiers {
			if id.Type != t.typ {
				continue
			}
			if id.Type != types.IdentifierTypeMicTicker || id.Domain == "" {
				return id, nil
			}
			op, ok := c.mics.Operating(id.Domain)
			if !ok {
				unknown = fmt.Errorf("venue %s of ticker %s is not a MIC", id.Domain, id.Value)
				continue
			}
			id.Domain = op
			return id, nil
		}
	}
	if unknown != nil {
		return types.Identifier{}, unknown
	}
	return types.Identifier{}, errors.New("states no identifier OpenFIGI maps")
}

// job is one entry of a mapping request.
type job struct {
	IDType   string `json:"idType"`
	IDValue  string `json:"idValue"`
	ExchCode string `json:"exchCode,omitempty"`
	Currency string `json:"currency,omitempty"`
}

// bloomberg spells the minor unit currencies as OpenFIGI does, the major code
// with its last letter lowercased.
var bloomberg = map[string]string{"GBX": "GBp"}

// jobOf returns the mapping job for an identifier Serves returned. A stated
// currency filters the job strictly.
func jobOf(id types.Identifier, currency string) job {
	for _, t := range idTypes {
		if t.typ != id.Type {
			continue
		}
		j := job{IDType: t.idType, IDValue: id.Value, Currency: currency}
		if b, ok := bloomberg[currency]; ok {
			j.Currency = b
		}
		switch id.Type {
		case types.IdentifierTypeOpenfigiTicker:
			j.IDValue, _ = withClassSep(id.Value, '/')
			j.ExchCode = id.Domain
		case types.IdentifierTypeMicTicker:
			j.IDValue, _ = withClassSep(id.Value, '/')
		}
		return j
	}
	return job{}
}
