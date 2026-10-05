package resolve

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// resolution is one stated key's progress from lookup to outcome.
type resolution struct {
	row gen.StatedKey
	// ids is every GUID and broker description the key states, strongest
	// first.
	ids      []types.Identifier
	fams     families
	found    *found
	results  []*result
	findings []gen.CreateFindingParams
	outcome  gen.ResolutionOutcome
	reason   string
}

// found is the instrument the database names for a key.
type found struct {
	instrument  gen.Instrument
	identifiers []gen.Identifier
	listings    []gen.Listing
	coverage    map[string]bool
}

// covered reports whether datasource has answered for the instrument.
// Reference data, the instruments with no provenance, is covered by every
// datasource.
func (f *found) covered(datasource string) bool {
	return f.instrument.FetchKeyID == nil || f.coverage[datasource]
}

// group returns f as a group.
func (f *found) group() *group {
	g := &group{class: f.instrument.AssetClass, listings: map[string][]types.Identifier{}}
	famOf := map[uuid.UUID]string{}
	for _, l := range f.listings {
		famOf[l.ID] = l.Currency
		g.listings[l.Currency] = nil
	}
	for _, id := range f.identifiers {
		v := to.Identifier(id)
		if id.ListingID == nil {
			g.instrument = append(g.instrument, v)
			continue
		}
		fam := famOf[*id.ListingID]
		g.listings[fam] = append(g.listings[fam], v)
	}
	return g
}

func (res *resolution) decide(outcome gen.ResolutionOutcome, format string, args ...any) {
	res.outcome, res.reason = outcome, fmt.Sprintf(format, args...)
}

// lookup finds the instrument the database names for each of rs and decides
// the resolutions that never reach a datasource. Every key's identifiers are
// read in one query.
func (r *Resolver) lookup(ctx context.Context, rs []*resolution) error {
	var ids []types.Identifier
	for _, res := range rs {
		if !currency(res.row) {
			ids = append(ids, res.ids...)
		}
	}
	hits, err := reread(ctx, r.store, ids)
	if err != nil {
		return err
	}
	byID := map[types.Identifier]*found{}
	for _, h := range hits {
		for _, id := range h.matched {
			byID[id] = h.found
		}
	}
	for _, res := range rs {
		if currency(res.row) {
			if err := r.lookupCurrency(ctx, res); err != nil {
				return err
			}
			continue
		}
		res.match(byID)
	}
	return nil
}

// currency reports whether k states a currency, which names money in it.
func currency(k gen.StatedKey) bool {
	return slices.ContainsFunc(k.Identifiers, func(id types.Identifier) bool { return id.Type == types.IdentifierTypeCurrency })
}

// match attaches to res the instrument byID names for it. It decides res
// when its identifiers name different instruments, when the class
// conflicts, or when the key names nothing a datasource could be asked.
func (res *resolution) match(byID map[types.Identifier]*found) {
	var first types.Identifier
	var f *found
	for _, id := range res.ids {
		hit, ok := byID[id]
		switch {
		case !ok:
			continue
		case f == nil:
			first, f = id, hit
		case hit.instrument.ID != f.instrument.ID:
			detail := fmt.Sprintf("%s and %s name different instruments", name(first), name(id))
			res.findings = append(res.findings, gen.CreateFindingParams{Kind: gen.FindingKindContradiction, Detail: ptr.To(detail)})
			res.decide(gen.ResolutionOutcomeUnrecognised, "%s", detail)
			return
		}
	}
	if f == nil {
		if !slices.ContainsFunc(res.ids, market.IsGUID) && !bare(res.row) {
			res.decide(gen.ResolutionOutcomeUnrecognised, "%s", unnamed(res.ids))
		}
		return
	}
	res.found = f
	if reason, ok := classConflict(res.row, f.instrument.AssetClass); ok {
		res.decide(gen.ResolutionOutcomeUnrecognised, "%s", reason)
	}
}

// unnamed returns the reason for a key that states no global identifier.
func unnamed(ids []types.Identifier) string {
	if len(ids) == 0 {
		return "no global identifier"
	}
	parts := make([]string, len(ids))
	for i, id := range ids {
		parts[i] = name(id)
	}
	return "no instrument is identified by " + strings.Join(parts, " or ")
}

// lookupCurrency resolves a currency key against the seed; a currency key
// never reaches a datasource.
func (r *Resolver) lookupCurrency(ctx context.Context, res *resolution) error {
	var code string
	for _, id := range res.row.Identifiers {
		if id.Type == types.IdentifierTypeCurrency {
			code = id.Value
			break
		}
	}
	hit, err := r.store.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: code})
	switch {
	case errors.Is(err, db.ErrNotFound):
		res.decide(gen.ResolutionOutcomeRejected, "no currency %s", code)
		return nil
	case err != nil:
		return fmt.Errorf("look up currency %s: %w", code, err)
	}
	if reason, ok := classConflict(res.row, hit.Instrument.AssetClass); ok {
		res.decide(gen.ResolutionOutcomeRejected, "%s", reason)
		return nil
	}
	loaded, err := load(ctx, r.store, []gen.Instrument{hit.Instrument})
	if err != nil {
		return err
	}
	f := loaded[hit.Instrument.ID]
	if fam := res.fams.family(res.row); fam != "" && f.listing(fam) == nil {
		res.decide(gen.ResolutionOutcomeRejected, "no listing of %s in %s", code, *res.row.Currency)
		return nil
	}
	res.found = f
	return nil
}

// load reads the identifiers, listings and coverage of instruments.
func load(ctx context.Context, q Queries, instruments []gen.Instrument) (map[uuid.UUID]*found, error) {
	out := make(map[uuid.UUID]*found, len(instruments))
	ids := make([]uuid.UUID, len(instruments))
	for i, inst := range instruments {
		out[inst.ID] = &found{instrument: inst, coverage: map[string]bool{}}
		ids[i] = inst.ID
	}
	if len(ids) == 0 {
		return out, nil
	}
	identifiers, err := q.ListIdentifiersOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list identifiers: %w", err)
	}
	for _, row := range identifiers {
		f := out[row.InstrumentID]
		f.identifiers = append(f.identifiers, row)
	}
	listings, err := q.ListListingsOf(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list listings: %w", err)
	}
	for _, l := range listings {
		f := out[l.InstrumentID]
		f.listings = append(f.listings, l)
	}
	coverage, err := q.ListIdentityCoverage(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list coverage: %w", err)
	}
	for _, c := range coverage {
		out[c.InstrumentID].coverage[c.Datasource] = true
	}
	return out, nil
}

// listing returns the listing of fam, nil where the instrument has none.
func (f *found) listing(fam string) *gen.Listing {
	for i := range f.listings {
		if f.listings[i].Currency == fam {
			return &f.listings[i]
		}
	}
	return nil
}
