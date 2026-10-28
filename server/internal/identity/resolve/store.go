package resolve

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=resolve -self_package=github.com/leedenison/stonks/server/internal/identity/resolve

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	tickervalidity.FactQueries
	tickervalidity.ValidityQueries
	tickervalidity.RecordQueries
	TickerAssertedOn(ctx context.Context, arg gen.TickerAssertedOnParams) (bool, error)
	ListIdentifierEventSources(ctx context.Context) ([]gen.ListIdentifierEventSourcesRow, error)
	tickers.Queries
	ListTickersOfInstrs(ctx context.Context, instrIds []uuid.UUID) ([]uuid.UUID, error)
	SetStatedKeyDatedAssociation(ctx context.Context, arg gen.SetStatedKeyDatedAssociationParams) (int64, error)
	ListKeyDates(ctx context.Context, arg gen.ListKeyDatesParams) ([]gen.ListKeyDatesRow, error)
	ListInstrsByIdentifiers(ctx context.Context, arg gen.ListInstrsByIdentifiersParams) ([]gen.ListInstrsByIdentifiersRow, error)
	ListIdentifiersOf(ctx context.Context, ids []uuid.UUID) ([]gen.Identifier, error)
	ListListingsOf(ctx context.Context, ids []uuid.UUID) ([]gen.Listing, error)
	ListIdentityCoverage(ctx context.Context, instrIds []uuid.UUID) ([]gen.IdentityCoverage, error)
	LockAdvisory(ctx context.Context, names []string) error
	CreateInstr(ctx context.Context, arg gen.CreateInstrParams) (gen.Instr, error)
	CreateListing(ctx context.Context, arg gen.CreateListingParams) (gen.Listing, error)
	SetListingPrimary(ctx context.Context, arg gen.SetListingPrimaryParams) error
	CreateIdentifier(ctx context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error)
	UpsertIdentityCoverage(ctx context.Context, arg gen.UpsertIdentityCoverageParams) error
	SetFetchKeyInstr(ctx context.Context, arg gen.SetFetchKeyInstrParams) error
	CreateIdentifierAssertion(ctx context.Context, arg gen.CreateIdentifierAssertionParams) error
	RelinkIdentifierAssertions(ctx context.Context, arg gen.RelinkIdentifierAssertionsParams) error
	CreateFinding(ctx context.Context, arg gen.CreateFindingParams) error
	SetStatedKeyAssociation(ctx context.Context, arg gen.SetStatedKeyAssociationParams) (int64, error)
	CreateResolutionKey(ctx context.Context, arg gen.CreateResolutionKeyParams) (gen.ResolutionKey, error)
	DeferConstraints(ctx context.Context) error
	MoveListing(ctx context.Context, arg gen.MoveListingParams) error
	RelinkIdentifiers(ctx context.Context, arg gen.RelinkIdentifiersParams) error
	RelinkStatedKeys(ctx context.Context, arg gen.RelinkStatedKeysParams) error
	RelinkFetchKeys(ctx context.Context, arg gen.RelinkFetchKeysParams) error
	MoveIdentityCoverage(ctx context.Context, arg gen.MoveIdentityCoverageParams) error
	DeleteIdentityCoverage(ctx context.Context, instrID uuid.UUID) error
	DeleteListings(ctx context.Context, instrID uuid.UUID) error
	DeleteInstr(ctx context.Context, id uuid.UUID) error
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
	Identity(ctx context.Context, parent gen.Run, e *market.Entry, keys []gen.StatedKey) ([]fetchKey, error)
	TickerIdentity(ctx context.Context, parent gen.Run, e *market.Entry, keys []gen.StatedKey) ([]fetchKey, error)
	IdentifierFacts(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.IdentifierFactRequest) ([]factKey, error)
}

var _ Fetcher = (*market.Fetcher)(nil)
