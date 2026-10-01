package resolve

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=resolve -self_package=github.com/leedenison/stonks/server/internal/resolve

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
)

// Queries is the view of the generated queries this package depends on.
type Queries interface {
	ListCurrencies(ctx context.Context) ([]gen.Currency, error)
	FindIdentifier(ctx context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error)
	ListInstrumentsByIdentifiers(ctx context.Context, arg gen.ListInstrumentsByIdentifiersParams) ([]gen.ListInstrumentsByIdentifiersRow, error)
	ListIdentifiers(ctx context.Context, instrumentID uuid.UUID) ([]gen.Identifier, error)
	ListListings(ctx context.Context, instrumentID uuid.UUID) ([]gen.Listing, error)
	ListIdentityCoverage(ctx context.Context, instrumentIds []uuid.UUID) ([]gen.IdentityCoverage, error)
	LockIdentifiers(ctx context.Context, keys []string) error
	CreateInstrument(ctx context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error)
	CreateListing(ctx context.Context, arg gen.CreateListingParams) (gen.Listing, error)
	CreateIdentifier(ctx context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error)
	UpsertIdentityCoverage(ctx context.Context, arg gen.UpsertIdentityCoverageParams) error
	SetFetchKeyInstrument(ctx context.Context, arg gen.SetFetchKeyInstrumentParams) error
	CreateFetchIdentifier(ctx context.Context, arg gen.CreateFetchIdentifierParams) error
	CreateFinding(ctx context.Context, arg gen.CreateFindingParams) error
	SetStatedKeyAssociation(ctx context.Context, arg gen.SetStatedKeyAssociationParams) error
	CreateResolutionKey(ctx context.Context, arg gen.CreateResolutionKeyParams) error
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Sources is the view of the datasource registry this package depends on.
type Sources interface {
	Enabled() []*market.Entry
}

var _ Sources = (*market.Registry)(nil)

// Fetcher is the view of the fetch framework this package depends on: one
// identity fetch from e, as a child run of parent, with a result per key.
type Fetcher interface {
	Identity(ctx context.Context, parent gen.Run, e *market.Entry, keys []gen.StatedKey) ([]result, error)
}

var _ Fetcher = market.IdentityFetcher{}
