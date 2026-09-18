package db

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Conn is what a DB is opened over: the pool, or a transaction in a test,
// under which Tx nests as a savepoint.
type Conn interface {
	gen.DBTX
	Begin(ctx context.Context) (pgx.Tx, error)
}

// DB is the queries over a connection, and Tx, which runs a consumer's view
// Q of them in one transaction. Q is an interface *gen.Queries satisfies, so
// the consumer mocks Q and never sees a transaction type.
type DB[Q any] struct {
	*gen.Queries
	conn Conn
}

// New returns a DB over conn. It panics if *gen.Queries does not satisfy Q,
// at wiring rather than on the first Tx.
func New[Q any](conn Conn) *DB[Q] {
	q := gen.New(conn)
	if _, ok := any(q).(Q); !ok {
		panic(fmt.Sprintf("db: *gen.Queries does not satisfy %T", *new(Q)))
	}
	return &DB[Q]{Queries: q, conn: conn}
}

// Tx runs fn over queries bound to one transaction, committing when fn
// returns nil and rolling back otherwise.
func (d *DB[Q]) Tx(ctx context.Context, fn func(Q) error) (err error) {
	tx, err := d.conn.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer func() {
		if rerr := tx.Rollback(ctx); rerr != nil && !errors.Is(rerr, pgx.ErrTxClosed) {
			err = errors.Join(err, fmt.Errorf("rollback: %w", rerr))
		}
	}()
	if err := fn(any(d.WithTx(tx)).(Q)); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}
