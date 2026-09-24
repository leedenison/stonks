//go:build dbtest

package openfigi

import (
	"context"
	"os"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
)

// TestCodesAreMICs checks that every venue an exchange code names is in the
// MIC reference table.
func TestCodesAreMICs(t *testing.T) {
	url := os.Getenv("STONKS_TEST_DATABASE_URL")
	if url == "" {
		t.Fatal("STONKS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	require.NoError(t, err)
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
