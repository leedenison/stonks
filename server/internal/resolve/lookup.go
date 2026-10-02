package resolve

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// resolution is one stated key's resolution: the identifiers that resolve
// it, what the lookup found, the results the datasources served, and
// the outcome once decided.
type resolution struct {
	row      gen.StatedKey
	guids    []types.Identifier
	found    *found
	results  []*result
	findings []gen.CreateFindingParams
	outcome  gen.ResolutionOutcome
	reason   string
}

// found is the instrument the database names for a key, with its
// identifiers and listings and the datasources with a coverage row for it.
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
	family := map[uuid.UUID]string{}
	for _, l := range f.listings {
		family[l.ID] = l.Currency
		g.listings[l.Currency] = nil
	}
	for _, id := range f.identifiers {
		v := to.Identifier(id)
		if id.ListingID == nil {
			g.instrument = append(g.instrument, v)
			continue
		}
		f := family[*id.ListingID]
		g.listings[f] = append(g.listings[f], v)
	}
	return g
}

func (res *resolution) decide(outcome gen.ResolutionOutcome, format string, args ...any) {
	res.outcome, res.reason = outcome, fmt.Sprintf(format, args...)
}

// lookup finds the instrument the database names for row and decides the
// resolutions that never reach a datasource.
func (r *Resolver) lookup(ctx context.Context, row gen.StatedKey, families func(string) string) (*resolution, error) {
	res := &resolution{row: row, guids: guids(row)}
	for _, id := range row.Identifiers {
		if id.Type == types.IdentifierTypeCurrency {
			return res, r.lookupCurrency(ctx, res, id.Value, families)
		}
	}
	if len(res.guids) == 0 && !bare(row) {
		res.decide(gen.ResolutionOutcomeUnrecognised, "no global identifier")
		return res, nil
	}
	var hits []gen.FindIdentifierRow
	for _, id := range res.guids {
		hit, err := r.store.FindIdentifier(ctx, gen.FindIdentifierParams{Type: id.Type, Domain: id.Domain, Value: id.Value})
		switch {
		case errors.Is(err, db.ErrNotFound):
			continue
		case err != nil:
			return nil, fmt.Errorf("look up %s: %w", name(id), err)
		}
		hits = append(hits, hit)
	}
	if len(hits) == 0 {
		return res, nil
	}
	first := hits[0]
	for _, hit := range hits[1:] {
		if hit.Instrument.ID != first.Instrument.ID {
			detail := fmt.Sprintf("%s and %s name different instruments", name(to.Identifier(first.Identifier)), name(to.Identifier(hit.Identifier)))
			res.findings = append(res.findings, gen.CreateFindingParams{Kind: gen.FindingKindContradiction, Detail: ptr.To(detail)})
			res.decide(gen.ResolutionOutcomeUnrecognised, "%s", detail)
			return res, nil
		}
	}
	f, err := load(ctx, r.store, first.Instrument)
	if err != nil {
		return nil, err
	}
	res.found = f
	if row.AssetClass != nil && Disjoint(*row.AssetClass, f.instrument.AssetClass) {
		res.decide(gen.ResolutionOutcomeUnrecognised, "asset class %s contradicts the instrument's %s", *row.AssetClass, f.instrument.AssetClass)
	}
	return res, nil
}

// lookupCurrency resolves a currency key against the seed, rejecting it
// where it names a code the seed lacks, a class disjoint from the
// instrument's, or a listing the instrument lacks.
func (r *Resolver) lookupCurrency(ctx context.Context, res *resolution, code string, families func(string) string) error {
	hit, err := r.store.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: code})
	switch {
	case errors.Is(err, db.ErrNotFound):
		res.decide(gen.ResolutionOutcomeRejected, "no currency %s", code)
		return nil
	case err != nil:
		return fmt.Errorf("look up currency %s: %w", code, err)
	}
	if res.row.AssetClass != nil && Disjoint(*res.row.AssetClass, hit.Instrument.AssetClass) {
		res.decide(gen.ResolutionOutcomeRejected, "asset class %s contradicts the instrument's %s", *res.row.AssetClass, hit.Instrument.AssetClass)
		return nil
	}
	f, err := load(ctx, r.store, hit.Instrument)
	if err != nil {
		return err
	}
	if fam := family(res.row, families); fam != "" && f.listing(fam) == nil {
		res.decide(gen.ResolutionOutcomeRejected, "no listing of %s in %s", code, *res.row.Currency)
		return nil
	}
	res.found = f
	return nil
}

// load reads instrument's identifiers, listings and coverage.
func load(ctx context.Context, q Queries, instrument gen.Instrument) (*found, error) {
	f := &found{instrument: instrument, coverage: map[string]bool{}}
	var err error
	if f.identifiers, err = q.ListIdentifiers(ctx, instrument.ID); err != nil {
		return nil, fmt.Errorf("list identifiers: %w", err)
	}
	if f.listings, err = q.ListListings(ctx, instrument.ID); err != nil {
		return nil, fmt.Errorf("list listings: %w", err)
	}
	coverage, err := q.ListIdentityCoverage(ctx, []uuid.UUID{instrument.ID})
	if err != nil {
		return nil, fmt.Errorf("list coverage: %w", err)
	}
	for _, c := range coverage {
		f.coverage[c.Datasource] = true
	}
	return f, nil
}

// listing returns the listing of family, nil where the instrument has none.
func (f *found) listing(family string) *gen.Listing {
	for i := range f.listings {
		if f.listings[i].Currency == family {
			return &f.listings[i]
		}
	}
	return nil
}
