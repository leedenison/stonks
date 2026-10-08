//go:build dbtest

package eodhd

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
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

	for code, ms := range codes {
		for _, m := range ms {
			if _, ok := tbl.Operating(m); !ok {
				t.Errorf("code %s names %s, which is not in the MIC reference table", code, m)
			}
		}
	}
	for name, m := range usVenues {
		if _, ok := tbl.Operating(m); !ok {
			t.Errorf("US venue %s is %s, which is not in the MIC reference table", name, m)
		}
	}
}
