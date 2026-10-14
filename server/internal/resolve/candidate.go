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
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// Candidate is one security that one or more datasources describe, as the
// claims of one agreement combined, offered for the user to confirm before
// the instrument is written. It differs from a market.Candidate, which is one
// listing of an answer.
type Candidate struct {
	// Datasource made the head claim, which a confirmation writes as the
	// winner.
	Datasource string
	// Attached names the datasources of the members other than the head, in
	// precedence order.
	Attached []string
	// Strongest is the identifier that names the head most firmly, as
	// claim.strongest picks it. The client names the candidate by it, and a
	// confirmation sends it.
	Strongest types.Identifier
	// Instrument takes the head's class and the identifiers of every member.
	// A confirmation does not write the listing identifiers that have no
	// currency family.
	types.Instrument
}

// ToProto writes c as its proto message, the listings sorted by family. A
// listing of no family has no currency.
func (c Candidate) ToProto() *statementv1.Candidate {
	out := &statementv1.Candidate{Datasource: c.Datasource, Attached: c.Attached, Strongest: c.Strongest.ToProto(), AssetClass: types.ToProto[typev1.AssetClass](c.Class)}
	for _, id := range c.Identifiers {
		out.Identifiers = append(out.Identifiers, id.ToProto())
	}
	for _, fam := range slices.Sorted(maps.Keys(c.Listings)) {
		l := &statementv1.CandidateListing{}
		if fam != noFamily {
			l.Currency = &fam
		}
		for _, id := range c.Listings[fam].Identifiers {
			l.Identifiers = append(l.Identifiers, id.ToProto())
		}
		out.Listings = append(out.Listings, l)
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
// head datasource's own order. A claim is offered if it survives the stated
// checks and, for a bare ticker, if it agrees with the statement. Candidates
// writes the findings against run and stops before choosing. The key's
// resolution is left as it was.
//
// Claims agree only through a stable identifier they share, so one security
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
	case res.arbitrated != nil:
		o.Reasons = []string{"the key takes the association of a key the user confirmed"}
	}
	cs, dropped, unserved := offeredClaims(res)
	o.Reasons = append(o.Reasons, unserved...)
	for _, a := range gatherAgreements(res, cs) {
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

// offeredClaims returns the claims the served fetch keys offer for res,
// in precedence order and then each datasource's order. It also returns the
// findings of the claims that disagree with the statement. Last, it returns
// the reason for each fetch key that served nothing.
func offeredClaims(res *resolution) ([]*claim, []gen.CreateFindingParams, []string) {
	var cs []*claim
	var findings []gen.CreateFindingParams
	var unserved []string
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			unserved = append(unserved, unservedReason(fk))
			continue
		}
		alts := filterAlternatives(fk, res, false)
		findings = append(findings, alts.findings...)
		cs = append(cs, alts.offered...)
		cs = append(cs, alts.survivors...)
	}
	return cs, findings, unserved
}

// Confirm is the body of a synchronous resolution that takes the candidate p
// names for key. It writes the candidate as a run writes a winner and the
// claims attached to it, with the user as arbiter. Confirm gathers the
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
	case res.arbitrated != nil:
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
	i := slices.IndexFunc(res.fetchKeys, func(fk *fetchKey) bool { return fk.Source == p.Datasource })
	if i < 0 {
		return nil, fmt.Errorf("%w: %s was not asked", ErrNoCandidate, p.Datasource)
	}
	if fk := res.fetchKeys[i]; fk.Outcome != gen.FetchOutcomeServed {
		return nil, fmt.Errorf("%w: %s", ErrNoCandidate, unservedReason(fk))
	}
	cs, _, _ := offeredClaims(res)
	for _, a := range gatherAgreements(res, cs) {
		if h := a.head(); h.fetchKey.Source == p.Datasource && h.IdentifiedBy(p.Identifier) {
			return a, nil
		}
	}
	return nil, fmt.Errorf("%w: %s names no candidate of %s", ErrNoCandidate, p.Identifier.Name(), p.Datasource)
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
	out := Candidate{Datasource: head.fetchKey.Source, Strongest: strongest, Instrument: types.Instrument{Class: head.Class, Listings: map[string]types.Listing{}}}
	if out.Class == "" {
		out.Class = types.AssetClassUnknown
	}
	for _, c := range a {
		if c != head {
			out.Attached = append(out.Attached, c.fetchKey.Source)
		}
		for _, id := range c.Identifiers {
			out.Identifiers = appendUnique(out.Identifiers, id)
		}
		for fam, gl := range c.Listings {
			l := out.Listings[fam]
			for _, id := range gl.Identifiers {
				l.Identifiers = appendUnique(l.Identifiers, id)
			}
			if l.Primary == "" {
				l.Primary = gl.Primary
			}
			out.Listings[fam] = l
		}
	}
	return out, true
}
