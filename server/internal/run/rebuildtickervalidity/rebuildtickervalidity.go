// Package rebuildtickervalidity starts runs that rebuild the ticker validity
// and names of every ticker.
//
// A write of facts rebuilds the ticker validity and names of its own tickers.
// A reorder of the datasources, or a deletion that cascades to facts, changes
// them outside any write. They stay stale until a rebuild ticker validity run
// finishes, and may lag while the run works.
package rebuildtickervalidity

//go:generate go tool mockgen -source=rebuildtickervalidity.go -destination=rebuildtickervalidity_mock_test.go -package=rebuildtickervalidity -self_package=github.com/leedenison/stonks/server/internal/run/rebuildtickervalidity

import (
	"context"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/run"
)

// lane serialises rebuild ticker validity runs.
const lane = "rebuild_ticker_validity"

// Queries is this package's view of the generated queries.
type Queries interface {
	tickervalidity.RebuildQueries
	tickers.BatchLocker
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

// Service starts rebuild ticker validity runs.
type Service struct {
	store Store
	runs  Runner
	batch int32
}

// New returns a Service whose runs rebuild batch tickers per transaction.
func New(store Store, runs Runner, batch int32) *Service {
	return &Service{store: store, runs: runs, batch: batch}
}

// Start starts a rebuild ticker validity run, as a system run an
// administrator started, and returns the pending row.
func (s *Service) Start(ctx context.Context) (gen.Run, error) {
	spec := run.Spec{Kind: gen.RunKindRebuildTickerValidity, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	return s.runs.Start(ctx, spec, func(ctx context.Context, _ gen.Run) error { return s.rebuild(ctx) })
}

// rebuild rebuilds the ticker validity and names of every ticker, a batch at a
// time, in id order.
// Each batch runs in a transaction of its own that locks its tickers' rows.
func (s *Service) rebuild(ctx context.Context) error {
	var after *uuid.UUID
	for {
		var ids []uuid.UUID
		err := s.store.Tx(ctx, func(q Queries) error {
			locked, err := tickers.LockBatch(ctx, q, after, s.batch)
			if err != nil {
				return err
			}
			ids = locked.IDs()
			return tickervalidity.Rebuild(ctx, q, locked)
		})
		if err != nil {
			return err
		}
		if len(ids) < int(s.batch) {
			return nil
		}
		after = &ids[len(ids)-1]
	}
}
