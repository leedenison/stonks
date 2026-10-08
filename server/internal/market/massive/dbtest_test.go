//go:build dbtest

package massive

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
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

	for v := range venues {
		if _, ok := tbl.Operating(v); !ok {
			t.Errorf("venue %s is not in the MIC reference table", v)
		}
	}
}
