//go:build dbtest

package group

import (
	"context"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/types"
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

// TestDomained holds domained equal to the identifier_type_traits table.
func TestDomained(t *testing.T) {
	ctx := context.Background()
	rows, err := pool.Query(ctx, "SELECT type FROM identifier_type_traits WHERE domain <> 'global'")
	require.NoError(t, err)
	defer rows.Close()
	want := map[types.IdentifierType]bool{}
	for rows.Next() {
		var typ types.IdentifierType
		require.NoError(t, rows.Scan(&typ))
		want[typ] = true
	}
	require.NoError(t, rows.Err())
	if diff := cmp.Diff(want, domained); diff != "" {
		t.Errorf("domained mismatch (-want +got):\n%s", diff)
	}
}
