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

// begin returns a transaction that is rolled back when the test ends.
func begin(t *testing.T) pgx.Tx {
	t.Helper()
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	t.Cleanup(func() {
		if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
			t.Errorf("rollback: %v", err)
		}
	})
	return tx
}

// newTx returns queries over a transaction that is rolled back when the test
// ends.
func newTx(t *testing.T) *gen.Queries {
	t.Helper()
	return gen.New(begin(t))
}

func TestUsers(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()

	id := db.NewID()
	created, err := q.CreateUser(ctx, gen.CreateUserParams{ID: id, Email: "One@example.com", Name: "One", Role: gen.UserRoleUser})
	require.NoError(t, err)
	if created.ID != id {
		t.Errorf("CreateUser id = %s, want %s", created.ID, id)
	}
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
	_, err = q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: "ONE@example.com", Role: gen.UserRoleUser})
	var pgErr *pgconn.PgError
	// 23505 is unique_violation.
	if !errors.As(err, &pgErr) || pgErr.Code != "23505" {
		t.Errorf("CreateUser with a case variant of an existing email: err = %v, want unique_violation", err)
	}
}

// TestUUIDv7 checks the key the schema mints for SQL that supplies none.
func TestUUIDv7(t *testing.T) {
	ctx := context.Background()
	var a, b uuid.UUID
	require.NoError(t, pool.QueryRow(ctx, "SELECT uuid_v7()").Scan(&a))
	require.NoError(t, pool.QueryRow(ctx, "SELECT uuid_v7()").Scan(&b))
	if a.Version() != 7 {
		t.Errorf("uuid_v7() version = %d, want 7", a.Version())
	}
	if a.Variant() != uuid.RFC4122 {
		t.Errorf("uuid_v7() variant = %s, want RFC4122", a.Variant())
	}
	if a.Time() > b.Time() {
		t.Errorf("uuid_v7() minted %s then %s, want the first no later", a, b)
	}
}
