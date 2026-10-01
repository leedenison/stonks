package resolve

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// request fetches the identity of every undecided key from each enabled
// datasource that has not covered the instrument it was found on, one fetch
// per datasource, concurrently. Each key takes its results in precedence
// order. Whether a datasource serves a key is the datasource's to say.
func (r *Resolver) request(ctx context.Context, run gen.Run, resolutions []*resolution) error {
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
	for i := range entries {
		for j, res := range batches[i] {
			res.results = append(res.results, &results[i][j])
		}
	}
	return nil
}
