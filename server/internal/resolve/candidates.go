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

// Candidate is one security that one or more datasources describe, as the
// sets of identifiers of one agreement, offered for the user to confirm
// before the instrument is written. It differs from a market.Candidate, which is one listing of an
// answer.
type Candidate struct {
	// Datasource served the head set, which a confirmation writes as the
	// winner.
	Datasource string
	// Attached names the datasources of the members other than the head, in
	// precedence order.
	Attached []string
	// Strongest is the identifier that names the head most firmly, as
	// idSet.strongest picks it. The client names the candidate by it, and a
	// confirmation sends it.
	Strongest types.Identifier
	// AssetClass is the head's class, which the instrument takes.
	AssetClass gen.AssetClass
	// Identifiers name the instrument itself, not one of its listings.
	Identifiers []types.Identifier
	// Listings is one per currency family, sorted by family.
	Listings []CandidateListing
}

// ToProto writes c as its proto message.
func (c Candidate) ToProto() *statementv1.Candidate {
	out := &statementv1.Candidate{Datasource: c.Datasource, Attached: c.Attached, Strongest: c.Strongest.ToProto(), AssetClass: types.ToProto[typev1.AssetClass](c.AssetClass)}
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

// Pick names the candidate the user confirms, by the datasource of its head
// and any identifier of the head.
type Pick struct {
	Datasource string
	Identifier types.Identifier
}

// ErrNoCandidate is returned by Confirm when the answers no longer offer the
// candidate, or the candidate fails a check against the stated key or the
// instrument the database names.
var ErrNoCandidate = errors.New("the answers no longer offer the candidate")

// Candidates is the body of a synchronous resolution that lists the
// candidates the datasources offer for key. Each candidate is one
// agreement, so one security that several datasources describe is offered
// once. The candidates are in the precedence order of their heads, then the
// head datasource's own order. A set is offered if it survives the stated
// checks and, for a bare ticker, if it agrees with the statement. Candidates
// writes the findings against run and stops before choosing. The key's
// resolution is left as it was.
//
// Sets agree only through a stable identifier they share, so one security
// can still be offered twice. EODHD states an ISIN without a FIGI, so for a
// US stock its candidate stands apart from OpenFIGI's. The datasource of the
// candidate the user confirms supplies the instrument.
func (r *Resolver) Candidates(ctx context.Context, run gen.Run, key gen.StatedKey) (Offer, error) {
	rs, err := r.prepare(ctx, run, []gen.StatedKey{key})
	if err != nil {
		return Offer{}, err
	}
	res := rs[0]
	var o Offer
	switch {
	case res.outcome != "":
		o.Reasons = reasons(res.reason)
	case res.inherit != nil:
		o.Reasons = []string{"the key takes the association of a key the user confirmed"}
	}
	sets, dropped, unserved := offers(res)
	o.Reasons = append(o.Reasons, unserved...)
	for _, a := range gather(res, sets) {
		if c, ok := candidateOf(a); ok {
			o.Candidates = append(o.Candidates, c)
		}
	}
	findings := slices.Concat(res.findings, dropped)
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

// offers returns the sets of identifiers the served results offer for res,
// in precedence order and then each datasource's order. It also returns the
// findings of the sets that disagree with the statement. Last, it returns
// the reason for each result that served nothing.
func offers(res *resolution) ([]*idSet, []gen.CreateFindingParams, []string) {
	var sets []*idSet
	var findings []gen.CreateFindingParams
	var unserved []string
	for _, rr := range res.results {
		if rr.Outcome != gen.FetchOutcomeServed {
			unserved = append(unserved, unservedReason(rr))
			continue
		}
		alts := filterAlternatives(rr, res, false)
		findings = append(findings, alts.findings...)
		sets = append(sets, alts.offered...)
		sets = append(sets, alts.survivors...)
	}
	return sets, findings, unserved
}

// Confirm is the body of a synchronous resolution that takes the candidate p
// names for key. It writes the candidate as a run writes a winner and the
// sets attached to it, with the user as arbiter. Confirm gathers the
// candidates again, so answers that changed since Candidates decide what it
// writes. Every key of key's group takes the same instrument. It also takes
// the listing when it states the same currency family as key.
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
	a, err := pickAgreement(res, p)
	if err != nil {
		return gen.ResolutionKey{}, err
	}
	return r.write(ctx, run, res, a)
}

// pickAgreement returns the agreement that p names among those Candidates
// lists for res. It is the first whose head came from p's datasource and
// carries p's identifier.
func pickAgreement(res *resolution, p Pick) (agreement, error) {
	i := slices.IndexFunc(res.results, func(rr *result) bool { return rr.Source == p.Datasource })
	if i < 0 {
		return nil, fmt.Errorf("%w: %s was not asked", ErrNoCandidate, p.Datasource)
	}
	if rr := res.results[i]; rr.Outcome != gen.FetchOutcomeServed {
		return nil, fmt.Errorf("%w: %s", ErrNoCandidate, unservedReason(rr))
	}
	sets, _, _ := offers(res)
	for _, a := range gather(res, sets) {
		if h := a.head(); h.r.Source == p.Datasource && h.identifiedBy(p.Identifier) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("%w: %s names no candidate of %s", ErrNoCandidate, name(p.Identifier), p.Datasource)
}

// candidateOf returns a as a Candidate, and false where its head carries no
// identifier to name it by. The listings of the members combine per
// currency family.
func candidateOf(a agreement) (Candidate, bool) {
	head := a.head()
	strongest, ok := head.strongest()
	if !ok {
		return Candidate{}, false
	}
	c := Candidate{Datasource: head.r.Source, Strongest: strongest, AssetClass: head.class}
	if c.AssetClass == "" {
		c.AssetClass = gen.AssetClassUnknown
	}
	listings := map[string][]types.Identifier{}
	for _, g := range a {
		if g != head {
			c.Attached = append(c.Attached, g.r.Source)
		}
		for _, id := range g.instrument {
			c.Identifiers = appendUnique(c.Identifiers, id)
		}
		for fam, ids := range g.listings {
			for _, id := range ids {
				listings[fam] = appendUnique(listings[fam], id)
			}
		}
	}
	for _, fam := range slices.Sorted(maps.Keys(listings)) {
		l := CandidateListing{Identifiers: listings[fam]}
		if fam != noFamily {
			f := fam
			l.Currency = &f
		}
		c.Listings = append(c.Listings, l)
	}
	return c, true
}
