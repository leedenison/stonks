package statement

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=statement -self_package=github.com/leedenison/stonks/server/internal/statement

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/group"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	group.Queries
	CreateStatement(ctx context.Context, arg gen.CreateStatementParams) (gen.Statement, error)
	CreateStatedKey(ctx context.Context, arg gen.CreateStatedKeyParams) (gen.StatedKey, error)
	CreateStatementSplit(ctx context.Context, arg gen.CreateStatementSplitParams) error
	ListCurrencies(ctx context.Context) ([]gen.Currency, error)
	LockUserKeys(ctx context.Context, userID uuid.UUID) error
	DeleteTransactions(ctx context.Context, arg gen.DeleteTransactionsParams) (int64, error)
	CreateTransaction(ctx context.Context, arg gen.CreateTransactionParams) (gen.Transaction, error)
	CreateStatementItem(ctx context.Context, arg gen.CreateStatementItemParams) error
	CompleteRun(ctx context.Context, id uuid.UUID) error
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Runner is this package's view of the run framework.
type Runner interface {
	Start(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error)
	Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// Resolver is this package's view of the resolve package: the
// body of a resolution run over the stated keys of a statement, with an
// outcome per key in their order.
type Resolver interface {
	Resolve(ctx context.Context, res gen.Run, keys []gen.StatedKey) ([]gen.ResolutionKey, error)
}

var _ Resolver = (*resolve.Resolver)(nil)
