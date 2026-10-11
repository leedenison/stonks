//go:build dbtest

package eodhd

import (
	"context"
	"maps"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market/markettest"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
)

// TestVenuesAreMICs checks that every MIC the tables name is in the MIC
// reference table.
func TestVenuesAreMICs(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tbl, err := mic.Load(ctx, gen.New(pool))
	require.NoError(t, err)
	var venues []string
	for _, ms := range codes {
		venues = append(venues, ms...)
	}
	markettest.VenuesAreMICs(t, tbl, slices.Concat(venues, slices.Collect(maps.Values(usVenues)))...)
}
