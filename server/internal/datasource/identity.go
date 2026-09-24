package datasource

import (
	"context"

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
	// Err is set when the provider failed for the identifier.
	Err error
}

// Identity looks up the set of identifiers that refer to the same instrument
// as the supplied identifiers.  Additional metadata about the instrument
// known to the datasource is also returned.
type Identity interface {
	Integration
	// Serves reports the identifier to send for key. An error means the
	// integration serves nothing for it and the error's text is the reason.
	Serves(key StatedKey) (types.Identifier, error)
	// Fetch calls the provider and marshalls the results.
	//
	// Fetch is called with a batch. The integration chunks the batch to whatever
	// the provider accepts. Rate limits are applied per datasource for the life
	// of the process, so concurrent resolutions share the quota.
	Fetch(ctx context.Context, sent []types.Identifier) ([]IdentityResult, error)
}
