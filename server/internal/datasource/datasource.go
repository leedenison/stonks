// Package datasource changes the datasources table and keeps the registry
// and the fetch cache in step with it. A reorder also starts a rebuild of the
// names of every ticker, since precedence decides which assertion wins a day.
package datasource

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
)

var (
	// ErrRefused is returned by Update when the registry would refuse the
	// row. It wraps the registry's reason.
	ErrRefused = errors.New("the registry refuses the datasource")
	// ErrOrder is returned by Reorder when the names do not name every
	// datasource exactly once.
	ErrOrder = errors.New("the names do not name every datasource once")
)

// Service changes datasources.
type Service struct {
	store    Store
	sources  Sources
	cache    Dropper
	rebuilds Rebuilder
}

// New returns a Service.
func New(store Store, sources Sources, cache Dropper, rebuilds Rebuilder) *Service {
	return &Service{store: store, sources: sources, cache: cache, rebuilds: rebuilds}
}

// Update changes a datasource's state and settings, makes the change take
// effect, and drops the datasource's cached answers. The registry checks an
// enabled row first, and a refusal leaves the row as it was.
func (s *Service) Update(ctx context.Context, arg gen.UpdateDatasourceParams) (gen.Datasource, error) {
	var row gen.Datasource
	err := s.store.Tx(ctx, func(q Queries) error {
		var err error
		if row, err = q.UpdateDatasource(ctx, arg); err != nil {
			return err
		}
		if !row.Enabled {
			return nil
		}
		if err := s.sources.Check(market.ConfigOf(row)); err != nil {
			return fmt.Errorf("%w: %w", ErrRefused, err)
		}
		return nil
	})
	if err != nil {
		return gen.Datasource{}, err
	}
	if err := s.reload(ctx); err != nil {
		return gen.Datasource{}, err
	}
	if err := s.cache.Drop(context.WithoutCancel(ctx), row.Name); err != nil {
		return gen.Datasource{}, fmt.Errorf("drop cached answers: %w", err)
	}
	return row, nil
}

// Reorder sets each datasource's precedence to its position in names, and
// returns the rebuild resolved tickers run it starts. The cache keeps its
// entries, because precedence orders answers after they are served. Where the
// run fails to start, the new precedence stands and the error is returned.
func (s *Service) Reorder(ctx context.Context, names []string) (gen.Run, error) {
	err := s.store.Tx(ctx, func(q Queries) error {
		rows, err := q.ListDatasources(ctx)
		if err != nil {
			return err
		}
		if err := everyOnce(names, rows); err != nil {
			return fmt.Errorf("%w: %w", ErrOrder, err)
		}
		return q.SetDatasourcePrecedence(ctx, names)
	})
	if err != nil {
		return gen.Run{}, err
	}
	if err := s.reload(ctx); err != nil {
		return gen.Run{}, err
	}
	started, err := s.rebuilds.Start(context.WithoutCancel(ctx))
	if err != nil {
		return gen.Run{}, fmt.Errorf("start rebuild resolved tickers: %w", err)
	}
	return started, nil
}

// reload rebuilds the registry once the table has changed. It runs to the
// end whether or not the caller has gone, so the table and the registry stay
// in step.
func (s *Service) reload(ctx context.Context) error {
	if err := s.sources.Reload(context.WithoutCancel(ctx)); err != nil {
		return fmt.Errorf("reload datasources: %w", err)
	}
	return nil
}

// everyOnce reports why names does not name every datasource of rows exactly
// once, if it does not.
func everyOnce(names []string, rows []gen.Datasource) error {
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return fmt.Errorf("%s is named twice", n)
		}
		seen[n] = true
	}
	for _, r := range rows {
		if !seen[r.Name] {
			return fmt.Errorf("%s is not named", r.Name)
		}
	}
	if len(names) != len(rows) {
		return errors.New("a name is no datasource")
	}
	return nil
}
