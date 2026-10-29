// Package refreshcorpacts decides when the corporate actions of held
// instruments are fetched, and what each fetch asks for. Package corpact
// records the answers; see [corpact.go](../../corpact/corpact.go).
//
// A held instrument is one that a transaction names: through a fixed key, or
// through the ticker of a dated key on the transaction's as-at date. A fetch
// asks about each ticker that ticker validity says named a held instrument,
// when the ticker is in the instrument's market. Its period runs from the
// earliest as-at date of the transactions of every user that name the
// instrument to the day after today. The period is the same whichever
// run asks, because a later answer replaces an earlier one and a shorter period
// would cut the coverage of another user's transactions. A statement and new
// identifier facts each start a fetch, and an administrator can start a run
// over every held instrument.
package refreshcorpacts

//go:generate go tool mockgen -source=refreshcorpacts.go -destination=refreshcorpacts_mock_test.go -package=refreshcorpacts -self_package=github.com/leedenison/stonks/server/internal/run/refreshcorpacts

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/run"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	tickervalidity.ValidityQueries
	corpact.MarketQueries
	ListHeldInstrs(ctx context.Context) ([]gen.ListHeldInstrsRow, error)
	ListTickersNaming(ctx context.Context, instrs []uuid.UUID) ([]gen.ListTickersNamingRow, error)
}

var _ Queries = (*gen.Queries)(nil)

// Runner is this package's view of the run framework.
type Runner interface {
	Start(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error)
	ChildAs(ctx context.Context, parent gen.Run, user *uuid.UUID, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// Recorder is this package's view of package corpact.
type Recorder interface {
	Fetch(ctx context.Context, parent gen.Run, reqs []market.CorpActRequest) error
}

var _ Recorder = (*corpact.Recorder)(nil)

// base holds what Service and Starter share.
type base struct {
	queries  Queries
	runs     Runner
	recorder Recorder
	mics     mic.MICMap
	clock    func() time.Time
	log      *slog.Logger
}

// Service fetches the corporate actions that statements and new identifier
// facts call for.
type Service struct{ base }

// New returns a Service. clock supplies today, which ends each period.
func New(queries Queries, runs Runner, recorder Recorder, mics mic.MICMap, clock func() time.Time, log *slog.Logger) *Service {
	return &Service{base{queries: queries, runs: runs, recorder: recorder, mics: mics, clock: clock, log: log}}
}

// Starter starts refresh corporate actions runs.
type Starter struct{ base }

// NewStarter returns a Starter. recorder's fetches must skip the cache.
func NewStarter(queries Queries, runs Runner, recorder Recorder, mics mic.MICMap, clock func() time.Time, log *slog.Logger) *Starter {
	return &Starter{base{queries: queries, runs: runs, recorder: recorder, mics: mics, clock: clock, log: log}}
}
