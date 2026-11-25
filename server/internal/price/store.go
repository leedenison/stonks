package price

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
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
