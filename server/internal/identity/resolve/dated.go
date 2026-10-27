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
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// eventKey is a fetch key of an identifier events request.
type eventKey = market.FetchKey[market.IdentifierEventRequest, market.IdentifierEventResult]

// datedTicker returns the MIC_TICKER through which k may be dated: the first
// venue ticker it states, at its operating MIC. A key that states a stable
// global identifier is fixed through that identifier instead, and datedTicker
// reports false.
func datedTicker(k gen.StatedKey, mics mic.MICMap) (types.Identifier, bool) {
	for _, id := range k.Identifiers {
		if id.Stable() && id.IsGlobal() {
			return types.Identifier{}, false
		}
	}
	for _, id := range k.Identifiers {
		if id.Type != types.IdentifierTypeMicTicker || id.Domain == "" {
			continue
		}
		if op, ok := mics.Operating(id.Domain); ok {
			return types.Identifier{Type: id.Type, Domain: op, Value: id.Value}, true
		}
	}
	return types.Identifier{}, false
}

// findResolutionDated reads the ticker validity of each key's dated ticker and
// the as-at dates of its transactions. Keys dated through the same market
// ticker share one read of its facts. A key that already has a dated
// association is matched as it stands. A key without a known instrument, whose
// ticker has validity, is dated through the ticker without an identity fetch.
func (r *Resolver) findResolutionDated(ctx context.Context, user uuid.UUID, rs []*resolution, keys []KeyDates) error {
	var ids []uuid.UUID
	read := map[types.MarketTicker]*tickervalidity.Ticker{}
	stated := make([]gen.StatedKey, len(rs))
	for i, res := range rs {
		stated[i] = res.row
	}
	dated, err := tickers.OfKeys(ctx, r.store, stated)
	if err != nil {
		return fmt.Errorf("read the dated keys' tickers: %w", err)
	}
	for i, res := range rs {
		if res.outcome != "" {
			continue
		}
		_, v, ok := tickers.Dated(res.row, dated)
		if !ok {
			if v, ok = datedTicker(res.row, r.mics); !ok {
				continue
			}
		}
		mt, ok := r.mics.MarketTicker(v)
		if !ok {
			continue
		}
		if read[mt] == nil {
			t, err := tickervalidity.ReadTicker(ctx, r.store, mt)
			if err != nil {
				return fmt.Errorf("read the facts of %s: %w", mt.Name(), err)
			}
			read[mt] = t
		}
		res.via, res.dating = v, read[mt]
		res.dates = slices.Clone(keys[i].Dates)
		ids = append(ids, res.row.ID)
		switch {
		case res.row.ViaTickerID != nil:
			res.decide(gen.ResolutionOutcomeMatched, "")
		case res.knownInstr == nil && res.dating.HasTickerValidity():
			res.datedFromFacts = true
			res.decide(gen.ResolutionOutcomeMatched, "")
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.store.ListKeyDates(ctx, gen.ListKeyDatesParams{UserID: user, Ids: ids})
	if err != nil {
		return fmt.Errorf("list as-at dates: %w", err)
	}
	byKey := map[uuid.UUID][]time.Time{}
	for _, row := range rows {
		byKey[row.StatedKeyID] = append(byKey[row.StatedKeyID], row.AsAt)
	}
	for _, res := range rs {
		if res.dating != nil {
			res.dates = append(res.dates, byKey[res.row.ID]...)
		}
	}
	return nil
}

// fetchResolutionEvents asks each enabled datasource that serves identifier
// events, concurrently, for what dating the keys needs: the ticker history of
// the instrument the identity fetch named, and a point in time lookup of the
// ticker on the earliest date the facts do not confirm. A lookup of today is
// left to the identity fetch. A day on which the ticker already has an
// assertion is not asked again. It returns the fetch keys in datasource
// order.
func (r *Resolver) fetchResolutionEvents(ctx context.Context, run gen.Run, rs []*resolution) ([]eventKey, error) {
	now := r.clock()
	today := types.Day(now)
	entries := r.sources.Enabled()
	reqs := make([][]market.IdentifierEventRequest, len(entries))
	for i, e := range entries {
		ie, ok := e.Integration.(market.IdentifierEvents)
		if !ok {
			continue
		}
		w, ok := e.Integration.(market.Windowed)
		if !ok {
			continue
		}
		server := ie.Events()
		seen := map[market.IdentifierEventRequest]bool{}
		add := func(q market.IdentifierEventRequest) {
			if !seen[q] {
				seen[q] = true
				reqs[i] = append(reqs[i], q)
			}
		}
		for _, res := range rs {
			if res.dating == nil {
				continue
			}
			if q, ok := chainRequest(res, server, market.History(w, now)); ok {
				add(q)
			}
			d, ok := res.dating.FirstUnconfirmed(res.dates)
			if !ok {
				continue
			}
			day := market.LookupDay(d, w, now)
			if !day.Before(today) || res.dating.Asserted(day) {
				continue
			}
			add(market.IdentifierEventRequest{Identifier: res.via, Period: types.Period{From: day, Before: day.AddDate(0, 0, 1)}})
		}
	}
	keys := make([][]eventKey, len(entries))
	g, gctx := errgroup.WithContext(ctx)
	for i, e := range entries {
		if len(reqs[i]) == 0 {
			continue
		}
		g.Go(func() error {
			fks, err := r.fetcher.IdentifierEvents(gctx, run, e, reqs[i])
			if err != nil {
				return fmt.Errorf("fetch identifier events from %s: %w", e.Name, err)
			}
			keys[i] = fks
			return nil
		})
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	return slices.Concat(keys...), nil
}

// chainRequest returns the request for the ticker history of the instrument
// the identity fetch named for res, under the first of its stable identifiers
// that server serves. It reports false where no claim won or none is served.
func chainRequest(res *resolution, server market.Server[market.IdentifierEventRequest, market.IdentifierEventResult], period types.Period) (market.IdentifierEventRequest, bool) {
	if len(res.fetchKeys) == 0 {
		return market.IdentifierEventRequest{}, false
	}
	winner := res.first(nil).winner
	if winner == nil {
		return market.IdentifierEventRequest{}, false
	}
	for id := range winner.AllIDs() {
		if !id.Stable() {
			continue
		}
		q := market.IdentifierEventRequest{Identifier: id, Period: period}
		if _, err := server.Serves(q); err == nil {
			return q, true
		}
	}
	return market.IdentifierEventRequest{}, false
}

// recordIdentifierFacts writes the identifier facts the batch's events
// fetches served, in one transaction. It runs after the keys are written, so
// a history names the instrument the batch created for its key. A reply that
// the system cannot spell is left out, and the resolution stands.
//
// It then writes the conflicts that one of the batch's facts completes as
// findings of run; see recordConflicts. It returns, for each datasource fetch
// run, the identifiers it wrote facts for: the identity fetches first, in
// precedence order, then the events fetches.
func (r *Resolver) recordIdentifierFacts(ctx context.Context, run gen.Run, b *batch) ([]WrittenIdentifierFacts, error) {
	recorded, err := r.recordEvents(ctx, b.events)
	if err != nil {
		return nil, err
	}
	written := newWritten(r.mics)
	for _, e := range r.sources.Enabled() {
		for _, res := range b.resolutions {
			for _, f := range res.written {
				if f.fk.Source == e.Name {
					written.add(f.fk.Run, f.id)
				}
			}
		}
	}
	if recorded {
		for _, k := range b.events {
			if k.Outcome == gen.FetchOutcomeServed {
				written.addFacts(k.Run, k.Response)
			}
		}
	}
	if len(written.order) == 0 {
		return nil, nil
	}
	mine := map[uuid.UUID]bool{}
	for _, res := range b.resolutions {
		for _, fk := range res.fetchKeys {
			mine[fk.ID] = true
		}
	}
	for _, k := range b.events {
		mine[k.ID] = true
	}
	if err := r.recordConflicts(ctx, run, written, mine); err != nil {
		return nil, err
	}
	return written.list(), nil
}

// recordEvents writes the identifier facts that the served keys hold, in one
// transaction. Where the system cannot spell a reply, it leaves the facts
// out, logs a warning and returns false without an error.
func (r *Resolver) recordEvents(ctx context.Context, keys []eventKey) (bool, error) {
	if len(keys) == 0 {
		return false, nil
	}
	rows, err := tickervalidity.RecordTickers(ctx, r.store, r.mics, keys)
	if err == nil {
		err = r.store.Tx(ctx, func(q Queries) error {
			return tickervalidity.Record(ctx, q, r.families, rows, keys)
		})
	}
	switch {
	case errors.Is(err, tickers.ErrSpelling):
		r.log.Warn("identifier facts left out", "err", err)
		return false, nil
	case err != nil:
		return false, fmt.Errorf("record identifier facts: %w", err)
	}
	return true, nil
}

// recordConflicts reads the facts of every ticker in written. It writes each
// disagreement, broken bracket and missed event that involves a fetch key in
// mine as a finding of run.
func (r *Resolver) recordConflicts(ctx context.Context, run gen.Run, written *writtenSet, mine map[uuid.UUID]bool) error {
	var kinds []gen.FindingKind
	err := r.store.Tx(ctx, func(q Queries) error {
		kinds = nil
		for _, t := range written.tickers() {
			fs, err := conflicts(ctx, q, t, mine)
			if err != nil {
				return err
			}
			for _, f := range fs {
				f.ID, f.RunID = db.NewID(), run.ID
				if err := q.CreateFinding(ctx, f); err != nil {
					return fmt.Errorf("create finding: %w", err)
				}
				kinds = append(kinds, f.Kind)
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record ticker validity findings: %w", err)
	}
	for _, kind := range kinds {
		runs.Found(ctx, kind)
	}
	return nil
}

// writtenSet gathers the facts each run wrote, in the order the runs first
// wrote one. A ticker at a venue is gathered at its market.
type writtenSet struct {
	mics  mic.MICMap
	order []uuid.UUID
	byRun map[uuid.UUID]*WrittenIdentifierFacts
	seen  map[uuid.UUID]map[any]bool
}

func newWritten(mics mic.MICMap) *writtenSet {
	return &writtenSet{mics: mics, byRun: map[uuid.UUID]*WrittenIdentifierFacts{}, seen: map[uuid.UUID]map[any]bool{}}
}

// run returns the facts of run, and whether k is new to them.
func (w *writtenSet) run(run gen.Run, k any) (*WrittenIdentifierFacts, bool) {
	if _, ok := w.byRun[run.ID]; !ok {
		w.order = append(w.order, run.ID)
		w.byRun[run.ID] = &WrittenIdentifierFacts{Run: run}
		w.seen[run.ID] = map[any]bool{}
	}
	if w.seen[run.ID][k] {
		return nil, false
	}
	w.seen[run.ID][k] = true
	return w.byRun[run.ID], true
}

// add gathers the assertion of id, a ticker at a venue, that run wrote.
func (w *writtenSet) add(run gen.Run, id types.Identifier) {
	if t, ok := w.mics.MarketTicker(id); ok {
		w.addTicker(run, t)
	}
}

func (w *writtenSet) addTicker(run gen.Run, t types.MarketTicker) {
	if f, ok := w.run(run, t); ok {
		f.Tickers = append(f.Tickers, t)
	}
}

// addFacts gathers the tickers whose facts r states, and the markets of its
// coverage rows of a whole market.
func (w *writtenSet) addFacts(run gen.Run, r market.IdentifierEventResult) {
	for _, t := range r.Tickers() {
		w.addTicker(run, t)
	}
	for _, c := range r.Coverage {
		if c.Ticker != nil {
			continue
		}
		if f, ok := w.run(run, "market:"+c.Market); ok {
			f.Markets = append(f.Markets, c.Market)
		}
	}
}

func (w *writtenSet) list() []WrittenIdentifierFacts {
	out := make([]WrittenIdentifierFacts, len(w.order))
	for i, id := range w.order {
		out[i] = *w.byRun[id]
	}
	return out
}

// tickers returns each ticker the runs wrote a fact of, once.
func (w *writtenSet) tickers() []types.MarketTicker {
	seen := map[types.MarketTicker]bool{}
	var out []types.MarketTicker
	for _, run := range w.order {
		for _, t := range w.byRun[run].Tickers {
			if !seen[t] {
				seen[t] = true
				out = append(out, t)
			}
		}
	}
	return out
}

// conflicts returns the findings among the facts of v that involve a fetch
// key in mine. A disagreement names the losing assertion's fetch key. A
// broken bracket names the event's. A missed event names the coverage's.
func conflicts(ctx context.Context, q Queries, v types.MarketTicker, mine map[uuid.UUID]bool) ([]gen.CreateFindingParams, error) {
	f, err := tickervalidity.Read(ctx, q, v)
	if err != nil {
		return nil, fmt.Errorf("read the facts of %s: %w", v.Name(), err)
	}
	d := tickervalidity.Derive(f)
	var out []gen.CreateFindingParams
	for _, dis := range d.Disagreements {
		if !mine[dis.Winner.Source.FetchKey] && !mine[dis.Loser.Source.FetchKey] {
			continue
		}
		detail := fmt.Sprintf("%s on %s: %s named %s and %s named %s; %s was kept",
			v.Name(), dis.On.Format(time.DateOnly), dis.Winner.Source.Datasource, named(dis.Winner.Instr),
			dis.Loser.Source.Datasource, named(dis.Loser.Instr), dis.Winner.Source.Datasource)
		out = append(out, gen.CreateFindingParams{Kind: gen.FindingKindIdentifierDisagreement, FetchKeyID: ptr.To(dis.Loser.Source.FetchKey), Detail: &detail})
	}
	for _, br := range d.Broken {
		if !mine[br.Event.FetchKey] {
			continue
		}
		detail := fmt.Sprintf("%s: an event on %s falls between assertions of %s on %s and %s",
			v.Name(), br.Event.On.Format(time.DateOnly), br.Instr, br.First.Format(time.DateOnly), br.Last.Format(time.DateOnly))
		out = append(out, gen.CreateFindingParams{Kind: gen.FindingKindBrokenTickerValidityBracket, FetchKeyID: ptr.To(br.Event.FetchKey), Detail: &detail})
	}
	for _, m := range d.Missed {
		if !mine[m.Coverage.FetchKey] && !mine[m.Inferred.Earlier.Source.FetchKey] && !mine[m.Inferred.Later.Source.FetchKey] {
			continue
		}
		detail := fmt.Sprintf("%s: assertions on %s and %s imply an event between them, and %s reported no event from %s to %s",
			v.Name(), m.Inferred.Earlier.At.Format(time.DateOnly), m.Inferred.Later.At.Format(time.DateOnly),
			m.Coverage.Datasource, m.Coverage.Period.From.Format(time.DateOnly), m.Coverage.Period.Before.Format(time.DateOnly))
		out = append(out, gen.CreateFindingParams{Kind: gen.FindingKindMissedEvent, FetchKeyID: ptr.To(m.Coverage.FetchKey), Detail: &detail})
	}
	return out, nil
}

// named returns the id of the instrument an assertion names, or "nothing"
// where it names none.
func named(id *uuid.UUID) string {
	if id == nil {
		return "nothing"
	}
	return id.String()
}
