package market

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// sendBare returns the ticker bare to send for key, or an error when key states
// a global identifier. A provider given only the ticker answers for whichever
// instrument matches it, which need not be the one that identifier names.
func sendBare(key gen.StatedKey, bare types.Identifier) (types.Identifier, error) {
	for _, id := range key.Identifiers {
		if id.IsGlobal() {
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

// Canonical reports whether ticker is written as the system writes a
// MIC_TICKER: at most one share class separator, written as a dot, with its
// case kept.
func Canonical(ticker string) bool {
	out, ok := WithClassSep(ticker, '.')
	return ok && out == ticker
}

// Equities is the set of stated classes a provider of listed equities
// answers: the classes it converts and the classes above them.
var Equities = map[types.AssetClass]bool{
	types.AssetClassUnknown:    true,
	types.AssetClassSecurity:   true,
	types.AssetClassEquity:     true,
	types.AssetClassStock:      true,
	types.AssetClassEtf:        true,
	types.AssetClassMutualFund: true,
}

// Preference states which identifier of a key an integration sends: the
// first type of Order that the key states, with a venue ticker at its
// operating MIC.
type Preference struct {
	// Served limits the asset classes; nil serves every class.
	Served map[types.AssetClass]bool
	Order  []types.IdentifierType
	// Lists reports whether the integration lists an operating MIC; nil
	// means every venue.
	Lists func(op string) bool
	Mics  mic.Table
}

// Serves returns the identifier of key to send, or why none is sent. A ticker
// without its venue is sent last, and only for a key without a global
// identifier. An integration that lists some venues declines a key that states
// a ticker at a venue outside them, whatever else the key states, since it
// would answer with listings elsewhere. One that lists every venue skips a
// ticker whose venue is not a MIC, and declines the key only when nothing is
// left.
func (p Preference) Serves(key gen.StatedKey) (types.Identifier, error) {
	if key.AssetClass != nil && p.Served != nil && !p.Served[*key.AssetClass] {
		return types.Identifier{}, fmt.Errorf("asset class '%s' rejected", *key.AssetClass)
	}
	first := map[types.IdentifierType]types.Identifier{}
	var bare *types.Identifier
	var unknown error
	for _, id := range key.Identifiers {
		if id.Type == types.IdentifierTypeMicTicker {
			if id.Domain == "" {
				if bare == nil {
					bare = &id
				}
				continue
			}
			op, ok := p.Mics.Operating(id.Domain)
			switch {
			case p.Lists != nil && (!ok || !p.Lists(op)):
				return types.Identifier{}, fmt.Errorf("venue '%s' rejected", id.Domain)
			case !ok:
				unknown = fmt.Errorf("venue %s of ticker %s is not a MIC", id.Domain, id.Value)
				continue
			}
			id.Domain = op
		}
		if _, seen := first[id.Type]; !seen {
			first[id.Type] = id
		}
	}
	for _, t := range p.Order {
		if id, ok := first[t]; ok {
			return id, nil
		}
	}
	if bare != nil && slices.Contains(p.Order, types.IdentifierTypeMicTicker) {
		return sendBare(key, *bare)
	}
	if unknown != nil {
		return types.Identifier{}, unknown
	}
	return types.Identifier{}, errors.New("no identifier of a type the datasource accepts")
}
