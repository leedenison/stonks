package statement

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// channel names the route a statement arrives through, the second half of a
// broker description's domain.
const channel = "upload"

func domain(b gen.Broker) string { return string(b) + "/" + channel }

// admitted holds the stated identifier types resolution looks up. A type
// joins as resolution learns to look it up, most by type alone; a ticker
// names one line only with its venue, so it joins under a test of the
// domain as well.
var admitted = map[gen.IdentifierType]bool{gen.IdentifierTypeCurrency: true}

// admissible reports whether resolution can look k up: it states an
// admitted identifier, or a description, which stands for the broker
// description identifier resolution mints.
func (k *key) admissible() bool {
	if k.description != nil {
		return true
	}
	for _, id := range k.identifiers {
		if admitted[gen.IdentifierType(id.Type)] {
			return true
		}
	}
	return false
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
	for _, k := range g.orderedKeys() {
		if err := g.resolveKey(ctx, k); err != nil {
			return err
		}
		arg := gen.CreateResolutionKeyParams{RunID: res.ID, UserID: g.user, StatedKeyID: k.id, Outcome: k.outcome, InstrumentID: k.instrument, ListingID: k.listing}
		if k.reason != "" {
			arg.Reason = &k.reason
		}
		if err := g.store.CreateResolutionKey(ctx, arg); err != nil {
			return fmt.Errorf("record resolution: %w", err)
		}
	}
	return nil
}

// resolveKey looks up what the key's admitted identifier names, an
// instrument for a currency identifier and a listing for a broker
// description, and matches it, or creates it for a description nothing
// holds yet.
func (g *ingestion) resolveKey(ctx context.Context, k *key) error {
	if id, ok := k.identifier(gen.IdentifierTypeCurrency); ok {
		inst, err := g.store.GetInstrumentByIdentifier(ctx, gen.GetInstrumentByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: id.Value})
		switch {
		case errors.Is(err, db.ErrNotFound):
			k.reject("no currency %s", id.Value)
			return nil
		case err != nil:
			return fmt.Errorf("look up currency %s: %w", id.Value, err)
		}
		return g.matchInstrument(ctx, k, inst, id.Value)
	}
	dom := domain(g.broker)
	arg := gen.GetListingByIdentifierParams{OwnerID: &g.user, Type: gen.IdentifierTypeBrokerDescription, Domain: &dom, Value: *k.description}
	found, err := g.store.GetListingByIdentifier(ctx, arg)
	switch {
	case err == nil:
		g.match(k, found)
	case !errors.Is(err, db.ErrNotFound):
		return fmt.Errorf("look up description %q: %w", arg.Value, err)
	case k.currency == nil:
		k.reject("no currency")
	default:
		return g.create(ctx, k, arg)
	}
	return nil
}

// create makes a user owned instrument and listing carrying the identifier
// arg names, in one transaction. An insert the unique index refuses means
// another run made it first, and the listing is re-read and matched.
func (g *ingestion) create(ctx context.Context, k *key, arg gen.GetListingByIdentifierParams) error {
	class := gen.AssetClassUnknown
	if k.class != nil {
		class = *k.class
	}
	var listing gen.Listing
	err := g.store.Tx(ctx, func(q Queries) error {
		inst, err := q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: class, OwnerID: &g.user})
		if err != nil {
			return fmt.Errorf("create instrument: %w", err)
		}
		listing, err = q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: inst.ID, Currency: *k.currency, OwnerID: &g.user})
		if err != nil {
			return fmt.Errorf("create listing: %w", err)
		}
		ident := gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: inst.ID, ListingID: &listing.ID, Type: arg.Type, Domain: arg.Domain, Value: arg.Value, OwnerID: &g.user}
		if _, err := q.CreateIdentifier(ctx, ident); err != nil {
			return fmt.Errorf("create identifier: %w", err)
		}
		return nil
	})
	switch {
	case err == nil:
		k.set(gen.ResolutionOutcomeCreated, listing)
	case db.IsConflict(err):
		found, err := g.store.GetListingByIdentifier(ctx, arg)
		if err != nil {
			return fmt.Errorf("re-read description %q: %w", arg.Value, err)
		}
		g.match(k, found)
	default:
		return err
	}
	return nil
}

// match answers k with the listing its identifier names, unless k
// contradicts it.
func (g *ingestion) match(k *key, found gen.GetListingByIdentifierRow) {
	if k.currency != nil && *k.currency != found.Listing.Currency {
		k.reject("currency %s contradicts the listing named, quoted in %s", *k.currency, found.Listing.Currency)
		return
	}
	if k.class != nil && disjoint(*k.class, found.AssetClass) {
		k.reject("asset class %s contradicts the listing's %s", *k.class, found.AssetClass)
		return
	}
	k.set(gen.ResolutionOutcomeMatched, found.Listing)
}

// matchInstrument answers k with inst and the listing k's currency picks,
// unless k contradicts the instrument or names a line it lacks.
func (g *ingestion) matchInstrument(ctx context.Context, k *key, inst gen.Instrument, code string) error {
	if k.class != nil && disjoint(*k.class, inst.AssetClass) {
		k.reject("asset class %s contradicts the instrument's %s", *k.class, inst.AssetClass)
		return nil
	}
	if k.currency == nil {
		k.outcome = gen.ResolutionOutcomeMatched
		k.instrument = &inst.ID
		return nil
	}
	l, err := g.store.GetListing(ctx, gen.GetListingParams{InstrumentID: inst.ID, Currency: *k.currency})
	switch {
	case errors.Is(err, db.ErrNotFound):
		k.reject("no listing of %s in %s", code, *k.currency)
	case err != nil:
		return fmt.Errorf("look up listing of %s in %s: %w", code, *k.currency, err)
	default:
		k.set(gen.ResolutionOutcomeMatched, l)
	}
	return nil
}
