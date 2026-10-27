package attestidentifier

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=attestidentifier -self_package=github.com/leedenison/stonks/server/internal/identity/attestidentifier

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	tickers.Queries
	tickers.Locker
	tickervalidity.RebuildQueries
	CreateIdentifierAttestation(ctx context.Context, arg gen.CreateIdentifierAttestationParams) error
	GetIdentifierAssertion(ctx context.Context, id uuid.UUID) (gen.GetIdentifierAssertionRow, error)
	DeleteAttestedAssertion(ctx context.Context, id uuid.UUID) (int64, error)
	GetRunByID(ctx context.Context, id uuid.UUID) (gen.Run, error)
	GetInstr(ctx context.Context, id uuid.UUID) (gen.Instr, error)
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
}

var _ Runner = (*run.Runner)(nil)

// Rebuilder rebuilds the names of the tickers whose rows have the ids.
type Rebuilder interface {
	Rebuild(ctx context.Context, q Queries, ids []uuid.UUID) error
}

// Names is the Rebuilder that rewrites resolved_tickers.
type Names struct{}

var _ Rebuilder = Names{}

// Rebuild rebuilds the names of ids; see tickervalidity.Rebuild.
func (Names) Rebuild(ctx context.Context, q Queries, ids []uuid.UUID) error {
	return tickervalidity.Rebuild(ctx, q, ids)
}

// Facts is this package's view of the replay package: what new identifier
// facts start.
type Facts interface {
	AfterIdentifierFacts(ctx context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts)
}
