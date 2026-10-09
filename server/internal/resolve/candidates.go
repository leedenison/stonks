package resolve

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	runs "github.com/leedenison/stonks/server/internal/run"
)

// Candidate is one group a datasource's answer describes, offered for the
// user to confirm: an instrument before it is written. It is a group, not a
// market.Candidate, which is one listing of an answer before grouping.
type Candidate struct {
	Datasource string
	// Strongest is the identifier that names the candidate most firmly, as
	// group.strongest picks it; the client names the candidate by it.
	Strongest  types.Identifier
	AssetClass gen.AssetClass
	// Identifiers name the instrument itself, not one of its listings.
	Identifiers []types.Identifier
	// Listings is one per currency family, sorted by family.
	Listings []CandidateListing
}

// CandidateListing is one listing of a candidate.
type CandidateListing struct {
	// Currency is the family code, nil where the datasource stated none; a
	// confirmation does not write that listing's identifiers.
	Currency    *string
	Identifiers []types.Identifier
}

// Offer is what a synchronous resolution offers for a key.
type Offer struct {
	Candidates []Candidate
	// Reasons has one entry per datasource that served nothing, saying why,
	// or one entry for a key that reaches no datasource.
	Reasons []string
}

// Pick names the candidate the user confirms, by any identifier of it.
type Pick struct {
	Datasource string
	Identifier types.Identifier
}

// ErrNoCandidate is returned by Confirm when the datasource's answer lacks
// the candidate, or the candidate fails a check against the stated key or
// the instrument the database names.
var ErrNoCandidate = errors.New("the answer lacks the candidate")

// Candidates is the body of a synchronous resolution that lists the groups
// the datasources offer for key, in datasource precedence order and then
// the datasource's own order. A group must pass the stated checks, and name
// the identifier sent where that is a GUID. It writes the findings against
// run, and stops before choosing, so it writes no resolution key.
func (r *Resolver) Candidates(ctx context.Context, run gen.Run, key gen.StatedKey) (Offer, error) {
	rs, err := r.prepare(ctx, run, []gen.StatedKey{key})
	if err != nil {
		return Offer{}, err
	}
	res := rs[0]
	var o Offer
	findings := res.findings
	switch {
	case res.outcome != "":
		o.Reasons = reasons(res.reason)
	case res.inherit != nil:
		o.Reasons = []string{"the key takes the association of a key the user confirmed"}
	}
	for _, rr := range res.results {
		if rr.Outcome != gen.FetchOutcomeServed {
			s := rr.Source + ": " + unserved[rr.Outcome]
			if rr.Reason != "" {
				s += ": " + rr.Reason
			}
			o.Reasons = append(o.Reasons, s)
			continue
		}
		guid := market.IsGUID(*rr.Sent)
		for _, g := range groups(rr) {
			if guid && !g.named {
				continue
			}
			if detail, ok := g.against(res); ok {
				findings = append(findings, dropped(rr, gen.DropStepStated, g.label(detail)))
				continue
			}
			if c, ok := candidateOf(g); ok {
				o.Candidates = append(o.Candidates, c)
			}
		}
	}
	if len(findings) == 0 {
		return o, nil
	}
	var kinds []gen.FindingKind
	err = r.store.Tx(ctx, func(q Queries) error {
		kinds, err = writeFindings(ctx, q, run, res, findings)
		return err
	})
	if err != nil {
		return Offer{}, fmt.Errorf("record findings for key %s: %w", key.ID, err)
	}
	for _, kind := range kinds {
		runs.Found(ctx, kind)
	}
	return o, nil
}

// Confirm is the body of a synchronous resolution that takes the candidate
// p names as the winner for key, and writes it as a run writes a winner,
// with the user as arbiter. Every key of key's group takes the same
// association.
func (r *Resolver) Confirm(ctx context.Context, run gen.Run, key gen.StatedKey, p Pick) (gen.ResolutionKey, error) {
	rs, err := r.prepare(ctx, run, []gen.StatedKey{key})
	if err != nil {
		return gen.ResolutionKey{}, err
	}
	res := rs[0]
	switch {
	case res.outcome != "":
		return gen.ResolutionKey{}, fmt.Errorf("%w: %s", ErrNoCandidate, res.reason)
	case res.inherit != nil:
		return gen.ResolutionKey{}, fmt.Errorf("%w: the key takes the association of a key the user confirmed", ErrNoCandidate)
	}
	g, err := pickGroup(res, p)
	if err != nil {
		return gen.ResolutionKey{}, err
	}
	return r.write(ctx, run, res, g)
}

// pickGroup returns the group of p's datasource's answer that p names.
func pickGroup(res *resolution, p Pick) (*group, error) {
	for _, rr := range res.results {
		if rr.Source != p.Datasource {
			continue
		}
		if rr.Outcome != gen.FetchOutcomeServed {
			return nil, fmt.Errorf("%w: %s: %s", ErrNoCandidate, rr.Source, unserved[rr.Outcome])
		}
		for _, g := range groups(rr) {
			if g.identifiedBy(p.Identifier) {
				return g, nil
			}
		}
		return nil, fmt.Errorf("%w: %s names no candidate of %s", ErrNoCandidate, name(p.Identifier), rr.Source)
	}
	return nil, fmt.Errorf("%w: %s was not asked", ErrNoCandidate, p.Datasource)
}

// candidateOf returns g as a Candidate, and false where g carries no
// identifier to name it by.
func candidateOf(g *group) (Candidate, bool) {
	strongest, ok := g.strongest()
	if !ok {
		return Candidate{}, false
	}
	c := Candidate{Datasource: g.r.Source, Strongest: strongest, AssetClass: g.class, Identifiers: g.instrument}
	if c.AssetClass == "" {
		c.AssetClass = gen.AssetClassUnknown
	}
	for _, fam := range slices.Sorted(maps.Keys(g.listings)) {
		l := CandidateListing{Identifiers: g.listings[fam]}
		if fam != noFamily {
			f := fam
			l.Currency = &f
		}
		c.Listings = append(c.Listings, l)
	}
	return c, true
}
