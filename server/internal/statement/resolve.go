package statement

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/resolve"
)

// statable reports whether k says anything about an instrument at all: an
// identifier or a description. A key stating neither names nothing, now or
// later, and its rows are rejected.
func (k *key) statable() bool {
	return k.description != nil || len(k.identifiers) > 0
}

// resolve resolves every key and records each outcome against res.
func (g *ingestion) resolve(ctx context.Context, res gen.Run) error {
	for _, k := range g.order {
		if err := g.resolveKey(ctx, k); err != nil {
			return err
		}
		arg := gen.CreateResolutionKeyParams{RunID: res.ID, UserID: g.user, StatedKeyID: k.id, Outcome: k.outcome}
		if k.reason != "" {
			arg.Reason = &k.reason
		}
		if err := g.store.CreateResolutionKey(ctx, arg); err != nil {
			return fmt.Errorf("record resolution: %w", err)
		}
	}
	return nil
}

// resolveKey resolves k from the stated identifier that resolution admits. A
// currency identifier is the only one admitted here, since the seed is the
// only thing that has named an instrument; a key stating none is
// unrecognised.
func (g *ingestion) resolveKey(ctx context.Context, k *key) error {
	id, ok := k.identifier(types.IdentifierTypeCurrency)
	if !ok {
		k.outcome = gen.ResolutionOutcomeUnrecognised
		return nil
	}
	found, err := g.store.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: id.Value})
	switch {
	case errors.Is(err, db.ErrNotFound):
		k.reject("no currency %s", id.Value)
		return nil
	case err != nil:
		return fmt.Errorf("look up currency %s: %w", id.Value, err)
	}
	return g.matchInstrument(ctx, k, found, id.Value)
}

// matchInstrument resolves k to the instrument found names and the listing
// of k's currency family, unless k contradicts the instrument or names a
// line it lacks. A currency identifier is seeded beside the instrument it
// names, so the association it makes is confirmed.
func (g *ingestion) matchInstrument(ctx context.Context, k *key, found gen.FindIdentifierRow, code string) error {
	if k.class != nil && resolve.Disjoint(*k.class, found.Instrument.AssetClass) {
		k.reject("asset class %s contradicts the instrument's %s", *k.class, found.Instrument.AssetClass)
		return nil
	}
	if k.currency == nil {
		k.associate(found.Identifier, gen.ValidityConfirmed)
		k.instrument = &found.Instrument.ID
		return nil
	}
	l, err := g.store.GetListing(ctx, gen.GetListingParams{InstrumentID: found.Instrument.ID, Currency: g.families[*k.currency]})
	switch {
	case errors.Is(err, db.ErrNotFound):
		k.reject("no listing of %s in %s", code, *k.currency)
	case err != nil:
		return fmt.Errorf("look up listing of %s in %s: %w", code, *k.currency, err)
	default:
		k.associate(found.Identifier, gen.ValidityConfirmed)
		k.set(l)
	}
	return nil
}
