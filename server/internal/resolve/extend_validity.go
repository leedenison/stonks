package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// heldTicker is one held ticker and what an extend validity run knows of it.
type heldTicker struct {
	row    gen.ListHeldTickersRow
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
// fetcher it is given skips the cache. It writes the assertions the answers
// make and the findings they complete. It returns, for each fetch run in
// precedence order, the identifiers it wrote facts for. It returns an error for
// a run other than a system extend validity run.
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
	written := newWritten()
	err = r.store.Tx(ctx, func(q Queries) error {
		written = newWritten()
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
			for _, id := range factIdentifiers(k.Response) {
				written.add(k.Run, id)
			}
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

// readHeld reads the validity of each held ticker, and the identifiers of
// the instrument its latest interval names.
func (r *Resolver) readHeld(ctx context.Context, held []gen.ListHeldTickersRow) ([]*heldTicker, error) {
	ts := make([]*heldTicker, len(held))
	var instruments []uuid.UUID
	for i, row := range held {
		t := &heldTicker{row: row, ticker: types.Identifier{Type: row.Type, Domain: row.Domain, Value: row.Value}}
		v, err := validity.ReadTicker(ctx, r.store, t.ticker)
		if err != nil {
			return nil, fmt.Errorf("read the facts of %s: %w", t.ticker.Name(), err)
		}
		if ivs := v.Derived.Intervals; len(ivs) > 0 && ivs[len(ivs)-1].Before == nil {
			t.instrument = &ivs[len(ivs)-1].Instrument
			instruments = append(instruments, *t.instrument)
		}
		ts[i] = t
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
	if fk.Response.Unknown {
		a := market.Assertion{Value: t.ticker, Negative: true, At: fk.AnsweredAt}
		if _, err := validity.Assert(ctx, q, r.families, fk.ID, a); err != nil {
			return err
		}
		written.add(fk.Run, t.ticker)
		return nil
	}
	l, ok := r.listingOf(fk.Response, t.ticker)
	if !ok {
		return nil
	}
	inst := t.instrument
	if inst == nil || !r.shares(l, t) {
		found, err := storedInstrument(ctx, q, l)
		if err != nil || found == nil {
			return err
		}
		inst = found
	}
	arg := gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &fk.ID, Type: t.ticker.Type, Domain: t.ticker.Domain, Value: t.ticker.Value,
		InstrumentID: inst, AssertedOn: day(fk.AnsweredAt),
	}
	if err := q.CreateIdentifierAssertion(ctx, arg); err != nil {
		return fmt.Errorf("record assertion: %w", err)
	}
	written.add(fk.Run, t.ticker)
	return nil
}

// storedInstrument returns the instrument of the first stable identifier of
// l that is stored, nil where none is.
func storedInstrument(ctx context.Context, q Queries, l market.Listing) (*uuid.UUID, error) {
	for _, id := range l.Identifiers {
		if !id.Stable() {
			continue
		}
		row, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: id.Type, Domain: id.Domain, Value: id.Value})
		if errors.Is(err, db.ErrNotFound) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("find %s: %w", id.Name(), err)
		}
		return &row.Instrument.ID, nil
	}
	return nil, nil
}

// listingOf returns the first listing of res that states v at a venue of
// v's operating MIC.
func (r *Resolver) listingOf(res market.IdentityResult, v types.Identifier) (market.Listing, bool) {
	for _, l := range res.Listings {
		for _, id := range l.Identifiers {
			if r.operating(id) == v {
				return l, true
			}
		}
	}
	return market.Listing{}, false
}

// shares reports whether l carries an identifier of t's instrument other
// than t's ticker. A ticker is compared at its operating MIC.
func (r *Resolver) shares(l market.Listing, t *heldTicker) bool {
	held := make([]types.Identifier, len(t.ids))
	for i, id := range t.ids {
		held[i] = r.operating(id)
	}
	for _, id := range l.Identifiers {
		id = r.operating(id)
		if id != t.ticker && slices.Contains(held, id) {
			return true
		}
	}
	return false
}

// operating returns id with a ticker's venue replaced by its operating MIC.
func (r *Resolver) operating(id types.Identifier) types.Identifier {
	if id.Type == types.IdentifierTypeMicTicker {
		if op, ok := r.mics.Operating(id.Domain); ok {
			id.Domain = op
		}
	}
	return id
}
