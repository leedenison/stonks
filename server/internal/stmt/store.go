package stmt

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=stmt -self_package=github.com/leedenison/stonks/server/internal/stmt

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/group"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	group.Queries
	CreateStmt(ctx context.Context, arg gen.CreateStmtParams) (gen.Stmt, error)
	UpsertStatedKey(ctx context.Context, arg gen.UpsertStatedKeyParams) (gen.StatedKey, error)
	CreateStmtKeys(ctx context.Context, arg []gen.CreateStmtKeysParams) (int64, error)
	CreateStmtSplit(ctx context.Context, arg gen.CreateStmtSplitParams) error
	LockUserKeys(ctx context.Context, userID uuid.UUID) error
	DeleteTxns(ctx context.Context, arg gen.DeleteTxnsParams) (int64, error)
	CreateTxns(ctx context.Context, arg []gen.CreateTxnsParams) (int64, error)
	CreateStmtItems(ctx context.Context, arg []gen.CreateStmtItemsParams) (int64, error)
	SweepStatedKeys(ctx context.Context, userID uuid.UUID) (int64, error)
	CompleteRun(ctx context.Context, id uuid.UUID) error
	SettleStmt(ctx context.Context, id uuid.UUID) error
	GetStatedKey(ctx context.Context, arg gen.GetStatedKeyParams) (gen.StatedKey, error)
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
	Sync(ctx context.Context, user uuid.UUID, kind gen.RunKind, trigger gen.RunTrigger, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// Resolver is this package's view of the resolve package: the body of a
// resolve statement keys run over the keys a statement states that lack a fixed
// association, with an outcome per key in their order, and the two
// synchronous resolutions of one key. A run and a confirmation also return
// the identifiers of the identifier facts they wrote.
type Resolver interface {
	Resolve(ctx context.Context, res gen.Run, keys []resolve.KeyDates) ([]gen.ResolutionKey, []resolve.WrittenIdentifierFacts, error)
	Candidates(ctx context.Context, res gen.Run, key gen.StatedKey) (resolve.Offer, error)
	Confirm(ctx context.Context, res gen.Run, key gen.StatedKey, p resolve.Pick) (gen.ResolutionKey, []resolve.WrittenIdentifierFacts, error)
}

var _ Resolver = (*resolve.Resolver)(nil)

// Facts is this package's view of the replay service: what new identifier
// facts start, and what follows a statement.
type Facts interface {
	AfterIdentifierFacts(ctx context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts)
	AfterStmt(ctx context.Context, stmt, except gen.Run, written []resolve.WrittenIdentifierFacts)
}
