//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/testutil/metrictest"
)

var reader = metrictest.Install()

// users is the view a consumer of the transaction helper would declare.
type users interface {
	CreateUser(ctx context.Context, arg gen.CreateUserParams) (gen.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (gen.User, error)
}

// TestTx checks that a transaction commits what fn wrote when fn returns nil
// and discards it otherwise, nested under the test's own transaction.
func TestTx(t *testing.T) {
	d := db.New[users](begin(t))
	ctx := context.Background()
	create := func(q users, email string) uuid.UUID {
		t.Helper()
		u, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: email, Role: gen.UserRoleUser})
		require.NoError(t, err)
		return u.ID
	}

	var committed uuid.UUID
	err := d.Tx(ctx, func(q users) error {
		committed = create(q, "tx-committed@example.com")
		return nil
	})
	require.NoError(t, err)
	if _, err := d.GetUser(ctx, committed); err != nil {
		t.Errorf("GetUser after a committed Tx: err = %v, want the row", err)
	}

	boom := errors.New("boom")
	var discarded uuid.UUID
	err = d.Tx(ctx, func(q users) error {
		discarded = create(q, "tx-discarded@example.com")
		return boom
	})
	if !errors.Is(err, boom) {
		t.Errorf("Tx returning an error: err = %v, want %v", err, boom)
	}
	if _, err := d.GetUser(ctx, discarded); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetUser after a rolled back Tx: err = %v, want ErrNotFound", err)
	}
}

// retries returns the retries Tx counted since the last call, by cause.
func retries(t *testing.T) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for name, n := range metrictest.Counts(t, reader) {
		if strings.HasPrefix(name, "stonks.db.tx_retries") {
			out[name] = n
		}
	}
	return out
}

// TestTxRetry checks that Tx runs fn again when the database refuses an
// attempt because of a concurrent transaction, up to three attempts, and
// that any other failure ends it.
func TestTxRetry(t *testing.T) {
	ctx := context.Background()
	refused := func(code string) error { return &pgconn.PgError{Code: code} }
	boom := errors.New("boom")
	tests := []struct {
		name    string
		errs    []error
		wantErr error
		retries map[string]int64
	}{
		{name: "a unique violation then success", errs: []error{refused(pgerrcode.UniqueViolation), nil}, retries: map[string]int64{"stonks.db.tx_retries{cause=unique_violation}": 1}},
		{name: "a deadlock then success", errs: []error{refused(pgerrcode.DeadlockDetected), nil}, retries: map[string]int64{"stonks.db.tx_retries{cause=deadlock}": 1}},
		{name: "a serialization failure then success", errs: []error{refused(pgerrcode.SerializationFailure), nil}, retries: map[string]int64{"stonks.db.tx_retries{cause=serialization_failure}": 1}},
		{name: "a concurrent write then success", errs: []error{fmt.Errorf("merge moved ticker: %w", db.ErrConcurrent), nil}, retries: map[string]int64{"stonks.db.tx_retries{cause=concurrent_write}": 1}},
		{name: "refused every time", errs: []error{refused(pgerrcode.DeadlockDetected), refused(pgerrcode.DeadlockDetected), refused(pgerrcode.DeadlockDetected)}, wantErr: refused(pgerrcode.DeadlockDetected), retries: map[string]int64{"stonks.db.tx_retries{cause=deadlock}": 2}},
		{name: "another failure", errs: []error{boom}, wantErr: boom, retries: map[string]int64{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			d := db.New[users](begin(t))
			retries(t)
			runs := 0
			err := d.Tx(ctx, func(users) error {
				err := tc.errs[runs]
				runs++
				return err
			})
			if runs != len(tc.errs) {
				t.Errorf("fn ran %d times, want %d", runs, len(tc.errs))
			}
			switch {
			case tc.wantErr == nil && err != nil:
				t.Errorf("Tx() error = %v, want nil", err)
			case tc.wantErr != nil && (err == nil || err.Error() != tc.wantErr.Error()):
				t.Errorf("Tx() error = %v, want %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.retries, retries(t)); diff != "" {
				t.Errorf("retries mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestTxRetryUniqueViolation checks that an attempt the unique index on
// email refuses is rolled back and the next attempt commits.
func TestTxRetryUniqueViolation(t *testing.T) {
	ctx := context.Background()
	d := db.New[users](begin(t))
	taken := uuid.NewString() + "@example.com"
	require.NoError(t, d.Tx(ctx, func(q users) error {
		_, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: taken, Role: gen.UserRoleUser})
		return err
	}))
	runs := 0
	var id uuid.UUID
	err := d.Tx(ctx, func(q users) error {
		runs++
		email := taken
		if runs > 1 {
			email = uuid.NewString() + "@example.com"
		}
		u, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: email, Role: gen.UserRoleUser})
		id = u.ID
		return err
	})
	require.NoError(t, err)
	if runs != 2 {
		t.Errorf("fn ran %d times, want 2", runs)
	}
	if _, err := d.GetUser(ctx, id); err != nil {
		t.Errorf("GetUser after the retried Tx: err = %v, want the row", err)
	}
}

// datasources is the view the commit case declares.
type datasources interface {
	CreateDatasource(ctx context.Context, arg gen.CreateDatasourceParams) (gen.Datasource, error)
}

// TestTxRetryAtCommit checks that Tx also retries a unique violation that a
// deferred constraint raises at commit. It runs on the pool, because a
// savepoint release leaves deferred constraints unchecked. Every attempt
// fails, so nothing is committed.
func TestTxRetryAtCommit(t *testing.T) {
	ctx := context.Background()
	d := db.New[datasources](pool)
	precedence := int32(1_000_000_000 + uuid.New().ID()%1_000_000_000)
	retries(t)
	runs := 0
	err := d.Tx(ctx, func(q datasources) error {
		runs++
		for range 2 {
			arg := gen.CreateDatasourceParams{Name: "tx-" + uuid.NewString(), Precedence: precedence}
			if _, err := q.CreateDatasource(ctx, arg); err != nil {
				return err
			}
		}
		return nil
	})
	if !db.IsConflict(err) {
		t.Fatalf("Tx() error = %v, want a unique violation", err)
	}
	if runs != 3 {
		t.Errorf("fn ran %d times, want 3", runs)
	}
	want := map[string]int64{"stonks.db.tx_retries{cause=unique_violation}": 2}
	if diff := cmp.Diff(want, retries(t)); diff != "" {
		t.Errorf("retries mismatch (-want +got):\n%s", diff)
	}
}

// TestTxRetryDeadlock checks that two transactions that take advisory locks
// in opposite orders both commit. Postgres refuses one of them, and Tx runs
// that one again once the other has committed.
func TestTxRetryDeadlock(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	a, b := uuid.NewString(), uuid.NewString()
	held := [2]chan struct{}{make(chan struct{}), make(chan struct{})}
	var runs atomic.Int32
	retries(t)
	run := func(i int, own, other string) error {
		first := true
		return db.New[db.AdvisoryLocker](pool).Tx(ctx, func(q db.AdvisoryLocker) error {
			runs.Add(1)
			if err := q.LockAdvisory(ctx, []string{own}); err != nil {
				return err
			}
			if first {
				first = false
				close(held[i])
				select {
				case <-held[1-i]:
				case <-ctx.Done():
					return ctx.Err()
				}
			}
			return q.LockAdvisory(ctx, []string{other})
		})
	}
	done := make(chan error, 2)
	go func() { done <- run(0, a, b) }()
	go func() { done <- run(1, b, a) }()
	for range 2 {
		if err := <-done; err != nil {
			t.Errorf("Tx() error = %v, want nil", err)
		}
	}
	if got := runs.Load(); got != 3 {
		t.Errorf("fn ran %d times, want 3", got)
	}
	want := map[string]int64{"stonks.db.tx_retries{cause=deadlock}": 1}
	if diff := cmp.Diff(want, retries(t)); diff != "" {
		t.Errorf("retries mismatch (-want +got):\n%s", diff)
	}
}

// TestNewPanics checks that a view the queries do not satisfy is refused at
// wiring.
func TestNewPanics(t *testing.T) {
	type missing interface{ Missing() }
	defer func() {
		if recover() == nil {
			t.Error("New over an unsatisfied view did not panic")
		}
	}()
	db.New[missing](begin(t))
}
