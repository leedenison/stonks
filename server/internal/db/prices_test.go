//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// newPrice records March 2026 as covered by f's datasource for a new USD
// listing, under a served key of f's fetch, with one price on 2 March.
func newPrice(t *testing.T, q *gen.Queries, f fetching) (coverage uuid.UUID) {
	t.Helper()
	ctx := context.Background()
	listing := newListing(t, q, newInstr(t, q, types.AssetClassStock), "USD")
	coverage = db.NewID()
	require.NoError(t, q.CreatePriceCoverage(ctx, gen.CreatePriceCoverageParams{
		ID: coverage, ListingID: listing.ID, Datasource: f.ds.Name, FetchKeyID: servedKey(t, q, f.fetch, f.key, "US67066G1040"),
		CoveredFrom: date(2026, 3, 1), CoveredBefore: date(2026, 4, 1),
	}))
	n, err := q.CreatePrices(ctx, []gen.CreatePricesParams{
		{CoverageID: coverage, Day: date(2026, 3, 2), Price: decimal.RequireFromString("143.00"), AsAt: date(2026, 3, 2)},
	})
	require.NoError(t, err)
	require.EqualValues(t, 1, n)
	return coverage
}

func TestPriceChecks(t *testing.T) {
	ctx := context.Background()
	const cover = "INSERT INTO price_coverage (id, listing_id, datasource, fetch_key_id, covered_from, covered_before) SELECT $1, listing_id, datasource, $2, "
	tests := []struct {
		name string
		sql  string
		code string
	}{
		{name: "coverage that ends as it starts", sql: cover + "'2026-05-01', '2026-05-01' FROM price_coverage WHERE id = $3", code: pgerrcode.CheckViolation},
		{name: "coverage that overlaps the datasource's own", sql: cover + "'2026-03-31', '2026-05-01' FROM price_coverage WHERE id = $3", code: pgerrcode.ExclusionViolation},
		{name: "coverage that meets the datasource's own", sql: cover + "'2026-04-01', '2026-05-01' FROM price_coverage WHERE id = $3"},
		{name: "a second price for a day", sql: "INSERT INTO prices (coverage_id, day, price, as_at) SELECT $3, '2026-03-02', 1, '2026-03-02' WHERE $1::uuid <> $2", code: pgerrcode.UniqueViolation},
		{name: "a price of zero", sql: "INSERT INTO prices (coverage_id, day, price, as_at) SELECT $3, '2026-03-03', 0, '2026-03-03' WHERE $1::uuid <> $2", code: pgerrcode.CheckViolation},
		{name: "a price as at a day before its own", sql: "INSERT INTO prices (coverage_id, day, price, as_at) SELECT $3, '2026-03-03', 1, '2026-03-02' WHERE $1::uuid <> $2", code: pgerrcode.CheckViolation},
		{name: "a price as at a later day", sql: "INSERT INTO prices (coverage_id, day, price, as_at) SELECT $3, '2026-03-03', 1, '2026-06-01' WHERE $1::uuid <> $2"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := begin(t)
			q := gen.New(tx)
			f := newFetching(t, q)
			coverage := newPrice(t, q, f)
			key := servedKey(t, q, newFetch(t, q, f.user, f.run, f.ds), f.key, "US0378331005")
			_, err := tx.Exec(ctx, tc.sql, db.NewID(), key, coverage)
			if tc.code == "" {
				require.NoError(t, err)
			} else if !sqlstate(err, tc.code) {
				t.Errorf("err = %v, want %s", err, tc.code)
			}
		})
	}
}

// TestPriceCoverageOfAnotherDatasource checks that two datasources may cover
// one day of one listing.
func TestPriceCoverageOfAnotherDatasource(t *testing.T) {
	ctx := context.Background()
	tx := begin(t)
	q := gen.New(tx)
	f := newFetching(t, q)
	coverage := newPrice(t, q, f)
	other := newDatasource(t, q, "other", 20)
	key := servedKey(t, q, newFetch(t, q, f.user, f.run, other), f.key, "US0378331005")
	_, err := tx.Exec(ctx, "INSERT INTO price_coverage (id, listing_id, datasource, fetch_key_id, covered_from, covered_before) SELECT $1, listing_id, $2, $3, covered_from, covered_before FROM price_coverage WHERE id = $4",
		db.NewID(), other.Name, key, coverage)
	require.NoError(t, err)
}

// TestDeleteListingCascadesPrices checks that deleting a listing deletes its
// price coverage and prices.
func TestDeleteListingCascadesPrices(t *testing.T) {
	ctx := context.Background()
	tx := begin(t)
	q := gen.New(tx)
	coverage := newPrice(t, q, newFetching(t, q))
	_, err := tx.Exec(ctx, "DELETE FROM listings WHERE id = (SELECT listing_id FROM price_coverage WHERE id = $1)", coverage)
	require.NoError(t, err)
	for _, table := range []string{"price_coverage", "prices"} {
		var n int
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
		if n != 0 {
			t.Errorf("%s has %d rows after the listing was deleted, want none", table, n)
		}
	}
}
