// Package fetchprices decides when prices and rates are fetched, and the
// order in which the datasources are asked. Package price states what is
// missing and records the answers; see [price.go](../../price/price.go).
//
// A run asks the enabled datasources in precedence order, each for the gaps
// that those above it left. A failed request never fails the run, and its
// days stay gaps for the next datasource and the next run.
//
// Statements and administrators start runs; see AfterStmt and Start.
package fetchprices

//go:generate go tool mockgen -source=fetchprices.go -destination=fetchprices_mock_test.go -package=fetchprices -self_package=github.com/leedenison/stonks/server/internal/run/fetchprices

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/price"
	"github.com/leedenison/stonks/server/internal/run"
)

// Demand is this package's view of the demand for prices.
type Demand interface {
	UseCurrencies(ctx context.Context) error
	Gaps(ctx context.Context, user *uuid.UUID) ([]price.Gap, []uuid.UUID, error)
}

var _ Demand = (*price.Demand)(nil)

// Recorder is this package's view of package price.
type Recorder interface {
	Fetch(ctx context.Context, parent gen.Run, e *market.Entry, gaps []price.Gap) (int, error)
}

var _ Recorder = (*price.Recorder)(nil)

// Sources is this package's view of the datasource registry. Enabled lists
// the datasources in precedence order.
type Sources interface {
	Enabled() []*market.Entry
}

var _ Sources = (*market.Registry)(nil)

// Runner is this package's view of the run framework.
type Runner interface {
	Start(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error)
	ChildAs(ctx context.Context, parent gen.Run, user *uuid.UUID, kind gen.RunKind, work run.Work) (gen.Run, error)
}

var _ Runner = (*run.Runner)(nil)

// base holds what Service and Starter share.
type base struct {
	demand   Demand
	runs     Runner
	recorder Recorder
	sources  Sources
	log      *slog.Logger
}

// Service fetches the prices that a statement calls for; see AfterStmt.
type Service struct{ base }

// New returns a Service.
func New(demand Demand, runs Runner, recorder Recorder, sources Sources, log *slog.Logger) *Service {
	return &Service{base{demand: demand, runs: runs, recorder: recorder, sources: sources, log: log}}
}

// Starter starts fetch prices runs.
type Starter struct{ base }

// NewStarter returns a Starter. recorder's fetches must skip the cache.
func NewStarter(demand Demand, runs Runner, recorder Recorder, sources Sources, log *slog.Logger) *Starter {
	return &Starter{base{demand: demand, runs: runs, recorder: recorder, sources: sources, log: log}}
}

// gaps returns the gaps in the prices of user, or of every user where user is
// nil, and in every rate. It first creates the USD listing of each currency
// in use, through which the rate of the currency is read.
func (b base) gaps(ctx context.Context, user *uuid.UUID) ([]price.Gap, []uuid.UUID, error) {
	if err := b.demand.UseCurrencies(ctx); err != nil {
		return nil, nil, err
	}
	return b.demand.Gaps(ctx, user)
}

// fill asks each enabled datasource in turn for the gaps that remain, beneath
// parent, and stops once none remains. It reads the gaps again before each
// datasource, so each is asked only for what those before it left.
func (b base) fill(ctx context.Context, parent gen.Run, user *uuid.UUID) error {
	for _, e := range b.sources.Enabled() {
		gaps, unasked, err := b.gaps(ctx, user)
		if err != nil {
			return err
		}
		if len(unasked) > 0 {
			b.log.Info("listings with a gap lack a ticker to ask with", "run", parent.ID, "listings", len(unasked))
		}
		if len(gaps) == 0 {
			return nil
		}
		if _, err := b.recorder.Fetch(ctx, parent, e, gaps); err != nil {
			return fmt.Errorf("fetch prices from %s: %w", e.Name, err)
		}
	}
	return nil
}
