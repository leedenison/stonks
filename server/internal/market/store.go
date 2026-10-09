package market

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=market -self_package=github.com/leedenison/stonks/server/internal/market

import (
	"context"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListDatasources(ctx context.Context) ([]gen.Datasource, error)
	ListOpenBlocks(ctx context.Context, arg gen.ListOpenBlocksParams) ([]gen.DatasourceBlock, error)
	CreateFetch(ctx context.Context, arg gen.CreateFetchParams) (gen.Fetch, error)
	CreateFetchKey(ctx context.Context, arg gen.CreateFetchKeyParams) error
	CreateDatasourceBlock(ctx context.Context, arg gen.CreateDatasourceBlockParams) (int64, error)
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Cache holds a served answer under the key of the request that produced
// it.
type Cache interface {
	// Get returns the entry of each key, nil where the key has none.
	Get(ctx context.Context, keys []string) ([][]byte, error)
	// Set stores value under key for the cache's lifetime.
	Set(ctx context.Context, key string, value []byte) error
	// Drop removes every entry of datasource.
	Drop(ctx context.Context, datasource string) error
}

// Runner is this package's view of the run framework.
type Runner interface {
	Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)
