//go:build dbtest

package resolve

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) {
	url := os.Getenv("STONKS_TEST_DATABASE_URL")
	if url == "" {
		log.Fatal("STONKS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	var err error
	pool, err = db.Open(ctx, url)
	if err != nil {
		log.Fatalf("open pool: %v", err)
	}
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// TestTree holds the class tree equal to the asset_class_tree table.
func TestTree(t *testing.T) {
	rows, err := gen.New(pool).ListAssetClassTree(context.Background())
	require.NoError(t, err)
	got := map[gen.AssetClass]gen.AssetClass{}
	for _, r := range rows {
		var parent gen.AssetClass
		if r.Parent != nil {
			parent = *r.Parent
		}
		got[r.Class] = parent
	}
	if diff := cmp.Diff(parents, got); diff != "" {
		t.Errorf("asset class tree mismatch (-code +table):\n%s", diff)
	}
}
