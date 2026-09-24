package datasource

import (
	"context"
	"fmt"
	"math/rand/v2"
	"slices"
	"time"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

const (
	retryBase  = 250 * time.Millisecond
	retryMax   = 2 * time.Second
	retryTries = 3
)

// request calls the provider, waiting on the datasource's limiter before each
// attempt. A temporary failure is retried under an exponential backoff unless
// it is about the datasource, which repeating cannot help.
func request[Q, P any](ctx context.Context, f *Fetcher, e *Entry, s Server[Q, P], reqs []FetchRequest[Q]) ([]FetchResponse[P], int, error) {
	wait := f.base
	for attempt := 1; ; attempt++ {
		if err := e.limiter.Wait(ctx); err != nil {
			return nil, attempt - 1, fmt.Errorf("rate limit: %w", err)
		}
		resps, err := s.Fetch(ctx, reqs)
		if err == nil {
			return resps, attempt, nil
		}
		fail := e.Integration.Classify(err)
		if !fail.Temporary || fail.Scope == gen.BlockScopeDatasource || attempt >= f.tries {
			return nil, attempt, err
		}
		pause := wait + rand.N(wait/2+1)
		if fail.RetryAfter > 0 {
			pause = fail.RetryAfter
		}
		f.log.Debug("retrying a fetch", "datasource", e.Name, "attempt", attempt, "wait", pause)
		select {
		case <-time.After(pause):
		case <-ctx.Done():
			return nil, attempt, ctx.Err()
		}
		if wait *= 2; wait > retryMax {
			wait = retryMax
		}
	}
}

// chunks splits batch into runs of at most n, or one run where n is zero.
func chunks(batch []int, n int) [][]int {
	if len(batch) == 0 {
		return nil
	}
	if n <= 0 {
		return [][]int{batch}
	}
	return slices.Collect(slices.Chunk(batch, n))
}
