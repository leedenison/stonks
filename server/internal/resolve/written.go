package resolve

import (
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// WrittenIdentifierFacts pairs a run with the identifiers of the identifier
// facts it wrote. An identifier with no value stands for its whole domain, as
// a domain coverage row does.
type WrittenIdentifierFacts struct {
	Run         gen.Run
	Identifiers []types.Identifier
}
