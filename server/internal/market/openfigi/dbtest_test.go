//go:build dbtest

package openfigi

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
)

// TestCodesAreMICs checks that every venue an exchange code names is in the
// MIC reference table.
func TestCodesAreMICs(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tbl, err := mic.Load(ctx, gen.New(pool))
	require.NoError(t, err)

	for code, ms := range codes {
		for _, m := range ms {
			if op, ok := tbl.Operating(m); !ok || op != m {
				t.Errorf("code %s names %s, want an operating MIC of the reference table, got %q, %v", code, m, op, ok)
			}
		}
	}
}
