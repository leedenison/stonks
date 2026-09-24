package datasource

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=datasource -self_package=github.com/leedenison/stonks/server/internal/datasource

import (
	"context"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run"
)

// Store is the view of the generated queries this package depends on. No
// write of a fetch depends on another, so the package needs no transaction.
type Store interface {
	ListDatasources(ctx context.Context) ([]gen.Datasource, error)
	ListOpenBlocks(ctx context.Context, arg gen.ListOpenBlocksParams) ([]gen.DatasourceBlock, error)
	CreateFetch(ctx context.Context, arg gen.CreateFetchParams) (gen.Fetch, error)
	CreateFetchKey(ctx context.Context, arg gen.CreateFetchKeyParams) error
	CreateDatasourceBlock(ctx context.Context, arg gen.CreateDatasourceBlockParams) (int64, error)
}

var _ Store = (*gen.Queries)(nil)

// Runner is the view of the run framework this package depends on.
type Runner interface {
	Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)
