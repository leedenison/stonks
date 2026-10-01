package market

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Candidate results of one fetch. Candidates are unranked, and several may be
// listings of one instrument rather than competing responses.
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
	Server[gen.StatedKey, IdentityResult]
}

// IdentityKind is the identity of the instruments stated keys name. The
// request is the stated key row, and its id is the subject of the fetch key.
var IdentityKind = Kind[gen.StatedKey, IdentityResult]{
	name:    gen.FetchKindIdentity,
	server:  func(e *Entry) Server[gen.StatedKey, IdentityResult] { return e.Identity },
	subject: func(k gen.StatedKey) uuid.UUID { return k.ID },
	count:   func(r IdentityResult) int { return len(r.Candidates) },
}

// IdentityFetcher fetches identity through F.
type IdentityFetcher struct {
	F *Fetcher
}

// Identity fetches the identity of keys from e, as a child run of parent,
// and returns a result per key whether or not the run failed.
func (a IdentityFetcher) Identity(ctx context.Context, parent gen.Run, e *Entry, keys []gen.StatedKey) ([]Result[gen.StatedKey, IdentityResult], error) {
	_, results, err := Fetch(ctx, a.F, parent, e, IdentityKind, keys)
	return results, err
}
