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
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// heldTicker is one held ticker at its market and what an extend validity
// run knows of it. ticker is the first venue ticker held at the market, the
// one an identity request sends.
type heldTicker struct {
	row    gen.ListHeldTickersRow
	key    types.MarketTicker
	ticker types.Identifier
	// instrument is the instrument the ticker's latest interval names, nil
	// where the ticker's latest interval has ended or the ticker has no
	// interval.
	instrument *uuid.UUID
	// ids is the instrument's identifiers, in the order ListIdentifiersOf
	// returns them.
	ids []types.Identifier
}

// heldAnswer is the identity answer to one held ticker.
type heldAnswer struct {
	t  *heldTicker
	fk fetchKey
}

// ExtendValidity is the body of an extend validity run. It asks each enabled
// datasource once about each held ticker that ListHeldTickers lists, and the
// fetcher it is given skips the cache. It asks a datasource that serves
// identifier events for the history of the instrument the ticker names, and
// any other datasource for the ticker's identity. It writes the facts the
// answers make and the findings they complete. It returns, for each fetch
// run in precedence order, the identifiers whose facts it wrote. It returns
// an error for a run other than a system extend validity run.
func (r *Resolver) ExtendValidity(ctx context.Context, run gen.Run, held []gen.ListHeldTickersRow) ([]WrittenIdentifierFacts, error) {
	if run.Kind != gen.RunKindExtendValidity || run.UserID != nil {
		return nil, fmt.Errorf("run %s is not a system extend validity run", run.ID)
	}
	ts, err := r.readHeld(ctx, held)
	if err != nil {
		return nil, err
	}
	answers, evKeys, err := r.fetchHeld(ctx, run, ts)
	if err != nil {
		return nil, err
	}
	recorded, err := r.recordEvents(ctx, evKeys)
	if err != nil {
		return nil, err
	}
	written := newWritten(r.mics)
	err = r.store.Tx(ctx, func(q Queries) error {
		written = newWritten(r.mics)
		for _, a := range answers {
			if err := r.assertHeld(ctx, q, a.t, a.fk, written); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("record extended validity: %w", err)
	}
	mine := map[uuid.UUID]bool{}
	for _, a := range answers {
		mine[a.fk.ID] = true
	}
	for _, k := range evKeys {
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

// readHeld reads the validity of each held ticker at its market, and the
// identifiers of the instrument its latest interval names. Tickers held at
// venues of one market are read once.
func (r *Resolver) readHeld(ctx context.Context, held []gen.ListHeldTickersRow) ([]*heldTicker, error) {
	var ts []*heldTicker
	seen := map[types.MarketTicker]bool{}
	var instruments []uuid.UUID
	for _, row := range held {
		v := types.Identifier{Type: row.Type, Domain: row.Domain, Value: row.Value}
		key, ok := r.mics.MarketTicker(v)
		if !ok || seen[key] {
			continue
		}
		seen[key] = true
		t := &heldTicker{row: row, key: key, ticker: v}
		read, err := validity.ReadTicker(ctx, r.store, key)
		if err != nil {
			return nil, fmt.Errorf("read the facts of %s: %w", key.Name(), err)
		}
		if ivs := read.Derived.Intervals; len(ivs) > 0 && ivs[len(ivs)-1].Before == nil {
			t.instrument = &ivs[len(ivs)-1].Instrument
			instruments = append(instruments, *t.instrument)
		}
		ts = append(ts, t)
	}
	if len(instruments) == 0 {
		return ts, nil
	}
	rows, err := r.store.ListIdentifiersOf(ctx, instruments)
	if err != nil {
		return nil, fmt.Errorf("list identifiers: %w", err)
	}
	ids := map[uuid.UUID][]types.Identifier{}
	for _, row := range rows {
		ids[row.InstrumentID] = append(ids[row.InstrumentID], to.Identifier(row))
	}
	for _, t := range ts {
		if t.instrument != nil {
			t.ids = ids[*t.instrument]
		}
	}
	return ts, nil
}

// fetchHeld asks the enabled datasources concurrently. It asks for the
// instrument's history where historyRequest allows it, once per instrument,
// and for the ticker's identity otherwise. It returns the answers and the
// events keys in datasource order.
func (r *Resolver) fetchHeld(ctx context.Context, run gen.Run, ts []*heldTicker) ([]heldAnswer, []eventKey, error) {
	now := r.clock()
	entries := r.sources.Enabled()
	idReqs := make([][]gen.StatedKey, len(entries))
	idOf := make([][]*heldTicker, len(entries))
	evReqs := make([][]market.IdentifierEventRequest, len(entries))
	for i, e := range entries {
		seen := map[market.IdentifierEventRequest]bool{}
		for _, t := range ts {
			if q, ok := historyRequest(e, t, now); ok {
				if !seen[q] {
					seen[q] = true
					evReqs[i] = append(evReqs[i], q)
				}
				continue
			}
			idReqs[i] = append(idReqs[i], gen.StatedKey{AssetClass: t.row.AssetClass, Currency: t.row.Currency, Identifiers: []types.Identifier{t.ticker}})
			idOf[i] = append(idOf[i], t)
		}
	}
	idKeys := make([][]fetchKey, len(entries))
	evKeys := make([][]eventKey, len(entries))
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
			if len(evReqs[i]) > 0 {
				fks, err := r.fetcher.IdentifierEvents(gctx, run, e, evReqs[i])
				if err != nil {
					return fmt.Errorf("fetch identifier events from %s: %w", e.Name, err)
				}
				evKeys[i] = fks
			}
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	var answers []heldAnswer
	var events []eventKey
	for i := range entries {
		for j, fk := range idKeys[i] {
			answers = append(answers, heldAnswer{t: idOf[i][j], fk: fk})
		}
		events = append(events, evKeys[i]...)
	}
	return answers, events, nil
}

// historyRequest returns the request for t's instrument history from e, over
// the history window, under the first stable identifier of the instrument
// that e serves. It returns false where e cannot serve the history.
func historyRequest(e *market.Entry, t *heldTicker, now time.Time) (market.IdentifierEventRequest, bool) {
	ie, ok := e.Integration.(market.IdentifierEvents)
	if !ok || t.instrument == nil {
		return market.IdentifierEventRequest{}, false
	}
	w, ok := e.Integration.(market.Windowed)
	if !ok {
		return market.IdentifierEventRequest{}, false
	}
	for _, id := range t.ids {
		if !id.Stable() {
			continue
		}
		q := market.IdentifierEventRequest{Identifier: id, Period: market.History(w, now)}
		if _, err := ie.Events().Serves(q); err == nil {
			return q, true
		}
	}
	return market.IdentifierEventRequest{}, false
}

// assertHeld writes the assertion of t that the identity answer fk makes,
// dated by the answer. An unknown ticker is asserted to name nothing. A
// listed ticker is asserted to name t's instrument if the listing shares
// another of its identifiers, and otherwise the instrument already stored for
// the listing. An answer that matches no stored instrument writes nothing.
//
// The shared identifier is tried first because a datasource may answer
// without a stable identifier the instrument carries. EODHD does this for
// some non-US rows, which come back without an ISIN. Extending validity never
// creates an instrument. A resolution may have dropped the answer. The held
// instrument then lacks the answer's identifiers. A new instrument made from
// them would contradict every later extend validity run.
func (r *Resolver) assertHeld(ctx context.Context, q Queries, t *heldTicker, fk fetchKey, written *writtenSet) error {
	if fk.Outcome != gen.FetchOutcomeServed {
		return nil
	}
	var inst *uuid.UUID
	if !fk.Response.Unknown {
		l, ok := r.listingOf(fk.Response, t.key)
		if !ok {
			return nil
		}
		inst = t.instrument
		if inst == nil || !r.shares(l, t) {
			found, err := validity.StoredInstrument(ctx, q, l.Identifiers)
			if err != nil || found == nil {
				return err
			}
			inst = found
		}
	}
	if err := validity.AssertInstrument(ctx, q, fk.ID, t.key, inst, fk.AnsweredAt); err != nil {
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

// shares reports whether l carries an identifier of t's instrument other
// than a ticker at t's market.
func (r *Resolver) shares(l market.Listing, t *heldTicker) bool {
	for _, id := range l.Identifiers {
		if mt, ok := r.mics.MarketTicker(id); ok && mt == t.key {
			continue
		}
		if slices.Contains(t.ids, id) {
			return true
		}
	}
	return false
}
