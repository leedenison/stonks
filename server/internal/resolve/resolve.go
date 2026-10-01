// Package resolve resolves stated keys to system owned instruments.
//
// A key is resolved through the identifiers it states that are recognised
// globally: eg. an ISIN, a SEDOL or a ticker with its venue. A key stating
// only a bare ticker never associates through it.
//
// Order. A key is looked up in the database by each such identifier,
// strongest first; two naming different instruments leave the key
// unrecognised with a contradiction finding. Every undecided key is
// then requested from each enabled datasource that has not covered the
// instrument it was found on, or from every one where it was not found.
//
// Choice. Datasources return candidates, and the candidates of each
// response are grouped transitively by the instrument grain identifiers
// they share. The best group of the highest precedence datasource is
// chosen.
//
// Write. Each key with a winner is written in a transaction of its own.
// The winner is either matched to an instrument or one is created. The key
// is associated through the strongest identifier it stated that identifies
// the instrument, whatever the datasource answered.
//
// Outcomes. A key is matched when associated, rejected when it contradicts
// the seed, unavailable when nothing served it and a datasource failed or
// was blocked, and unrecognised otherwise.
package resolve

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Resolver resolves stated keys against the database and the datasources.
type Resolver struct {
	store   Store
	fetcher Fetcher
	sources Sources
	log     *slog.Logger
}

// New returns a Resolver.
func New(store Store, fetcher Fetcher, sources Sources, log *slog.Logger) *Resolver {
	return &Resolver{store: store, fetcher: fetcher, sources: sources, log: log}
}

// Resolve is the body of a resolution run: it resolves every key against the
// database and the enabled datasources, writes what each response names and
// the association on the key, and records a resolution_keys row per key
// against res, returned in the order of keys.
func (r *Resolver) Resolve(ctx context.Context, run gen.Run, rows []gen.StatedKey) ([]gen.ResolutionKey, error) {
	families, err := r.families(ctx)
	if err != nil {
		return nil, err
	}
	resolutions := make([]*resolution, len(rows))
	for i, row := range rows {
		if resolutions[i], err = r.lookup(ctx, row, families); err != nil {
			return nil, err
		}
	}
	if err := r.request(ctx, run, resolutions); err != nil {
		return nil, err
	}
	out := make([]gen.ResolutionKey, 0, len(resolutions))
	for _, res := range resolutions {
		rk, err := r.write(ctx, run, res, families)
		if err != nil {
			return nil, err
		}
		out = append(out, rk)
	}
	return out, nil
}

// families reads the currency table as a map from code to family.
func (r *Resolver) families(ctx context.Context) (func(string) string, error) {
	rows, err := r.store.ListCurrencies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list currencies: %w", err)
	}
	m := make(map[string]string, len(rows))
	for _, c := range rows {
		m[c.Code] = c.Family
	}
	return func(code string) string { return m[code] }, nil
}
