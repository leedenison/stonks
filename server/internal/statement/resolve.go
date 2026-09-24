package statement

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// statable reports whether k says anything about an instrument at all: an
// identifier or a description. A key stating neither names nothing, now or
// later, and its rows are rejected.
func (k *key) statable() bool {
	return k.description != nil || len(k.identifiers) > 0
}

// parents is the asset class tree, the root mapping to "". A test holds it
// equal to the asset_class_tree table.
var parents = map[gen.AssetClass]gen.AssetClass{
	gen.AssetClassUnknown:     "",
	gen.AssetClassCash:        gen.AssetClassUnknown,
	gen.AssetClassSecurity:    gen.AssetClassUnknown,
	gen.AssetClassEquity:      gen.AssetClassSecurity,
	gen.AssetClassStock:       gen.AssetClassEquity,
	gen.AssetClassEtf:         gen.AssetClassEquity,
	gen.AssetClassMutualFund:  gen.AssetClassEquity,
	gen.AssetClassFixedIncome: gen.AssetClassSecurity,
	gen.AssetClassDerivative:  gen.AssetClassSecurity,
	gen.AssetClassOption:      gen.AssetClassDerivative,
	gen.AssetClassFuture:      gen.AssetClassDerivative,
}

// under reports whether a is b or lies below it.
func under(a, b gen.AssetClass) bool {
	for c := a; c != ""; c = parents[c] {
		if c == b {
			return true
		}
	}
	return false
}

// disjoint reports whether no instrument can be of both classes.
func disjoint(a, b gen.AssetClass) bool { return !under(a, b) && !under(b, a) }

// resolve answers every key and records each answer against res.
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

// resolveKey answers k from the identifier it states that resolution
// admits. A currency identifier is the only one admitted here, since the
// seed is the only thing that has named an instrument; a key stating none is
// unresolved.
func (g *ingestion) resolveKey(ctx context.Context, k *key) error {
	id, ok := k.identifier(types.IdentifierTypeCurrency)
	if !ok {
		k.outcome = gen.ResolutionOutcomeUnresolved
		return nil
	}
	found, err := g.store.GetInstrumentByIdentifier(ctx, gen.GetInstrumentByIdentifierParams{Type: types.IdentifierTypeCurrency, Value: id.Value})
	switch {
	case errors.Is(err, db.ErrNotFound):
		k.reject("no currency %s", id.Value)
		return nil
	case err != nil:
		return fmt.Errorf("look up currency %s: %w", id.Value, err)
	}
	return g.matchInstrument(ctx, k, found, id.Value)
}

// matchInstrument answers k with the instrument found names and the listing
// k's currency picks, unless k contradicts the instrument or names a line it
// lacks. A currency identifier is seeded beside the instrument it names, so
// the association it makes is confirmed.
func (g *ingestion) matchInstrument(ctx context.Context, k *key, found gen.GetInstrumentByIdentifierRow, code string) error {
	if k.class != nil && disjoint(*k.class, found.Instrument.AssetClass) {
		k.reject("asset class %s contradicts the instrument's %s", *k.class, found.Instrument.AssetClass)
		return nil
	}
	if k.currency == nil {
		k.associate(found.Identifier, gen.ValidityConfirmed)
		k.instrument = &found.Instrument.ID
		return nil
	}
	l, err := g.store.GetListing(ctx, gen.GetListingParams{InstrumentID: found.Instrument.ID, Currency: *k.currency})
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
