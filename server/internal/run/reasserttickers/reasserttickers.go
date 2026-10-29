// Package reasserttickers starts runs that ask the datasources again about
// every held ticker.
//
// A reassert tickers run is a system run an administrator starts. Its work
// lives in [reassert_tickers.go](../../identity/resolve/reassert_tickers.go).
package reasserttickers

//go:generate go tool mockgen -source=reasserttickers.go -destination=reasserttickers_mock_test.go -package=reasserttickers -self_package=github.com/leedenison/stonks/server/internal/run/reasserttickers

import (
	"context"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/run/replayrun"
)

// ErrEmpty reports that Start found no held ticker.
var ErrEmpty = fmt.Errorf("no ticker is held: %w", run.ErrEmpty)

// lane serialises reassert tickers runs.
const lane = "reassert_tickers"

// Queries is this package's view of the generated queries.
type Queries interface {
	ListHeldTickers(ctx context.Context) ([]gen.ListHeldTickersRow, error)
}

var _ Queries = (*gen.Queries)(nil)

// Runner is this package's view of the run framework.
type Runner interface {
	Start(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// Reasserter is this package's view of the resolve package.
type Reasserter interface {
	ReassertTickers(ctx context.Context, run gen.Run, held []gen.ListHeldTickersRow) ([]resolve.WrittenIdentifierFacts, error)
}

var _ Reasserter = (*resolve.Resolver)(nil)

// Facts is this package's view of the replay package: what new identifier
// facts start.
type Facts interface {
	AfterIdentifierFacts(ctx context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts)
}

var _ Facts = (*replayrun.Service)(nil)

// Service starts reassert tickers runs.
type Service struct {
	queries    Queries
	runs       Runner
	reasserter Reasserter
	facts      Facts
}

// New returns a Service. reasserter's fetches must skip the cache.
func New(queries Queries, runs Runner, reasserter Reasserter, facts Facts) *Service {
	return &Service{queries: queries, runs: runs, reasserter: reasserter, facts: facts}
}

// Start starts a reassert tickers run over every held ticker, as a system run,
// and returns the pending row. A ticker that becomes held later waits for the
// next run. Its identifier facts replay the unresolved keys that state each
// ticker, and fetch the corporate actions of the held instruments that the
// tickers name; see [replayrun.go](../replayrun/replayrun.go).
func (s *Service) Start(ctx context.Context) (gen.Run, error) {
	held, err := s.queries.ListHeldTickers(ctx)
	if err != nil {
		return gen.Run{}, fmt.Errorf("list held tickers: %w", err)
	}
	if len(held) == 0 {
		return gen.Run{}, ErrEmpty
	}
	spec := run.Spec{Kind: gen.RunKindReassertTickers, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	return s.runs.Start(ctx, spec, func(ctx context.Context, row gen.Run) error {
		written, err := s.reasserter.ReassertTickers(ctx, row, held)
		if err != nil {
			return err
		}
		s.facts.AfterIdentifierFacts(ctx, row, written)
		return nil
	})
}
