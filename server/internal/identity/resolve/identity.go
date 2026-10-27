package resolve

import (
	"context"
	"fmt"

	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// fetchResolutionIdentities fetches the identity of every undecided key, with
// one concurrent fetch per datasource. A datasource that already covers the
// instrument the lookup found is not asked. Each key takes its fetch keys in
// precedence order, and whether a datasource serves a key is the
// datasource's to say. It replaces the currency of every listing it returns
// with the currency's family.
func (r *Resolver) fetchResolutionIdentities(ctx context.Context, run gen.Run, resolutions []*resolution) error {
	entries := r.sources.Enabled()
	batches := make([][]*resolution, len(entries))
	for _, res := range resolutions {
		if res.outcome != "" {
			continue
		}
		for i, e := range entries {
			if res.knownInstrument == nil || !res.knownInstrument.Covered(e.Name) {
				batches[i] = append(batches[i], res)
			}
		}
	}
	fetchKeys := make([][]fetchKey, len(entries))
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
			fks, err := r.fetcher.Identity(gctx, run, e, reqs)
			if err != nil {
				return fmt.Errorf("fetch from %s: %w", e.Name, err)
			}
			fetchKeys[i] = fks
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	for _, fks := range fetchKeys {
		for j := range fks {
			for k, c := range fks[j].Response.Listings {
				fks[j].Response.Listings[k].Currency = r.families.Family(c.Currency)
			}
		}
	}
	for i := range entries {
		for j, res := range batches[i] {
			res.fetchKeys = append(res.fetchKeys, &fetchKeys[i][j])
		}
	}
	return nil
}
