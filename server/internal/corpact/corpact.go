// Package corpact fetches, records and reads the corporate actions of
// tickers.
//
// Each market names one corporate actions source. A fetch asks only that
// datasource for the corporate actions of the market's tickers. A read takes
// only that datasource's answers. Each market names one identifier event
// source in the same way. A comparison across datasources would raise
// findings without giving a better figure. A market whose source is unset,
// disabled or failing leaves its holdings provisional, whichever other
// datasource has answers.
//
// A corporate action is stored as a datasource reported it, against the ticker
// and its market. Ticker validity decides the instrument it applies to when it
// is read; see
// [tickervalidity.go](../identity/tickervalidity/tickervalidity.go).
//
// The latest answer of a datasource for a ticker, by the moment the
// datasource gave it, replaces its earlier one. A cached answer keeps the
// moment of the call that filled the cache.
//
// Only the source of an instrument's market restates a holding of the
// instrument; see Markets in calendar.go. A ticker of the instrument in
// another market lends it no corporate actions, so one holding is never
// restated by two sources. An instrument without a market leaves its holdings
// provisional.
//
// Holdings and the stated split check read the corporate actions of an
// instrument through one calendar, from its market; see calendar.go.
//
// A fetch checks the splits that statements state against the calendar; see
// split.go. What starts a fetch, and what it asks for, is in
// [refreshcorpacts.go](../run/refreshcorpacts/refreshcorpacts.go).
package corpact

import (
	"context"
	"fmt"
	"log/slog"
	"slices"

	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// answerKey is a fetch key of a corporate actions request.
type answerKey = market.FetchKey[market.CorpActRequest, market.CorpActResult]

// Recorder fetches the corporate actions of tickers, and records the answers
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

// Fetch asks the corporate actions source of each request's market,
// concurrently, for reqs, as child runs of parent. It then records each
// served answer, and writes the unmatched splits the answers raise.
func (r *Recorder) Fetch(ctx context.Context, parent gen.Run, reqs []market.CorpActRequest) error {
	reqs = r.distinct(reqs)
	if len(reqs) == 0 {
		return nil
	}
	groups, err := r.bySource(ctx, reqs)
	if err != nil {
		return err
	}
	keys := make([][]answerKey, len(groups))
	g, gctx := errgroup.WithContext(ctx)
	for i, grp := range groups {
		g.Go(func() error {
			fks, err := r.fetcher.CorpActs(gctx, parent, grp.entry, grp.reqs)
			if err != nil {
				return fmt.Errorf("fetch corporate actions from %s: %w", grp.entry.Name, err)
			}
			keys[i] = fks
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return err
	}
	var recorded []recordedKey
	for _, k := range slices.Concat(keys...) {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		rec, ok, err := r.record(ctx, k)
		if err != nil {
			return err
		}
		if ok {
			recorded = append(recorded, rec)
		}
	}
	return r.checkSplits(ctx, parent, recorded)
}

// sourceGroup holds the requests that go to one corporate actions source.
type sourceGroup struct {
	entry *market.Entry
	reqs  []market.CorpActRequest
}

// bySource groups reqs by the corporate actions source of their market. It
// warns of, and leaves out, each request without a usable source.
func (r *Recorder) bySource(ctx context.Context, reqs []market.CorpActRequest) ([]sourceGroup, error) {
	rows, err := r.store.ListCorpActSources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list corporate actions sources: %w", err)
	}
	named := make(map[string]string, len(rows))
	for _, row := range rows {
		named[row.Code] = row.Datasource
	}
	var out []sourceGroup
	warned := map[string]bool{}
	for _, q := range reqs {
		t, ok := r.mics.MarketTicker(q.Ticker)
		if !ok {
			r.log.Warn("corporate actions left out", "ticker", q.Ticker.Name(), "reason", "unknown venue")
			continue
		}
		e, why := r.source(named[t.Market])
		if e == nil {
			if !warned[t.Market] {
				warned[t.Market] = true
				r.log.Warn("corporate actions of market left out", "market", t.Market, "reason", why)
			}
			continue
		}
		i := slices.IndexFunc(out, func(g sourceGroup) bool { return g.entry.Name == e.Name })
		if i < 0 {
			i = len(out)
			out = append(out, sourceGroup{entry: e})
		}
		out[i].reqs = append(out[i].reqs, q)
	}
	return out, nil
}

// source returns the enabled datasource of the name given when it serves
// corporate actions. Otherwise it returns nil and the reason.
func (r *Recorder) source(name string) (*market.Entry, string) {
	if name == "" {
		return nil, "the source is unset"
	}
	e := r.sources.Entry(name)
	if e == nil {
		return nil, name + " is not enabled"
	}
	if _, ok := e.Integration.(market.CorpActs); !ok {
		return nil, name + " does not serve corporate actions"
	}
	return e, ""
}

// distinct keeps the first request of each ticker at its market, so a ticker
// is asked about once per market. A request
// for a ticker outside the system's spelling is kept, and left out when its
// answer is recorded.
func (r *Recorder) distinct(reqs []market.CorpActRequest) []market.CorpActRequest {
	seen := map[any]bool{}
	var out []market.CorpActRequest
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
func (r *Recorder) marketTicker(q market.CorpActRequest) (types.MarketTicker, bool) {
	t, ok := r.mics.MarketTicker(q.Ticker)
	if !ok || tickers.Spelled(r.mics, t) != nil {
		return types.MarketTicker{}, false
	}
	return t, true
}
