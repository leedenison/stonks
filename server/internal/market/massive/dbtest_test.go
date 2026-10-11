//go:build dbtest

package massive

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

// TestVenuesAreMICs checks that every venue the table names is in the MIC
// reference table.
func TestVenuesAreMICs(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tbl, err := mic.Load(ctx, gen.New(pool))
	require.NoError(t, err)
	markettest.VenuesAreMICs(t, tbl, slices.Collect(maps.Keys(venues))...)
}
