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

// pausedError reports that the framework made no call because the datasource
// is paused. Its text is the reason a key records.
type pausedError string

func (e pausedError) Error() string { return string(e) }

// request calls the provider and retries a temporary failure under backoff.
// Where the provider says when to try again, every caller of the datasource
// waits, not only this one. A refused call spends the quota of every fetch,
// so one wait for all costs less than one refused call per request. A paused
// datasource gets no call, and request returns a pausedError.
func request[Q, P any](ctx context.Context, f *Fetcher, e *Entry, s Server[Q, P], reqs []Request[Q]) ([]Response[P], int, error) {
	wait := f.base
	for attempt := 1; ; attempt++ {
		if reason, ok := e.limiter.paused(f.now()); ok {
			return nil, attempt - 1, pausedError(reason)
		}
		if d := e.limiter.waits(f.now()); d > 0 {
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
		// datasource, such as a spent daily quota. Where the provider says
		// when to try again, no call is made until then.
		if fail.Temporary && fail.Scope == gen.BlockScopeDatasource && fail.RetryAfter > 0 {
			e.limiter.pause(f.now().Add(fail.RetryAfter), reasonOf(fail, err))
			f.log.Info("pausing a datasource", "datasource", e.Name, "for", fail.RetryAfter)
		}
		if !fail.Temporary || fail.Scope == gen.BlockScopeDatasource || attempt >= f.tries {
			return nil, attempt, err
		}
		if fail.RetryAfter > 0 {
			d := min(fail.RetryAfter, retryMax)
			e.limiter.waitUntil(f.now().Add(d))
			f.log.Debug("waiting on a datasource", "datasource", e.Name, "attempt", attempt, "wait", d)
			continue
		}
		pause := min(wait+rand.N(wait/2+1), retryMax) //nolint:gosec // jitter needs no secrecy
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
