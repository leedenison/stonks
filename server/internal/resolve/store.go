package resolve

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=resolve -self_package=github.com/leedenison/stonks/server/internal/resolve

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListCurrencies(ctx context.Context) ([]gen.Currency, error)
	FindIdentifier(ctx context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error)
	ListInstrumentsByIdentifiers(ctx context.Context, arg gen.ListInstrumentsByIdentifiersParams) ([]gen.ListInstrumentsByIdentifiersRow, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
	ListIdentityCoverage(ctx context.Context, instrumentIds []uuid.UUID) ([]gen.IdentityCoverage, error)
	LockIdentifiers(ctx context.Context, keys []string) error
	CreateInstrument(ctx context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error)
	CreateListing(ctx context.Context, arg gen.CreateListingParams) (gen.Listing, error)
	CreateIdentifier(ctx context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error)
	UpsertIdentityCoverage(ctx context.Context, arg gen.UpsertIdentityCoverageParams) error
	SetFetchKeyInstrument(ctx context.Context, arg gen.SetFetchKeyInstrumentParams) error
	CreateFetchIdentifier(ctx context.Context, arg gen.CreateFetchIdentifierParams) error
	CreateFinding(ctx context.Context, arg gen.CreateFindingParams) error
	SetStatedKeyAssociation(ctx context.Context, arg gen.SetStatedKeyAssociationParams) (int64, error)
	GetStatedKey(ctx context.Context, arg gen.GetStatedKeyParams) (gen.StatedKey, error)
	ListUserArbitratedKeys(ctx context.Context, userID uuid.UUID) ([]gen.StatedKey, error)
	ListStatedKeysOfGroups(ctx context.Context, arg gen.ListStatedKeysOfGroupsParams) ([]gen.StatedKey, error)
	LockUserKeys(ctx context.Context, userID uuid.UUID) error
	CreateResolutionKey(ctx context.Context, arg gen.CreateResolutionKeyParams) (gen.ResolutionKey, error)
	DeferConstraints(ctx context.Context) error
	MoveListing(ctx context.Context, arg gen.MoveListingParams) error
	RelinkIdentifiers(ctx context.Context, arg gen.RelinkIdentifiersParams) error
	RelinkStatedKeys(ctx context.Context, arg gen.RelinkStatedKeysParams) error
	RelinkFetchKeys(ctx context.Context, arg gen.RelinkFetchKeysParams) error
	MoveIdentityCoverage(ctx context.Context, arg gen.MoveIdentityCoverageParams) error
	DeleteIdentityCoverage(ctx context.Context, instrumentID uuid.UUID) error
	DeleteListings(ctx context.Context, instrumentID uuid.UUID) error
	DeleteInstrument(ctx context.Context, id uuid.UUID) error
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Sources is this package's view of the datasource registry.
type Sources interface {
	Enabled() []*market.Entry
}

var _ Sources = (*market.Registry)(nil)

// Fetcher is this package's view of the fetch framework.
type Fetcher interface {
	Identity(ctx context.Context, parent gen.Run, e *market.Entry, keys []gen.StatedKey) ([]result, error)
}

var _ Fetcher = (*market.Fetcher)(nil)
