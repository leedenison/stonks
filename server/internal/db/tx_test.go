//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

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
