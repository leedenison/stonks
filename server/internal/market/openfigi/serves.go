package openfigi

import (
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// idTypes are the identifier types sent, strongest first, and the OpenFIGI
// idType of each.
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
func (c *Client) Serves(key gen.StatedKey) (types.Identifier, error) {
	var unknown error
	for _, t := range idTypes {
		for _, id := range key.Identifiers {
			if id.Type != t.typ {
				continue
			}
			if id.Type != types.IdentifierTypeMicTicker {
				return id, nil
			}
			if id.Domain == "" {
				return market.Bare(key, id)
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
	return types.Identifier{}, errors.New("no recognised identifier type")
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

// filter returns the currency that a job for key uses as its filter, spelt
// as OpenFIGI spells it, and "" where the key states none.
func filter(key gen.StatedKey) string {
	code := currency(key)
	if b, ok := bloomberg[code]; ok {
		return b
	}
	return code
}

// Params is the currency filter, which is the one parameter a job takes
// from the key beyond the identifier sent.
func (c *Client) Params(key gen.StatedKey) []string {
	if f := filter(key); f != "" {
		return []string{f}
	}
	return nil
}

// jobOf returns the mapping job for an identifier Serves returned, filtered
// on currency. OpenFIGI returns no currency, so a stated currency filters
// the job strictly and every candidate is in it.
func jobOf(id types.Identifier, currency string) job {
	for _, t := range idTypes {
		if t.typ != id.Type {
			continue
		}
		j := job{IDType: t.idType, IDValue: id.Value, Currency: currency}
		switch id.Type {
		case types.IdentifierTypeOpenfigiTicker:
			j.IDValue, _ = market.WithClassSep(id.Value, '/')
			j.ExchCode = id.Domain
		case types.IdentifierTypeMicTicker:
			j.IDValue, _ = market.WithClassSep(id.Value, '/')
		}
		return j
	}
	return job{}
}
