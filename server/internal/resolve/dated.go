package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"
	"golang.org/x/sync/errgroup"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// eventKey is a fetch key of an identifier events request.
type eventKey = market.FetchKey[market.IdentifierEventRequest, market.IdentifierEventResult]

// datedValue returns the MIC_TICKER value through which k may be dated: the
// first venue ticker it states, at its operating MIC. A key that states a
// stable global identifier is fixed through that identifier instead, and
// datedValue reports false.
func datedValue(k gen.StatedKey, mics mic.Table) (types.Identifier, bool) {
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

// associatedValue returns the MIC_TICKER value k's dated association names,
// and false where k has none.
func associatedValue(k gen.StatedKey) (types.Identifier, bool) {
	if k.ViaType == nil || k.ViaDomain == nil || k.ViaValue == nil {
		return types.Identifier{}, false
	}
	return types.Identifier{Type: *k.ViaType, Domain: *k.ViaDomain, Value: *k.ViaValue}, true
}

// findResolutionDated reads the validity of each key's dated value and the
// order dates of its transactions. Keys dated through one value share one
// read of its facts. A key that already has a dated association is matched as
// it stands. A key without a known instrument, whose value has validity, is
// dated through the value without an identity fetch.
func (r *Resolver) findResolutionDated(ctx context.Context, user uuid.UUID, rs []*resolution, keys []KeyOrderDates) error {
	var ids []uuid.UUID
	read := map[types.Identifier]*validity.Ticker{}
	for i, res := range rs {
		if res.outcome != "" {
			continue
		}
		v, ok := associatedValue(res.row)
		if !ok {
			if v, ok = datedValue(res.row, r.mics); !ok {
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
// value on the earliest date the facts do not confirm. A lookup of today is
// left to the identity fetch. A day on which the value already has an
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
func (r *Resolver) recordIdentifierFacts(ctx context.Context, b *batch) error {
	if len(b.events) == 0 {
		return nil
	}
	err := r.store.Tx(ctx, func(q Queries) error {
		return validity.Record(ctx, q, r.mics, r.families, b.events)
	})
	switch {
	case errors.Is(err, validity.ErrSpelling), errors.Is(err, validity.ErrNotTicker):
		r.log.Warn("identifier facts left out", "err", err)
		return nil
	case err != nil:
		return fmt.Errorf("record identifier facts: %w", err)
	}
	return nil
}
