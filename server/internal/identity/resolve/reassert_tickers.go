package resolve

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// heldTicker is one held ticker at its market and what a reassert tickers run
// knows of it. ticker is the first venue ticker held at the market, the
// one an identity request sends.
type heldTicker struct {
	row       gen.ListHeldTickersRow
	key       types.MarketTicker
	ticker    types.Identifier
	tickerRow gen.Ticker
	// instrument is the instrument the ticker's latest interval names, nil
	// where the ticker's latest interval has ended or the ticker has no
	// interval.
	instr *uuid.UUID
	// ids is the instrument's identifiers, in the order ListIdentifiersOf
	// returns them.
	ids []types.Identifier
}

// heldAnswer is the identity answer to one held ticker.
type heldAnswer struct {
	t  *heldTicker
	fk fetchKey
}

// ReassertTickers is the body of a reassert tickers run. It asks each enabled
// datasource once about each held ticker that ListHeldTickers lists, and the
// fetcher it is given skips the cache. It asks a datasource that serves
// identifier facts for the history of the instrument the ticker names, and
// any other datasource for the ticker's identity. It writes the facts the
// answers make and the findings they complete. It returns, for each fetch
// run in precedence order, the identifiers whose facts it wrote. It returns
// an error for a run other than a system reassert tickers run.
func (r *Resolver) ReassertTickers(ctx context.Context, run gen.Run, held []gen.ListHeldTickersRow) ([]WrittenIdentifierFacts, error) {
	if run.Kind != gen.RunKindReassertTickers || run.UserID != nil {
		return nil, fmt.Errorf("run %s is not a system reassert tickers run", run.ID)
	}
	ts, err := r.readHeld(ctx, held)
	if err != nil {
		return nil, err
	}
	answers, factKeys, err := r.fetchHeld(ctx, run, ts)
	if err != nil {
		return nil, err
	}
	recorded, err := r.recordFacts(ctx, factKeys)
	if err != nil {
		return nil, err
	}
	written := newWritten(r.mics)
	var ids []uuid.UUID
	for _, a := range answers {
		if !slices.Contains(ids, a.t.tickerRow.ID) {
			ids = append(ids, a.t.tickerRow.ID)
		}
	}
	err = r.store.Tx(ctx, func(q Queries) error {
		written = newWritten(r.mics)
		if err := tickers.Lock(ctx, q, ids); err != nil {
			return err
		}
		for _, a := range answers {
			if err := r.assertHeld(ctx, q, a.t, a.fk, written); err != nil {
				return err
			}
		}
		return tickervalidity.Rebuild(ctx, q, ids)
	})
	if err != nil {
		return nil, fmt.Errorf("record extended ticker validity: %w", err)
	}
	mine := map[uuid.UUID]bool{}
	for _, a := range answers {
		mine[a.fk.ID] = true
	}
	for _, k := range factKeys {
		mine[k.ID] = true
		if recorded && k.Outcome == gen.FetchOutcomeServed {
			written.addFacts(k.Run, k.Response)
		}
	}
	if len(written.order) == 0 {
		return nil, nil
	}
	if err := r.recordConflicts(ctx, run, written, mine); err != nil {
		return nil, err
	}
	return written.list(), nil
}

// readHeld reads the ticker validity of each held ticker at its market, and the
// identifiers of the instrument its latest interval names. Tickers held at
// venues of one market are read once.
func (r *Resolver) readHeld(ctx context.Context, held []gen.ListHeldTickersRow) ([]*heldTicker, error) {
	var ts []*heldTicker
	seen := map[types.MarketTicker]bool{}
	var keys []types.MarketTicker
	for _, row := range held {
		v := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: row.Venue, Value: row.Value}
		key := types.MarketTicker{Market: row.Market, Value: row.Value}
		if seen[key] {
			continue
		}
		seen[key] = true
		keys = append(keys, key)
		ts = append(ts, &heldTicker{row: row, key: key, ticker: v, tickerRow: gen.Ticker{ID: row.TickerID, Market: row.Market, Value: row.Value}})
	}
	read, err := tickervalidity.Load(ctx, r.store, keys)
	if err != nil {
		return nil, fmt.Errorf("read the held tickers' ticker validity: %w", err)
	}
	var instrs []uuid.UUID
	for _, t := range ts {
		if ivs := read[t.key]; len(ivs) > 0 && ivs[len(ivs)-1].Before == nil {
			t.instr = &ivs[len(ivs)-1].Instr
			instrs = append(instrs, *t.instr)
		}
	}
	if len(instrs) == 0 {
		return ts, nil
	}
	rows, err := r.store.ListIdentifiersOf(ctx, instrs)
	if err != nil {
		return nil, fmt.Errorf("list identifiers: %w", err)
	}
	ids := map[uuid.UUID][]types.Identifier{}
	for _, row := range rows {
		ids[row.InstrID] = append(ids[row.InstrID], to.Identifier(row))
	}
	for _, t := range ts {
		if t.instr != nil {
			t.ids = ids[*t.instr]
		}
	}
	return ts, nil
}

// fetchHeld asks the enabled datasources concurrently. It asks the identifier
// event source of the ticker's market for the instrument's history where
// historyRequest allows it, once per instrument. It asks every other
// datasource for the ticker's identity. It returns the answers and the facts
// keys in datasource order.
func (r *Resolver) fetchHeld(ctx context.Context, run gen.Run, ts []*heldTicker) ([]heldAnswer, []factKey, error) {
	now := r.clock()
	sources, err := r.eventSources(ctx)
	if err != nil {
		return nil, nil, err
	}
	entries := r.sources.Enabled()
	idReqs := make([][]gen.StatedKey, len(entries))
	idOf := make([][]*heldTicker, len(entries))
	factReqs := make([][]market.IdentifierFactRequest, len(entries))
	for i, e := range entries {
		seen := map[market.IdentifierFactRequest]bool{}
		for _, t := range ts {
			if q, ok := historyRequest(e, t, now); ok && sources[t.key.Market] == e.Name {
				if !seen[q] {
					seen[q] = true
					factReqs[i] = append(factReqs[i], q)
				}
				continue
			}
			idReqs[i] = append(idReqs[i], gen.StatedKey{AssetClass: t.row.AssetClass, Currency: t.row.Currency, Identifiers: []types.Identifier{t.ticker}})
			idOf[i] = append(idOf[i], t)
		}
	}
	idKeys := make([][]fetchKey, len(entries))
	factKeys := make([][]factKey, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	for i, e := range entries {
		g.Go(func() error {
			if len(idReqs[i]) > 0 {
				fks, err := r.fetcher.TickerIdentity(gctx, run, e, idReqs[i])
				if err != nil {
					return fmt.Errorf("fetch from %s: %w", e.Name, err)
				}
				idKeys[i] = fks
			}
			if len(factReqs[i]) > 0 {
				fks, err := r.fetcher.IdentifierFacts(gctx, run, e, factReqs[i])
				if err != nil {
					return fmt.Errorf("fetch identifier facts from %s: %w", e.Name, err)
				}
				factKeys[i] = fks
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	var answers []heldAnswer
	var facts []factKey
	for i := range entries {
		for j, fk := range idKeys[i] {
			answers = append(answers, heldAnswer{t: idOf[i][j], fk: fk})
		}
		facts = append(facts, factKeys[i]...)
	}
	return answers, facts, nil
}

// historyRequest returns the request for t's instrument history from e, over
// the history window, under the first stable identifier of the instrument
// that e serves. It returns false where e cannot serve the history.
func historyRequest(e *market.Entry, t *heldTicker, now time.Time) (market.IdentifierFactRequest, bool) {
	ie, ok := e.Integration.(market.IdentifierFacts)
	if !ok || t.instr == nil {
		return market.IdentifierFactRequest{}, false
	}
	w, ok := e.Integration.(market.Windowed)
	if !ok {
		return market.IdentifierFactRequest{}, false
	}
	for _, id := range t.ids {
		if !id.Stable() {
			continue
		}
		q := market.IdentifierFactRequest{Identifier: id, Period: market.History(w, now)}
		if _, err := ie.Facts().Serves(q); err == nil {
			return q, true
		}
	}
	return market.IdentifierFactRequest{}, false
}

// assertHeld writes the assertion of t that the identity answer fk makes,
// dated by the answer. An unknown ticker is asserted to name nothing. A
// listed ticker is asserted to name t's instrument if the listing shares
// another of its identifiers, and otherwise the instrument already stored for
// the listing. An answer that matches no stored instrument writes nothing.
//
// The shared identifier is tried first because a datasource may answer without
// a stable identifier the instrument carries. EODHD does this for some non-US
// rows, which come back without an ISIN. Extending ticker validity never
// creates an instrument. A resolution may have dropped the answer. The held
// instrument then lacks the answer's identifiers. A new instrument made from
// them would contradict every later reassert tickers run.
func (r *Resolver) assertHeld(ctx context.Context, q Queries, t *heldTicker, fk fetchKey, written *writtenSet) error {
	if fk.Outcome != gen.FetchOutcomeServed {
		return nil
	}
	var instr *uuid.UUID
	var s tickervalidity.Stated
	if !fk.Response.Unknown {
		l, ok := r.listingOf(fk.Response, t.key)
		if !ok {
			return nil
		}
		s = r.stated(l, t.key)
		instr = t.instr
		if instr == nil || !shares(l, t) {
			found, err := tickervalidity.StoredInstr(ctx, q, l.Identifiers)
			if err != nil || found == nil {
				return err
			}
			instr = found
		}
	}
	if err := tickervalidity.AssertInstr(ctx, q, fk.ID, t.tickerRow, instr, fk.AnsweredAt, s); err != nil {
		return err
	}
	written.addTicker(fk.Run, t.key)
	return nil
}

// listingOf returns the first listing of res that states key at any venue of
// its market. A listing that moved between venues of the market still
// carries the held ticker.
func (r *Resolver) listingOf(res market.IdentityResult, key types.MarketTicker) (market.Listing, bool) {
	for _, l := range res.Listings {
		for _, id := range l.Identifiers {
			if mt, ok := r.mics.MarketTicker(id); ok && mt == key {
				return l, true
			}
		}
	}
	return market.Listing{}, false
}

// stated returns the venue at which l states key, and l's currency as the
// datasource stated it. The currency is empty where the system does not know
// it.
func (r *Resolver) stated(l market.Listing, key types.MarketTicker) tickervalidity.Stated {
	s := tickervalidity.Stated{Currency: known(r.families, l.Currency)}
	for _, id := range l.Identifiers {
		if mt, ok := r.mics.MarketTicker(id); ok && mt == key {
			s.Venue, _ = r.mics.Operating(id.Domain)
			break
		}
	}
	return s
}

// shares reports whether l carries an identity identifier of t's
// instrument.
func shares(l market.Listing, t *heldTicker) bool {
	return slices.ContainsFunc(l.Identifiers, func(id types.Identifier) bool { return slices.Contains(t.ids, id) })
}
