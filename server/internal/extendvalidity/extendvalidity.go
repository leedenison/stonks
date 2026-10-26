// Package extendvalidity starts runs that extend the validity of held
// tickers.
//
// An extend validity run is a system run an administrator starts. Its work
// lives in [extend_validity.go](../resolve/extend_validity.go).
package extendvalidity

//go:generate go tool mockgen -source=extendvalidity.go -destination=extendvalidity_mock_test.go -package=extendvalidity -self_package=github.com/leedenison/stonks/server/internal/extendvalidity

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/replay"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
)

// ErrEmpty reports that Start found no held ticker.
var ErrEmpty = errors.New("no ticker is held")

// lane serialises extend validity runs.
const lane = "extend_validity"

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

// Extender is this package's view of the resolve package.
type Extender interface {
	ExtendValidity(ctx context.Context, run gen.Run, held []gen.ListHeldTickersRow) ([]resolve.WrittenIdentifierFacts, error)
}

var _ Extender = (*resolve.Resolver)(nil)

// Replays is this package's view of the replay package.
type Replays interface {
	ReplayNewIdentifierFacts(ctx context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts)
}

var _ Replays = (*replay.Service)(nil)

// Service starts extend validity runs.
type Service struct {
	queries  Queries
	runs     Runner
	extender Extender
	replays  Replays
}

// New returns a Service. extender's fetches must skip the cache.
func New(queries Queries, runs Runner, extender Extender, replays Replays) *Service {
	return &Service{queries: queries, runs: runs, extender: extender, replays: replays}
}

// Start starts an extend validity run over every held ticker, as a system run,
// and returns the pending row. A ticker that becomes held later waits for the
// next run. Its identifier facts replay the unresolved keys that state each
// ticker; see [replay.go](../replay/replay.go).
func (s *Service) Start(ctx context.Context) (gen.Run, error) {
	held, err := s.queries.ListHeldTickers(ctx)
	if err != nil {
		return gen.Run{}, fmt.Errorf("list held tickers: %w", err)
	}
	if len(held) == 0 {
		return gen.Run{}, ErrEmpty
	}
	spec := run.Spec{Kind: gen.RunKindExtendValidity, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	return s.runs.Start(ctx, spec, func(ctx context.Context, row gen.Run) error {
		written, err := s.extender.ExtendValidity(ctx, row, held)
		if err != nil {
			return err
		}
		s.replays.ReplayNewIdentifierFacts(ctx, row, written)
		return nil
	})
}
