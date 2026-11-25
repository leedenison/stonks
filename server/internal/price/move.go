package price

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Move moves the coverage and prices of the listing from onto the listing
// to, for each day that the same datasource has not covered on to. The rest
// stays on from. The caller runs it in the transaction that deletes from.
func Move(ctx context.Context, q MoveQueries, from, to uuid.UUID) error {
	rows, err := q.ListPriceCoverage(ctx, []uuid.UUID{from})
	if err != nil {
		return fmt.Errorf("list price coverage: %w", err)
	}
	if len(rows) == 0 {
		return nil
	}
	sources := map[string]bool{}
	for _, r := range rows {
		sources[r.Datasource] = true
	}
	for _, source := range slices.Sorted(maps.Keys(sources)) {
		if err := db.LockPrices(ctx, q, source, []uuid.UUID{to}); err != nil {
			return err
		}
	}
	held, err := q.ListPriceCoverage(ctx, []uuid.UUID{to})
	if err != nil {
		return fmt.Errorf("list price coverage: %w", err)
	}
	for _, r := range rows {
		whole := types.Period{From: r.CoveredFrom, Before: r.CoveredBefore}
		kept := types.Gaps(periods(held, r.Datasource), whole)
		if len(kept) == 1 && kept[0] == whole {
			if err := q.MovePriceCoverage(ctx, gen.MovePriceCoverageParams{ID: r.ID, ListingID: to}); err != nil {
				return fmt.Errorf("move price coverage: %w", err)
			}
			continue
		}
		for _, k := range kept {
			id := db.NewID()
			if err := q.CreatePriceCoverage(ctx, gen.CreatePriceCoverageParams{
				ID: id, ListingID: to, Datasource: r.Datasource, FetchKeyID: r.FetchKeyID, CoveredFrom: k.From, CoveredBefore: k.Before,
			}); err != nil {
				return fmt.Errorf("create price coverage: %w", err)
			}
			if err := q.MovePrices(ctx, gen.MovePricesParams{From: r.ID, To: id, Day: k.From, Before: k.Before}); err != nil {
				return fmt.Errorf("move prices: %w", err)
			}
		}
	}
	return nil
}
