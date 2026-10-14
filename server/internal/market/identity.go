package market

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Listing is one listing of an instrument, as a datasource returned it for a
// fetch. Listings are unranked. Several may belong to one instrument.
type Listing struct {
	Identifiers []types.Identifier
	Class       types.AssetClass
	Currency    string
	// Primary is the operating MIC of the venue the datasource states as
	// the listing's primary; empty where it states none.
	Primary string
}

// Answer returns the IdentityResult for sent with no listings yet; the
// caller adds them. A MIC_TICKER is filtered on its ticker alone, so the
// listings at every venue come back and resolution chooses among them by the
// stated venue. limited marks a provider that lists only some of an
// instrument's listings.
func Answer(sent types.Identifier, limited bool) IdentityResult {
	if sent.Type == types.IdentifierTypeMicTicker {
		sent.Domain = ""
	}
	return IdentityResult{Filtered: []types.Identifier{sent}, Limited: limited}
}

// IdentityResult is what was returned for one identifier of a batch.
type IdentityResult struct {
	// Filtered is the identifiers the datasource filtered on strictly, so
	// every listing is consistent with them. Empty means an unconstrained
	// search.
	Filtered []types.Identifier
	Listings []Listing
	// Limited reports that the provider lists only some of an instrument's
	// listings. Otherwise an answer without a listing in a currency means
	// the instrument has none in it.
	Limited bool
}

// Identity looks up the set of identifiers that refer to the same instrument
// as the supplied identifiers.  Additional metadata about the instrument
// known to the datasource is also returned.
type Identity interface {
	Integration
	Server[gen.StatedKey, IdentityResult]
}

// identityKind is the identity of the instruments stated keys name.
var identityKind = Kind[gen.StatedKey, IdentityResult]{
	name: gen.FetchKindIdentity,
	server: func(e *Entry) Server[gen.StatedKey, IdentityResult] {
		s, _ := e.Integration.(Identity)
		return s
	},
	subject: func(k gen.StatedKey) uuid.UUID { return k.ID },
	count:   func(r IdentityResult) int { return len(r.Listings) },
}

// Identity fetches the identity of keys from e, as a child run of parent.
func (f *Fetcher) Identity(ctx context.Context, parent gen.Run, e *Entry, keys []gen.StatedKey) ([]FetchKey[gen.StatedKey, IdentityResult], error) {
	return fetch(ctx, f, parent, e, identityKind, keys)
}
