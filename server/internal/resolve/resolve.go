// Package resolve resolves stated keys to system owned instruments.
//
// A key is resolved through the identifiers it states that are recognised
// globally, such as an ISIN, a SEDOL or a ticker with its venue. It also
// resolves through the broker's description of the line, when the system
// holds one. When a key states only a bare ticker, nothing associates
// through it. The resolver never writes a stated description as an
// identifier row.
//
// The database is consulted before any datasource, and only the keys it
// leaves undecided are sent out. Two runs stating one key produce one
// instrument, however they interleave; see write.go.
//
// A datasource that lists only some of an instrument's listings never
// contradicts a stated currency.
//
// Only a currency key can be rejected, so whether a statement is refused
// never depends on what another user resolved first.
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

// Resolve is the body of a resolution run. It returns one resolution key per
// row, in the order of rows. A run that fails partway leaves the keys it has
// already written resolved.
func (r *Resolver) Resolve(ctx context.Context, run gen.Run, rows []gen.StatedKey) ([]gen.ResolutionKey, error) {
	cur, err := r.currencies(ctx)
	if err != nil {
		return nil, err
	}
	resolutions := make([]*resolution, len(rows))
	for i, row := range rows {
		res := &resolution{row: row, fam: cur.family(row)}
		res.trusted = res.trustedIDs()
		resolutions[i] = res
	}
	if err := r.lookup(ctx, resolutions); err != nil {
		return nil, err
	}
	if err := r.request(ctx, run, resolutions, cur); err != nil {
		return nil, err
	}
	out := make([]gen.ResolutionKey, 0, len(resolutions))
	for _, res := range resolutions {
		rk, err := r.write(ctx, run, res)
		if err != nil {
			return nil, err
		}
		out = append(out, rk)
	}
	return out, nil
}

// currencies reads the currency table.
func (r *Resolver) currencies(ctx context.Context) (currencies, error) {
	rows, err := r.store.ListCurrencies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list currencies: %w", err)
	}
	cur := make(currencies, len(rows))
	for _, c := range rows {
		cur[c.Code] = c.Family
	}
	return cur, nil
}
