//go:build dbtest

package market

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
)

// TestTraits holds the traits equal to the identifier_type_traits table.
func TestTraits(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	defer pool.Close()
	rows, err := gen.New(pool).ListIdentifierTypeTraits(ctx)
	require.NoError(t, err)
	got := map[types.IdentifierType]gen.IdentifierTypeTrait{}
	for _, r := range rows {
		got[r.Type] = r
	}
	want := map[types.IdentifierType]gen.IdentifierTypeTrait{}
	for typ := range traits {
		want[typ] = Trait(typ)
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("traits mismatch (-code +table):\n%s", diff)
	}
}

// TestOpenFIGICodesAreMICs checks that every venue an exchange code names is in the
// MIC reference table.
func TestOpenFIGICodesAreMICs(t *testing.T) {
	ctx := context.Background()
	pool := dbtest.Open()
	t.Cleanup(pool.Close)
	tbl, err := mic.Load(ctx, gen.New(pool))
	require.NoError(t, err)

	for code, ms := range openfigiCodes {
		for _, m := range ms {
			if op, ok := tbl.Operating(m); !ok || op != m {
				t.Errorf("code %s names %s, want an operating MIC of the reference table, got %q, %v", code, m, op, ok)
			}
		}
	}
}
