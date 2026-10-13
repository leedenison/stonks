//go:build dbtest

package mirror

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// TestTraits holds the traits equal to the identifier_type_traits table.
func TestTraits(t *testing.T) {
	rows, err := gen.New(pool).ListIdentifierTypeTraits(context.Background())
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

// TestTree holds the class tree equal to the asset_class_tree table.
func TestTree(t *testing.T) {
	rows, err := gen.New(pool).ListAssetClassTree(context.Background())
	require.NoError(t, err)
	got := map[types.AssetClass]types.AssetClass{}
	for _, r := range rows {
		var parent types.AssetClass
		if r.Parent != nil {
			parent = *r.Parent
		}
		got[r.Class] = parent
	}
	if diff := cmp.Diff(parents, got); diff != "" {
		t.Errorf("asset class tree mismatch (-code +table):\n%s", diff)
	}
}
