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
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/ptr"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
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

// findResolutionDated reads the validity of each key's dated ticker and the
// order dates of its transactions. Keys dated through one ticker share one
// read of its facts. A key that already has a dated association is matched as
// it stands. A key without a known instrument, whose ticker has validity, is
// dated through the ticker without an identity fetch.
func (r *Resolver) findResolutionDated(ctx context.Context, user uuid.UUID, rs []*resolution, keys []KeyOrderDates) error {
	var ids []uuid.UUID
	read := map[types.Identifier]*validity.Ticker{}
	for i, res := range rs {
		if res.outcome != "" {
			continue
		}
		v, ok := to.AssociatedTicker(res.row)
		if !ok {
			if v, ok = datedTicker(res.row, r.mics); !ok {
				continue
			}
		}
		if read[v] == nil {
			t, err := validity.ReadTicker(ctx, r.store, v)
			if err != nil {
				return fmt.Errorf("read the facts of %s: %w", v.Name(), err)
			}
			read[v] = t
		}
		res.dating = read[v]
		res.orderDates = slices.Clone(keys[i].Dates)
		ids = append(ids, res.row.ID)
		switch {
		case res.row.ViaType != nil:
			res.decide(gen.ResolutionOutcomeMatched, "")
		case res.knownInstrument == nil && res.dating.HasValidity():
			res.datedFromFacts = true
			res.decide(gen.ResolutionOutcomeMatched, "")
		}
	}
	if len(ids) == 0 {
		return nil
	}
	rows, err := r.store.ListKeyOrderDates(ctx, gen.ListKeyOrderDatesParams{UserID: user, Ids: ids})
	if err != nil {
		return fmt.Errorf("list order dates: %w", err)
	}
	byKey := map[uuid.UUID][]time.Time{}
	for _, row := range rows {
		byKey[row.StatedKeyID] = append(byKey[row.StatedKeyID], row.OrderDate)
	}
	for _, res := range rs {
		if res.dating != nil {
			res.orderDates = append(res.orderDates, byKey[res.row.ID]...)
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
			d, ok := res.dating.FirstUnconfirmed(res.orderDates)
			if !ok {
				continue
			}
			day := market.LookupDay(d, w, now)
			if !day.Before(today) || res.dating.Asserted(day) {
				continue
			}
			add(market.IdentifierEventRequest{Identifier: res.dating.Identifier, Period: types.Period{From: day, Before: day.AddDate(0, 0, 1)}})
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
	winner := choose(res, nil, nil).winner
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
// findings of run; see recordConflicts. It returns, for each fetch run, the
// identifiers it wrote facts for: the identity fetches first, in precedence
// order, then the events fetches.
func (r *Resolver) recordIdentifierFacts(ctx context.Context, run gen.Run, b *batch) ([]WrittenIdentifierFacts, error) {
	recorded, err := r.recordEvents(ctx, b.events)
	if err != nil {
		return nil, err
	}
	written := newWritten()
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
				for _, id := range factIdentifiers(k.Response) {
					written.add(k.Run, id)
				}
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
	err := r.store.Tx(ctx, func(q Queries) error {
		return validity.Record(ctx, q, r.mics, r.families, keys)
	})
	switch {
	case errors.Is(err, validity.ErrSpelling), errors.Is(err, validity.ErrNotTicker):
		r.log.Warn("identifier facts left out", "err", err)
		return false, nil
	case err != nil:
		return false, fmt.Errorf("record identifier facts: %w", err)
	}
	return true, nil
}

// recordConflicts reads the facts of every ticker in written. It writes each
// disagreement and broken bracket that involves a fetch key in mine as a
// finding of run.
func (r *Resolver) recordConflicts(ctx context.Context, run gen.Run, written *writtenSet, mine map[uuid.UUID]bool) error {
	var kinds []gen.FindingKind
	err := r.store.Tx(ctx, func(q Queries) error {
		kinds = nil
		met := map[conflict]bool{}
		for _, v := range written.tickers() {
			fs, err := conflicts(ctx, q, v, mine, met)
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
		return fmt.Errorf("record validity findings: %w", err)
	}
	for _, kind := range kinds {
		runs.Found(ctx, kind)
	}
	return nil
}

// factIdentifiers returns the identifiers r states facts of: the tickers of
// its events, the ticker a rename moved to, the tickers of its assertions, and
// the ticker or domain of its coverage.
func factIdentifiers(r market.IdentifierEventResult) []types.Identifier {
	var out []types.Identifier
	for _, e := range r.Events {
		out = append(out, e.Value)
		if e.ToValue != "" {
			out = append(out, types.Identifier{Type: e.Value.Type, Domain: e.Value.Domain, Value: e.ToValue})
		}
	}
	for _, a := range r.Assertions {
		out = append(out, a.Value)
	}
	for _, c := range r.Coverage {
		out = append(out, c.Identifier)
	}
	return out
}

// writtenSet gathers the identifiers of the facts each run wrote, in the
// order the runs first wrote one.
type writtenSet struct {
	order []uuid.UUID
	byRun map[uuid.UUID]*WrittenIdentifierFacts
	seen  map[uuid.UUID]map[types.Identifier]bool
}

func newWritten() *writtenSet {
	return &writtenSet{byRun: map[uuid.UUID]*WrittenIdentifierFacts{}, seen: map[uuid.UUID]map[types.Identifier]bool{}}
}

func (w *writtenSet) add(run gen.Run, id types.Identifier) {
	if _, ok := w.byRun[run.ID]; !ok {
		w.order = append(w.order, run.ID)
		w.byRun[run.ID] = &WrittenIdentifierFacts{Run: run}
		w.seen[run.ID] = map[types.Identifier]bool{}
	}
	if !w.seen[run.ID][id] {
		w.seen[run.ID][id] = true
		w.byRun[run.ID].Identifiers = append(w.byRun[run.ID].Identifiers, id)
	}
}

func (w *writtenSet) list() []WrittenIdentifierFacts {
	out := make([]WrittenIdentifierFacts, len(w.order))
	for i, id := range w.order {
		out[i] = *w.byRun[id]
	}
	return out
}

// tickers returns each ticker the runs wrote a fact of, once, skipping domain
// identifiers.
func (w *writtenSet) tickers() []types.Identifier {
	seen := map[types.Identifier]bool{}
	var out []types.Identifier
	for _, run := range w.order {
		for _, id := range w.byRun[run].Identifiers {
			if !id.IsDomain() && !seen[id] {
				seen[id] = true
				out = append(out, id)
			}
		}
	}
	return out
}

// conflict is one finding's kind, fetch key and day. A datasource that
// states its facts at every venue of a market repeats a conflict for each
// venue's ticker, and met keeps the first.
type conflict struct {
	kind gen.FindingKind
	fk   uuid.UUID
	on   time.Time
}

// conflicts returns the findings among the facts of v in which a fetch key
// in mine took part. It skips each conflict already in met and adds the
// others to it. A disagreement names the losing assertion's fetch key. A
// broken bracket names the event's.
func conflicts(ctx context.Context, q Queries, v types.Identifier, mine map[uuid.UUID]bool, met map[conflict]bool) ([]gen.CreateFindingParams, error) {
	f, err := validity.Read(ctx, q, v)
	if err != nil {
		return nil, fmt.Errorf("read the facts of %s: %w", v.Name(), err)
	}
	d := validity.Derive(f)
	var out []gen.CreateFindingParams
	for _, dis := range d.Disagreements {
		c := conflict{gen.FindingKindDisagreement, dis.Loser.Source.FetchKey, dis.On}
		if (!mine[dis.Winner.Source.FetchKey] && !mine[dis.Loser.Source.FetchKey]) || met[c] {
			continue
		}
		met[c] = true
		detail := fmt.Sprintf("%s on %s: %s named %s and %s named %s; %s was kept",
			v.Name(), dis.On.Format(time.DateOnly), dis.Winner.Source.Datasource, named(dis.Winner.Instrument),
			dis.Loser.Source.Datasource, named(dis.Loser.Instrument), dis.Winner.Source.Datasource)
		out = append(out, gen.CreateFindingParams{Kind: gen.FindingKindDisagreement, FetchKeyID: ptr.To(dis.Loser.Source.FetchKey), Detail: &detail})
	}
	for _, br := range d.Broken {
		c := conflict{gen.FindingKindBrokenValidityBracket, br.Event.FetchKey, br.Event.On}
		if !mine[br.Event.FetchKey] || met[c] {
			continue
		}
		met[c] = true
		detail := fmt.Sprintf("%s: an event on %s falls between assertions of %s on %s and %s",
			v.Name(), br.Event.On.Format(time.DateOnly), br.Instrument, br.First.Format(time.DateOnly), br.Last.Format(time.DateOnly))
		out = append(out, gen.CreateFindingParams{Kind: gen.FindingKindBrokenValidityBracket, FetchKeyID: ptr.To(br.Event.FetchKey), Detail: &detail})
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
