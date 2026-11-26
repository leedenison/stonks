package price

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=price -self_package=github.com/leedenison/stonks/server/internal/price

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

// MoveQueries are the queries Move needs.
type MoveQueries interface {
	db.AdvisoryLocker
	ListPriceCoverage(ctx context.Context, listingIds []uuid.UUID) ([]gen.PriceCoverage, error)
	CreatePriceCoverage(ctx context.Context, arg gen.CreatePriceCoverageParams) error
	MovePriceCoverage(ctx context.Context, arg gen.MovePriceCoverageParams) error
	MovePrices(ctx context.Context, arg gen.MovePricesParams) error
}

var _ MoveQueries = (*gen.Queries)(nil)

// RecordQueries are the queries Record needs.
type RecordQueries interface {
	db.AdvisoryLocker
	GetListingByID(ctx context.Context, id uuid.UUID) (gen.Listing, error)
	ListPriceCoverage(ctx context.Context, listingIds []uuid.UUID) ([]gen.PriceCoverage, error)
	CreatePriceCoverage(ctx context.Context, arg gen.CreatePriceCoverageParams) error
	CreatePrices(ctx context.Context, arg []gen.CreatePricesParams) (int64, error)
}

var _ RecordQueries = (*gen.Queries)(nil)

// DemandQueries are the queries a Demand needs.
type DemandQueries interface {
	corpact.CalendarQueries
	ListHoldingKeys(ctx context.Context, userID *uuid.UUID) ([]gen.ListHoldingKeysRow, error)
	ListInstrsByID(ctx context.Context, ids []uuid.UUID) ([]gen.Instr, error)
	ListListingNames(ctx context.Context, instrIds []uuid.UUID) ([]gen.ListListingNamesRow, error)
	ListRateListings(ctx context.Context) ([]gen.ListRateListingsRow, error)
	CreateRateListings(ctx context.Context, arg gen.CreateRateListingsParams) error
	ListPriceCoverage(ctx context.Context, listingIds []uuid.UUID) ([]gen.PriceCoverage, error)
}

var _ DemandQueries = (*gen.Queries)(nil)

// Store is the database as a Recorder sees it: the queries of a write, and
// Tx, which runs a set of them in one transaction.
type Store interface {
	RecordQueries
	Tx(ctx context.Context, fn func(RecordQueries) error) error
}

var _ Store = (*db.DB[RecordQueries])(nil)

// UploadQueries are the queries of one uploaded series.
type UploadQueries interface {
	RecordQueries
	CreateFetchKey(ctx context.Context, arg gen.CreateFetchKeyParams) error
}

var _ UploadQueries = (*gen.Queries)(nil)

// UploadStore is the database as an Uploader sees it.
type UploadStore interface {
	CreateFetch(ctx context.Context, arg gen.CreateFetchParams) (gen.Fetch, error)
	ListRateListings(ctx context.Context) ([]gen.ListRateListingsRow, error)
	GetRunByID(ctx context.Context, id uuid.UUID) (gen.Run, error)
	Tx(ctx context.Context, fn func(UploadQueries) error) error
}

var _ UploadStore = (*db.DB[UploadQueries])(nil)

// Runner is this package's view of the run framework.
type Runner interface {
	Sync(ctx context.Context, user *uuid.UUID, kind gen.RunKind, trigger gen.RunTrigger, work run.Work) (gen.Run, error)
	ChildAs(ctx context.Context, parent gen.Run, user *uuid.UUID, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// Fetcher is this package's view of the fetch framework.
type Fetcher interface {
	Prices(ctx context.Context, parent gen.Run, e *market.Entry, reqs []market.PriceRequest) ([]priceKey, error)
}

var _ Fetcher = (*market.Fetcher)(nil)

// ReadQueries are the queries Read needs.
type ReadQueries interface {
	corpact.CalendarQueries
	ListListingClasses(ctx context.Context, ids []uuid.UUID) ([]gen.ListListingClassesRow, error)
	ListPrices(ctx context.Context, arg gen.ListPricesParams) ([]gen.ListPricesRow, error)
	ListPricesBefore(ctx context.Context, arg gen.ListPricesBeforeParams) ([]gen.ListPricesBeforeRow, error)
	ListPriceCoverage(ctx context.Context, listingIds []uuid.UUID) ([]gen.PriceCoverage, error)
}

var _ ReadQueries = (*gen.Queries)(nil)
