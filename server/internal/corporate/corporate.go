// Package corporate fetches, records and reads the corporate events of
// tickers.
//
// A corporate event is stored as a datasource reported it, against the
// ticker and its market. Validity decides the instrument it applies to when
// it is read; see [validity.go](../validity/validity.go).
//
// The latest answer of a datasource for a ticker, by the moment the
// datasource gave it, replaces its earlier one. A cached answer keeps the
// moment of the call that filled the cache.
package corporate

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// answerKey is a fetch key of a corporate events request.
type answerKey = market.FetchKey[market.CorporateEventRequest, market.CorporateEventResult]

// Recorder fetches the corporate events of tickers, and records the answers
// and the findings they raise.
type Recorder struct {
	store   Store
	fetcher Fetcher
	sources Sources
	mics    mic.MICMap
	log     *slog.Logger
}

// New returns a Recorder.
func New(store Store, fetcher Fetcher, sources Sources, mics mic.MICMap, log *slog.Logger) *Recorder {
	return &Recorder{store: store, fetcher: fetcher, sources: sources, mics: mics, log: log}
}

// Fetch asks each enabled datasource that serves corporate events,
// concurrently, for reqs, as child runs of parent. It then records each
// served answer.
func (r *Recorder) Fetch(ctx context.Context, parent gen.Run, reqs []market.CorporateEventRequest) error {
	reqs = r.distinct(reqs)
	if len(reqs) == 0 {
		return nil
	}
	entries := r.sources.Enabled()
	keys := make([][]answerKey, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	for i, e := range entries {
		if _, ok := e.Integration.(market.CorporateEvents); !ok {
			continue
		}
		g.Go(func() error {
			fks, err := r.fetcher.CorporateEvents(gctx, parent, e, reqs)
			if err != nil {
				return fmt.Errorf("fetch corporate events from %s: %w", e.Name, err)
			}
			keys[i] = fks
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	for _, k := range slices.Concat(keys...) {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		if err := r.record(ctx, k); err != nil {
			return err
		}
	}
	return nil
}

// distinct keeps the first request of each ticker at its market, so a ticker
// is asked about once per market. A request
// for a ticker outside the system's spelling is kept, and left out when its
// answer is recorded.
func (r *Recorder) distinct(reqs []market.CorporateEventRequest) []market.CorporateEventRequest {
	seen := map[any]bool{}
	var out []market.CorporateEventRequest
	for _, q := range reqs {
		var k any = q.Ticker
		if t, ok := r.mics.MarketTicker(q.Ticker); ok {
			k = t
		}
		if !seen[k] {
			seen[k] = true
			out = append(out, q)
		}
	}
	return out
}

// marketTicker returns the ticker of q at its market. It reports false where
// the system cannot spell the ticker or its venue.
func (r *Recorder) marketTicker(q market.CorporateEventRequest) (types.MarketTicker, bool) {
	t, ok := r.mics.MarketTicker(q.Ticker)
	if !ok || !market.Canonical(t.Value) {
		return types.MarketTicker{}, false
	}
	return t, true
}
