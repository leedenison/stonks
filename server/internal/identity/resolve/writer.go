package resolve

import (
	"context"
	"fmt"
	"maps"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/join"
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
	instr, err := w.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: class, FetchKeyID: &winner.fetchKey.ID})
	if err != nil {
		return gen.Instr{}, fmt.Errorf("create instrument: %w", err)
	}
	return instr, nil
}

// attach adds the listings and identity identifiers of c that the instrument
// lacks, with c's fetch key as provenance, and asserts c's tickers on the UTC
// day of the answer. A listing takes the primary venue c states only while it
// has none, whichever run or claim created it.
func (w *writer) attach(ctx context.Context, c *identityClaim) error {
	fetchKeyID := c.fetchKey.ID
	for _, id := range c.Identifiers {
		if id.Ticker() {
			continue
		}
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
			if id.Ticker() {
				continue
			}
			if err := w.identify(ctx, id, &listing, fetchKeyID); err != nil {
				return err
			}
		}
	}
	arg := gen.SetFetchKeyInstrParams{ID: fetchKeyID, UserID: &w.user, InstrID: &w.instr.Row.ID}
	if err := w.q.SetFetchKeyInstr(ctx, arg); err != nil {
		return fmt.Errorf("attach fetch key: %w", err)
	}
	// A claim names one instrument, so a ticker it carries makes one
	// assertion per venue, with the currency of the first listing that
	// states it there. Only a ticker is asserted.
	type at struct {
		ticker uuid.UUID
		venue  string
	}
	asserted := map[at]bool{}
	for id := range c.AllIDs() {
		t, ok, err := w.res.tickerOf(ctx, w.q, id)
		if err != nil {
			return err
		}
		venue, _ := w.res.mics.Operating(id.Domain)
		if ok && !asserted[at{t.ID, venue}] {
			asserted[at{t.ID, venue}] = true
			s := tickervalidity.Stated{Venue: venue, Currency: currencyOf(w.res.families, c, id)}
			if err := tickervalidity.AssertInstr(ctx, w.q, fetchKeyID, t, &w.instr.Row.ID, c.fetchKey.AnsweredAt, s); err != nil {
				return err
			}
		}
		w.res.wrote(c.fetchKey, id)
	}
	return nil
}

// currencyOf returns the currency of the first listing of c's answer that
// states id, as the datasource stated it. It is empty where no listing states
// id or the system lacks the currency.
func currencyOf(f currency.Families, c *identityClaim, id types.Identifier) string {
	for _, l := range c.fetchKey.Response.Listings {
		if slices.Contains(l.Identifiers, id) {
			return known(f, l.Currency)
		}
	}
	return ""
}

// identify writes the identity identifier id on the instrument, or on listing
// where set, unless it already identifies the instrument.
func (w *writer) identify(ctx context.Context, id types.Identifier, listing *uuid.UUID, fetchKeyID uuid.UUID) error {
	if w.instr.Identifier(id) != nil || w.taken[id] {
		return nil
	}
	row, err := w.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: w.instr.Row.ID, ListingID: listing, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &fetchKeyID})
	if err != nil {
		return fmt.Errorf("create %s: %w", id.Name(), err)
	}
	w.instr.Identifiers = append(w.instr.Identifiers, row)
	return nil
}

// associate writes the association of res with the instrument, on the
// listing of the stated family, with arbiter. It reports false when the key
// has nothing to associate through. When the user already arbitrated the
// key's association, the update matches zero rows and that association
// stands.
//
// An identity row that the key states fixes the association first. A user's
// choice is fixed through choice.via, and a key that the resolver dates is
// dated through its venue ticker. The arbiter of a dated key is never the
// user.
func (w *writer) associate(ctx context.Context, res *resolution, arbiter gen.Arbiter, choice *userChoice) (bool, error) {
	var listingID *uuid.UUID
	if l := w.instr.Listing(res.family); l != nil {
		listingID = &l.ID
	}
	via, ok := w.identityVia(res)
	switch {
	case ok:
	case choice != nil:
		row := w.instr.Identifier(choice.via)
		if row == nil {
			return false, fmt.Errorf("%w: another instrument carries %s", ErrNoCandidate, choice.via.Name())
		}
		via = *row
	case res.dating != nil:
		return true, writeDated(ctx, w.q, w.user, res, res.via, arbiter)
	default:
		return false, nil
	}
	arg := gen.SetStatedKeyAssociationParams{ID: res.row.ID, UserID: w.user, InstrID: &w.instr.Row.ID, ListingID: listingID, ViaIdentifierID: &via.ID, Arbiter: arbiter}
	if _, err := w.q.SetStatedKeyAssociation(ctx, arg); err != nil {
		return false, fmt.Errorf("associate key: %w", err)
	}
	return true, nil
}

// writeDated gives res a dated association through the MIC_TICKER v, with
// arbiter. When the user already arbitrated the key's association, it stands.
func writeDated(ctx context.Context, q Queries, user uuid.UUID, res *resolution, v types.Identifier, arbiter gen.Arbiter) error {
	t, ok, err := res.tickerOf(ctx, q, v)
	if err != nil {
		return err
	}
	venue, known := res.mics.Operating(v.Domain)
	if !ok || !known {
		return fmt.Errorf("date key through %s: the system cannot spell the ticker", v.Name())
	}
	arg := gen.SetStatedKeyDatedAssociationParams{
		ID: res.row.ID, UserID: user, ViaTickerID: t.ID, ViaMarket: t.Market, ViaVenue: venue, Arbiter: arbiter,
	}
	if _, err := q.SetStatedKeyDatedAssociation(ctx, arg); err != nil {
		return fmt.Errorf("date key: %w", err)
	}
	return nil
}

// identityVia returns the identity row that carries res's association: the
// strongest global identifier or broker description of res that identifies
// the instrument, with a description ranking below every global identifier.
func (w *writer) identityVia(res *resolution) (gen.Identifier, bool) {
	for _, id := range res.globalAndDescriptionIDs {
		if row := w.instr.Identifier(id); row != nil {
			return *row, true
		}
	}
	return gen.Identifier{}, false
}
