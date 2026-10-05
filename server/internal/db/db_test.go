//go:build dbtest

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

// begin returns a transaction that is rolled back when the test ends.
func begin(t *testing.T) pgx.Tx {
	t.Helper()
	return dbtest.Begin(t, pool)
}

// explainer runs each query of a transaction and records the plan of the
// last one, so a test asserts the plan of the query sqlc generated.
type explainer struct {
	pgx.Tx
	plan string
}

func (e *explainer) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	if err := e.Tx.QueryRow(ctx, "EXPLAIN (FORMAT JSON) "+sql, args...).Scan(&e.plan); err != nil {
		return nil, err
	}
	return e.Tx.Query(ctx, sql, args...)
}

// indexConds returns the Index Cond of every plan node that scans index. A
// condition the index cannot serve shows as the node's filter, so it is
// absent here.
func indexConds(t *testing.T, plan, index string) []string {
	t.Helper()
	type node struct {
		IndexName string `json:"Index Name"`
		IndexCond string `json:"Index Cond"`
		Plans     []node `json:"Plans"`
	}
	var root []struct {
		Plan node `json:"Plan"`
	}
	require.NoError(t, json.Unmarshal([]byte(plan), &root))
	var out []string
	var walk func(n node)
	walk = func(n node) {
		if n.IndexName == index {
			out = append(out, n.IndexCond)
		}
		for _, c := range n.Plans {
			walk(c)
		}
	}
	for _, r := range root {
		walk(r.Plan)
	}
	return out
}

// explain returns queries over a rolled-back transaction with sequential
// scans disabled, and the explainer that records their plans.
func explain(t *testing.T) (*gen.Queries, *explainer) {
	t.Helper()
	tx := begin(t)
	_, err := tx.Exec(context.Background(), "SET LOCAL enable_seqscan = off")
	require.NoError(t, err)
	e := &explainer{Tx: tx}
	return gen.New(e), e
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
