package resolve

import (
	"context"
	"errors"
	"fmt"
	"maps"
	"slices"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	runs "github.com/leedenison/stonks/server/internal/run"
)

// Candidate is one instrument a datasource's answer describes, as a set of
// identifiers offered for the user to confirm before the instrument is
// written. It differs from a market.Candidate, which is one listing of an
// answer.
type Candidate struct {
	Datasource string
	// Strongest is the identifier that names the candidate most firmly, as
	// idSet.strongest picks it; the client names the candidate by it.
	Strongest  types.Identifier
	AssetClass gen.AssetClass
	// Identifiers name the instrument itself, not one of its listings.
	Identifiers []types.Identifier
	// Listings is one per currency family, sorted by family.
	Listings []CandidateListing
}

// ToProto writes c as its proto message.
func (c Candidate) ToProto() *statementv1.Candidate {
	out := &statementv1.Candidate{Datasource: c.Datasource, Strongest: c.Strongest.ToProto(), AssetClass: types.ToProto[typev1.AssetClass](c.AssetClass)}
	for _, id := range c.Identifiers {
		out.Identifiers = append(out.Identifiers, id.ToProto())
	}
	for _, l := range c.Listings {
		out.Listings = append(out.Listings, l.ToProto())
	}
	return out
}

// CandidateListing is one listing of a candidate.
type CandidateListing struct {
	// Currency is the family code, nil where the datasource stated none; a
	// confirmation does not write that listing's identifiers.
	Currency    *string
	Identifiers []types.Identifier
}

// ToProto writes l as its proto message.
func (l CandidateListing) ToProto() *statementv1.CandidateListing {
	out := &statementv1.CandidateListing{Currency: l.Currency}
	for _, id := range l.Identifiers {
		out.Identifiers = append(out.Identifiers, id.ToProto())
	}
	return out
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

// Candidates is the body of a synchronous resolution that lists the sets of
// identifiers the datasources offer for key, in datasource precedence order
// and then the datasource's own order. A set is listed if it survives the
// stated checks and, for a bare ticker, if it agrees with the statement. It
// writes the findings against run and stops before choosing, so it writes no
// resolution key.
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
			o.Reasons = append(o.Reasons, unservedReason(rr))
			continue
		}
		alts := filterAlternatives(rr, res, false)
		findings = append(findings, alts.findings...)
		for _, g := range slices.Concat(alts.offered, alts.survivors) {
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

// Confirm is the body of a synchronous resolution that takes the candidate p
// names as the winner for key, and writes it as a run writes a winner, with
// the user as arbiter. Every key of key's group takes the same instrument. It
// also takes the listing when it states the same currency family as key.
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
	g, err := pickSet(res, p)
	if err != nil {
		return gen.ResolutionKey{}, err
	}
	return r.write(ctx, run, res, g)
}

// pickSet returns the set that p names in its datasource's answer.
func pickSet(res *resolution, p Pick) (*idSet, error) {
	for _, rr := range res.results {
		if rr.Source != p.Datasource {
			continue
		}
		if rr.Outcome != gen.FetchOutcomeServed {
			return nil, fmt.Errorf("%w: %s", ErrNoCandidate, unservedReason(rr))
		}
		for _, g := range idSets(rr) {
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
func candidateOf(g *idSet) (Candidate, bool) {
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
