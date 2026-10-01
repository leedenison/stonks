// Package to converts generated rows to the types of [types]. A conversion
// is a method on its source type, defined beside it; a generated type cannot
// carry one, so its conversions live here.
package to

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Identifier returns the triple an identifier row names.
func Identifier(row gen.Identifier) types.Identifier {
	return types.Identifier{Type: row.Type, Domain: row.Domain, Value: row.Value}
}
