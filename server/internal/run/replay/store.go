package replay

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=replay -self_package=github.com/leedenison/stonks/server/internal/run/replay

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/group"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	group.Queries
	ListUnavailableKeys(ctx context.Context, arg gen.ListUnavailableKeysParams) ([]gen.ListUnavailableKeysRow, error)
	ListKeysUncoveredBy(ctx context.Context, arg gen.ListKeysUncoveredByParams) ([]gen.ListKeysUncoveredByRow, error)
	CreateReplay(ctx context.Context, arg gen.CreateReplayParams) error
	ListUnassociatedKeysStating(ctx context.Context, arg gen.ListUnassociatedKeysStatingParams) ([]gen.StatedKey, error)
	LockUserKeys(ctx context.Context, userID uuid.UUID) error
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
	ChildAs(ctx context.Context, parent gen.Run, user *uuid.UUID, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// Resolver is this package's view of the resolve package: the body of a
// resolution run over stated keys.
type Resolver interface {
	Resolve(ctx context.Context, res gen.Run, keys []resolve.KeyDates) ([]gen.ResolutionKey, []resolve.WrittenIdentifierFacts, error)
}

var _ Resolver = (*resolve.Resolver)(nil)

// CorpActs is this package's view of the refreshcorpacts
// package.
type CorpActs interface {
	FetchAfterIdentifierFacts(ctx context.Context, parent gen.Run, written []resolve.WrittenIdentifierFacts)
}

// Sources is this package's view of the datasource registry.
type Sources interface {
	Enabled() []*market.Entry
}

var _ Sources = (*market.Registry)(nil)
