package datasource

import (
	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// StatedKey is one key of an identity fetch.
type StatedKey struct {
	ID          uuid.UUID
	Identifiers []types.Identifier
	Class       gen.AssetClass
	Currency    string
}

// Candidate results of one fetch. Candidates are unranked, and several may be
// listings of one instrument rather than competing answers.
type Candidate struct {
	Identifiers []types.Identifier
	Class       gen.AssetClass
	Currency    string
}

// IdentityResult is what was returned for one identifier of a batch.
type IdentityResult struct {
	// Filtered contains one or more identifiers when the datasource strictly
	// filtered on them, empty otherwise.  This allows the framework to
	// correctly interpret the Candidates set.
	Filtered []types.Identifier
	// Candidates contains the results that are constrained to be consistent
	// with Filtered.  Where Filtered is empty, candidates contain the results
	// of an unconstrained search.
	Candidates []Candidate
}

// Identity looks up the set of identifiers that refer to the same instrument
// as the supplied identifiers.  Additional metadata about the instrument
// known to the datasource is also returned.
type Identity interface {
	Integration
	Server[StatedKey, IdentityResult]
}

// IdentityKind is the identity of the instruments stated keys name.
var IdentityKind = Kind[StatedKey, IdentityResult]{
	name:    gen.FetchKindIdentity,
	server:  func(e *Entry) Server[StatedKey, IdentityResult] { return e.Identity },
	subject: func(k StatedKey) uuid.UUID { return k.ID },
}
