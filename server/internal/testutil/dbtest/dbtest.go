//go:build dbtest

// Package dbtest gives a package's integration tests the test stack's
// database, and each test a transaction that is rolled back when it ends.
package dbtest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/leedenison/stonks/server/internal/db"
)

// Main opens the stack's database into pool, runs the package's tests and
// exits with their code.
func Main(m *testing.M, pool **pgxpool.Pool) {
	*pool = Open()
	code := m.Run()
	(*pool).Close()
	os.Exit(code)
}

// Open connects to the stack's database. It stops the run when the URL is
// unset or the database is unreachable, so a package cannot pass without
// testing anything.
func Open() *pgxpool.Pool {
	url := os.Getenv("STONKS_TEST_DATABASE_URL")
	if url == "" {
		fail(errors.New("STONKS_TEST_DATABASE_URL is not set"))
	}
	pool, err := db.Open(context.Background(), url)
	if err != nil {
		fail(fmt.Errorf("open pool: %w", err))
	}
	return pool
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, err)
	os.Exit(1)
}

// Begin returns a transaction on pool that is rolled back when the test
// ends.
func Begin(t *testing.T, pool *pgxpool.Pool) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	return tx
}
