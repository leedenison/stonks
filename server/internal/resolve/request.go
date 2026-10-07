package resolve

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// request fetches the identity of every undecided key, one concurrent fetch
// per datasource. A datasource that already covers the instrument the
// lookup found is not asked. Each key takes its results in precedence
// order, and whether a datasource serves a key is the datasource's to say.
// It replaces the currency of every candidate it returns with the
// currency's family.
func (r *Resolver) request(ctx context.Context, run gen.Run, resolutions []*resolution, cur currencies) error {
	entries := r.sources.Enabled()
	batches := make([][]*resolution, len(entries))
	for _, res := range resolutions {
		if res.outcome != "" {
			continue
		}
		for i, e := range entries {
			if res.found == nil || !res.found.covered(e.Name) {
				batches[i] = append(batches[i], res)
			}
		}
	}
	results := make([][]result, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	for i, e := range entries {
		if len(batches[i]) == 0 {
			continue
		}
		reqs := make([]gen.StatedKey, len(batches[i]))
		for j, res := range batches[i] {
			reqs[j] = res.row
		}
		g.Go(func() error {
			rs, err := r.fetcher.Identity(gctx, run, e, reqs)
			if err != nil {
				return fmt.Errorf("fetch from %s: %w", e.Name, err)
			}
			results[i] = rs
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	for _, rs := range results {
		for j := range rs {
			for k, c := range rs[j].Response.Candidates {
				rs[j].Response.Candidates[k].Currency = cur[c.Currency]
			}
		}
	}
	for i := range entries {
		for j, res := range batches[i] {
			res.results = append(res.results, &results[i][j])
		}
	}
	return nil
}
