//go:build dbtest

package market

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// TestTraits holds the traits equal to the identifier_type_traits table.
func TestTraits(t *testing.T) {
	url := os.Getenv("STONKS_TEST_DATABASE_URL")
	if url == "" {
		log.Fatal("STONKS_TEST_DATABASE_URL is not set")
	}
	ctx := context.Background()
	pool, err := db.Open(ctx, url)
	require.NoError(t, err)
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
