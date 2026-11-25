package price

import (
	"context"

	"github.com/google/uuid"

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
