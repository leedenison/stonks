package market

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

// request calls the provider and retries a temporary failure under backoff.
// Where the provider says when to try again, every caller of the datasource
// waits, not only this one. A refused call spends the quota of every fetch,
// so one wait for all costs less than one refused call per request.
func request[Q, P any](ctx context.Context, f *Fetcher, e *Entry, s Server[Q, P], reqs []Request[Q]) ([]Response[P], int, error) {
	wait := f.base
	for attempt := 1; ; attempt++ {
		if d := e.limiter.held(f.now()); d > 0 {
			if err := f.sleep(ctx, d); err != nil {
				return nil, attempt - 1, err
			}
		}
		if err := e.limiter.rate.Wait(ctx); err != nil {
			return nil, attempt - 1, fmt.Errorf("rate limit: %w", err)
		}
		resps, err := s.Fetch(ctx, reqs)
		if err == nil {
			return resps, attempt, nil
		}
		fail := e.Integration.Classify(err)
		// Repeating a call cannot help a temporary failure about the
		// datasource, such as a spent daily quota.
		if !fail.Temporary || fail.Scope == gen.BlockScopeDatasource || attempt >= f.tries {
			return nil, attempt, err
		}
		if fail.RetryAfter > 0 {
			hold := min(fail.RetryAfter, retryMax)
			e.limiter.holdUntil(f.now().Add(hold))
			f.log.Debug("holding a datasource", "datasource", e.Name, "attempt", attempt, "hold", hold)
			continue
		}
		pause := min(wait+rand.N(wait/2+1), retryMax)
		f.log.Debug("retrying a fetch", "datasource", e.Name, "attempt", attempt, "wait", pause)
		if err := f.sleep(ctx, pause); err != nil {
			return nil, attempt, err
		}
		wait = min(wait*2, retryMax)
	}
}

// sleep waits for d or until ctx is done.
func sleep(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
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
