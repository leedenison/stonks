// Package rebuildresolvedtickers starts runs that rebuild the names of every
// ticker.
//
// A write of facts rebuilds the names of its own tickers. A reorder of the
// datasources, or a deletion that cascades to facts, changes names outside
// any write. Those names stay stale until a rebuild resolved tickers run, and
// may lag while it works.
package rebuildresolvedtickers

//go:generate go tool mockgen -source=rebuildresolvedtickers.go -destination=rebuildresolvedtickers_mock_test.go -package=rebuildresolvedtickers -self_package=github.com/leedenison/stonks/server/internal/run/rebuildresolvedtickers

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/run"
)

// lane serialises rebuild resolved tickers runs.
const lane = "rebuild_resolved_tickers"

// Queries is this package's view of the generated queries.
type Queries interface {
	tickervalidity.RebuildQueries
	LockTickerBatch(ctx context.Context, arg gen.LockTickerBatchParams) ([]uuid.UUID, error)
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

// Service starts rebuild resolved tickers runs.
type Service struct {
	store Store
	runs  Runner
	batch int32
}

// New returns a Service whose runs rebuild batch tickers per transaction.
func New(store Store, runs Runner, batch int32) *Service {
	return &Service{store: store, runs: runs, batch: batch}
}

// Start starts a rebuild resolved tickers run, as a system run an
// administrator started, and returns the pending row.
func (s *Service) Start(ctx context.Context) (gen.Run, error) {
	spec := run.Spec{Kind: gen.RunKindRebuildResolvedTickers, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	return s.runs.Start(ctx, spec, func(ctx context.Context, _ gen.Run) error { return s.rebuild(ctx) })
}

// rebuild rebuilds the names of every ticker, a batch at a time, in id order.
// Each batch runs in a transaction of its own that locks its tickers' rows.
func (s *Service) rebuild(ctx context.Context) error {
	var after *uuid.UUID
	for {
		var ids []uuid.UUID
		err := s.store.Tx(ctx, func(q Queries) error {
			var err error
			if ids, err = q.LockTickerBatch(ctx, gen.LockTickerBatchParams{After: after, Lim: s.batch}); err != nil {
				return fmt.Errorf("lock tickers: %w", err)
			}
			return tickervalidity.Rebuild(ctx, q, ids)
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
