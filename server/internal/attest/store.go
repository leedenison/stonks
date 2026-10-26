package attest

//go:generate go tool mockgen -source=store.go -destination=store_mock_test.go -package=attest -self_package=github.com/leedenison/stonks/server/internal/attest

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	CreateIdentifierAttestation(ctx context.Context, arg gen.CreateIdentifierAttestationParams) error
	GetIdentifierAssertion(ctx context.Context, id uuid.UUID) (gen.IdentifierAssertion, error)
	DeleteAttestedAssertion(ctx context.Context, id uuid.UUID) (int64, error)
	GetRunByID(ctx context.Context, id uuid.UUID) (gen.Run, error)
	GetInstrument(ctx context.Context, id uuid.UUID) (gen.Instrument, error)
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

// Replays is this package's view of the replay package.
type Replays interface {
	ReplayNewIdentifierFacts(ctx context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts)
}
