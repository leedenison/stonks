package datasource

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=datasource -self_package=github.com/leedenison/stonks/server/internal/datasource

import (
	"context"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListDatasources(ctx context.Context) ([]gen.Datasource, error)
	UpdateDatasource(ctx context.Context, arg gen.UpdateDatasourceParams) (gen.Datasource, error)
	SetDatasourcePrecedence(ctx context.Context, names []string) error
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Sources is this package's view of the datasource registry: the check that
// the registry would accept a row, and the reload that makes a change to the
// table take effect.
type Sources interface {
	Check(cfg market.Config) error
	Reload(ctx context.Context) error
}

var _ Sources = (*market.Registry)(nil)

// Dropper is this package's view of the fetch cache: the drop of a
// datasource's entries once its settings change.
type Dropper interface {
	Drop(ctx context.Context, datasource string) error
}

var _ Dropper = market.Cache(nil)
