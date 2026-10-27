package resolve

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/join"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/types"
)

// writer writes the listings and identifiers of the attached claims onto one
// instrument. It skips each the instrument already has and each another
// instrument has, which taken names. instr starts as the instrument's rows and
// takes each row written.
type writer struct {
	q     Queries
	user  uuid.UUID
	instr join.Instr
	taken map[types.Identifier]bool
	// res is the resolution of the key being written. The writer records
	// there each fact it asserts.
	res *resolution
}

// create writes the instrument of the winner, with its class and provenance.
func (w *writer) create(ctx context.Context, winner *identityClaim) (gen.Instr, error) {
	class := winner.Class
	if class == "" {
		class = types.AssetClassUnknown
	}
	inst, err := w.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: class, FetchKeyID: &winner.fetchKey.ID})
	if err != nil {
		return gen.Instr{}, fmt.Errorf("create instrument: %w", err)
	}
	return inst, nil
}

// attach writes the listings and identifiers of c the instrument lacks, each
// with c's fetch key as provenance, records the fetch key's response against
// the instrument, and asserts each of c's identifiers on the UTC day of the
// answer. A listing takes the primary venue c states only while it has none,
// whichever run or claim created it.
func (w *writer) attach(ctx context.Context, c *identityClaim) error {
	fetchKeyID := c.fetchKey.ID
	for _, id := range c.Identifiers {
		if err := w.identify(ctx, id, nil, fetchKeyID); err != nil {
			return err
		}
	}
	for _, fam := range slices.Sorted(maps.Keys(c.Listings)) {
		if fam == noFamily {
			continue
		}
		l := w.instr.Listing(fam)
		var primary *string
		if p := c.Listings[fam].Primary; p != "" {
			primary = &p
		}
		switch {
		case l == nil:
			row, err := w.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: w.instr.Row.ID, Currency: fam, FetchKeyID: &fetchKeyID, PrimaryMic: primary})
			if err != nil {
				return fmt.Errorf("create listing in %s: %w", fam, err)
			}
			w.instr.Listings = append(w.instr.Listings, row)
			l = &row
		case l.PrimaryMic == nil && primary != nil:
			if err := w.q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: l.ID, PrimaryMic: primary}); err != nil {
				return fmt.Errorf("set primary venue of listing in %s: %w", fam, err)
			}
			l.PrimaryMic = primary
		}
		listing := l.ID
		for _, id := range c.Listings[fam].Identifiers {
			if err := w.identify(ctx, id, &listing, fetchKeyID); err != nil {
				return err
			}
		}
	}
	arg := gen.SetFetchKeyInstrParams{ID: fetchKeyID, UserID: &w.user, InstrID: &w.instr.Row.ID}
	if err := w.q.SetFetchKeyInstr(ctx, arg); err != nil {
		return fmt.Errorf("attach fetch key: %w", err)
	}
	// A claim names one instrument, so the tickers it carries at venues of
	// one market make one assertion.
	asserted := map[any]bool{}
	for id := range c.AllIDs() {
		var k any = id
		if t, ok := w.res.mics.MarketTicker(id); ok {
			k = t
		}
		if !asserted[k] {
			asserted[k] = true
			if err := tickervalidity.AssertIdentifier(ctx, w.q, w.res.mics, fetchKeyID, id, &w.instr.Row.ID, c.fetchKey.AnsweredAt); err != nil {
				return err
			}
		}
		w.res.wrote(c.fetchKey, id)
	}
	return nil
}

// identify writes id on the instrument, or on listing where set, unless it
// already identifies the instrument. An identity answer holds on the day it
// was given, so a ticker it names moves to the instrument from whichever
// instrument carried it.
func (w *writer) identify(ctx context.Context, id types.Identifier, listing *uuid.UUID, fetchKeyID uuid.UUID) error {
	if w.instr.Identifier(id) != nil || w.taken[id] {
		return nil
	}
	var row gen.Identifier
	var err error
	if id.Ticker() {
		row, err = w.q.PlaceTicker(ctx, gen.PlaceTickerParams{ID: db.NewID(), InstrID: w.instr.Row.ID, ListingID: listing, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &fetchKeyID})
	} else {
		row, err = w.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: w.instr.Row.ID, ListingID: listing, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &fetchKeyID})
	}
	if err != nil {
		return fmt.Errorf("create %s: %w", id.Name(), err)
	}
	w.instr.Identifiers = append(w.instr.Identifiers, row)
	return nil
}

// associate writes the association of res with the instrument, through the
// identifier via returns and on the listing of the stated family, with
// arbiter. It reports false when no identifier identifies the instrument.
// When the user already arbitrated the key's association, the update matches
// zero rows and that association stands.
//
// An association through a ticker is dated through the MIC_TICKER on the
// ticker's listing, unless the user chose it. Where the listing has none, the
// association is fixed.
func (w *writer) associate(ctx context.Context, res *resolution, arbiter gen.Arbiter) (bool, error) {
	var listingID *uuid.UUID
	if l := w.instr.Listing(res.family); l != nil {
		listingID = &l.ID
	}
	via, ok := w.via(res, listingID)
	if !ok {
		return false, nil
	}
	if v, ok := w.datedThrough(via); ok && arbiter != gen.ArbiterUser {
		return true, writeDated(ctx, w.q, w.user, res, v, arbiter)
	}
	arg := gen.SetStatedKeyAssociationParams{ID: res.row.ID, UserID: w.user, InstrID: &w.instr.Row.ID, ListingID: listingID, ViaID: &via.ID, Arbiter: arbiter}
	if _, err := w.q.SetStatedKeyAssociation(ctx, arg); err != nil {
		return false, fmt.Errorf("associate key: %w", err)
	}
	return true, nil
}

// datedThrough returns the MIC_TICKER a key associated through via is
// dated through: via itself, or the MIC_TICKER on via's listing for another
// ticker. It reports false where via is no ticker or its listing has no
// MIC_TICKER.
func (w *writer) datedThrough(via gen.Identifier) (types.Identifier, bool) {
	id := to.Identifier(via)
	if !id.Ticker() {
		return types.Identifier{}, false
	}
	if id.Type == types.IdentifierTypeMicTicker {
		return id, true
	}
	for _, row := range w.tickers() {
		if row.ListingID != nil && via.ListingID != nil && *row.ListingID == *via.ListingID {
			return to.Identifier(row), true
		}
	}
	return types.Identifier{}, false
}

// tickers returns the instrument's MIC_TICKER rows, in order of venue and
// value.
func (w *writer) tickers() []gen.Identifier {
	var out []gen.Identifier
	for _, row := range w.instr.Identifiers {
		if row.Type == types.IdentifierTypeMicTicker {
			out = append(out, row)
		}
	}
	slices.SortFunc(out, func(a, b gen.Identifier) int {
		return strings.Compare(a.Domain+":"+a.Value, b.Domain+":"+b.Value)
	})
	return out
}

// writeDated gives res a dated association through v, with arbiter. When the
// user already arbitrated the key's association, it stands.
func writeDated(ctx context.Context, q Queries, user uuid.UUID, res *resolution, v types.Identifier, arbiter gen.Arbiter) error {
	arg := gen.SetStatedKeyDatedAssociationParams{ID: res.row.ID, UserID: user, ViaDomain: v.Domain, ViaValue: v.Value, Arbiter: arbiter}
	if _, err := q.SetStatedKeyDatedAssociation(ctx, arg); err != nil {
		return fmt.Errorf("date key: %w", err)
	}
	return nil
}

// via returns the identifier that carries res's association. That is the
// strongest global identifier or broker description of res that also
// identifies the instrument, with a description ranking below every global
// identifier. Failing that, it is a venue ticker of the chosen listing, then a
// venue ticker of the instrument.
func (w *writer) via(res *resolution, listing *uuid.UUID) (gen.Identifier, bool) {
	for _, id := range res.globalAndDescriptionIDs {
		if row := w.instr.Identifier(id); row != nil {
			return *row, true
		}
	}
	tickers := w.tickers()
	for _, row := range tickers {
		if listing != nil && row.ListingID != nil && *row.ListingID == *listing {
			return row, true
		}
	}
	if len(tickers) > 0 {
		return tickers[0], true
	}
	return gen.Identifier{}, false
}
