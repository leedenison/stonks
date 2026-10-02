// Package resolve resolves stated keys to system owned instruments.
//
// A key is resolved through the identifiers it states that are recognised
// globally: eg. an ISIN, a SEDOL or a ticker with its venue. When a key
// states only a bare ticker, nothing associates through it.
//
// Order. A key is looked up in the database by each such identifier,
// strongest first; where two name different instruments, the key is left
// unrecognised with a contradiction finding. Every undecided key is then
// requested from each enabled datasource that has not covered the
// instrument the lookup found for it, or from every one where the lookup
// found none.
//
// Choice. Datasources return candidates, and the candidates of each
// response are grouped transitively by the instrument grain identifiers
// they share. The best group of the highest precedence datasource is
// chosen.
//
// Write. Each key with a winner is written in a transaction of its own,
// under an advisory lock on the identifiers it states, so two runs stating
// one key produce one instrument. The write re-reads the database under
// the lock, attaches to the instrument found, and creates one where none is
// found. The key is associated through the strongest identifier it stated.
//
// Merge. When a response identifies two instruments, they are one stored
// twice. The resolver folds the later created instrument into the earlier
// and records a merged finding. Where two carry different values of one
// identifier type and domain naming one subject, the instrument or a
// listing of one currency family, or disjoint classes, the resolver leaves
// them apart with a contradiction finding.
//
// Outcomes. A key is matched when associated, rejected when it contradicts
// the seed, unavailable when a datasource failed or was blocked before
// anything served it, and unrecognised otherwise. Only a currency key can
// be rejected, so whether a statement is refused never depends on what
// another user resolved first. A drop is a finding, except for a group not
// naming the identifier sent, which is summarised in the key's reason.
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
