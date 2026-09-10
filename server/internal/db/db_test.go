//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
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
		log.Fatalf("open: %v", err)
	}
	code := m.Run()
	pool.Close()
	os.Exit(code)
}

// newTx returns queries over a transaction that is rolled back when the test
// ends.
func newTx(t *testing.T) *gen.Queries {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	return gen.New(tx)
}

func TestUsers(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()

	created, err := q.CreateUser(ctx, gen.CreateUserParams{Email: "One@example.com", Name: "One", Role: gen.UserRoleUser})
	require.NoError(t, err)
	if created.GoogleSubject != nil {
		t.Errorf("CreateUser google subject = %q, want nil", *created.GoogleSubject)
	}
	if created.CreatedAt.IsZero() {
		t.Error("CreateUser created_at is zero")
	}

	got, err := q.GetUserByEmail(ctx, "one@EXAMPLE.com")
	require.NoError(t, err)
	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("GetUserByEmail mismatch (-want +got):\n%s", diff)
	}

	_, err = q.GetUser(ctx, uuid.New())
	if !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetUser of an unknown id: err = %v, want ErrNotFound", err)
	}

	// The unique violation aborts the transaction, so it is the last statement.
	_, err = q.CreateUser(ctx, gen.CreateUserParams{Email: "ONE@example.com", Role: gen.UserRoleUser})
	var pgErr *pgconn.PgError
	// 23505 is unique_violation.
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("CreateUser with a case variant of an existing email: err = %v, want unique_violation", err)
	}
}
