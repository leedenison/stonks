package resolve

import (
	"slices"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// claimVerdict is what choose decides about the claims of every response.
type claimVerdict struct {
	// winner is nil where no claim survived.
	winner *identityClaim
	// confirmed reports a confirmation, whose user choice replaces the naming
	// check for a key sent under a bare ticker or an issuer's identifier.
	confirmed bool
	// refused says why the user choice of a confirmation fails a check, and
	// is empty where there is no user choice or it passes.
	refused string
	// attached is the best claim of each datasource in precedence order, the
	// winner among them where the database named no instrument.
	attached []*identityClaim
	findings []gen.CreateFindingParams
	// counts counts the claims of each datasource's response, and notNaming
	// those dropped for not naming the identifier sent. The reason of a key
	// no claim won summarises both.
	counts    map[string]int
	notNaming map[string]int
	// offered holds the sets of consistent claims put to the user, each with
	// a stable identifier on its head. They are gathered from the claims that
	// the resolution dropped only because the key was sent under a bare
	// ticker. Where the key was sent under a venue
	// ticker that the provider filtered bare and no claim names the venue,
	// they are gathered from the survivors.
	offered []consistentClaims
}

// consistentClaims is a head claim and its members. For each datasource
// below the head, the member is the best ranked claim that is consistent
// with every member above it and corroborates the head.
// That is the claim a run would attach to the head as the winner. The claims
// are in precedence order, and one claim may belong to several sets.
type consistentClaims []*identityClaim

// head is the claim of the highest precedence datasource, which a
// confirmation writes as the winner.
func (a consistentClaims) head() *identityClaim { return a[0] }

// member returns the claim of a that fk made, nil where it has none.
func (a consistentClaims) member(fk *fetchKey) *identityClaim {
	for _, c := range a {
		if c.fetchKey == fk {
			return c
		}
	}
	return nil
}

// choose picks the winner for res. The database's claim, known, wins where
// it is not nil. Otherwise the head of userChoice wins where userChoice is
// not nil. Otherwise the best claim of the highest precedence datasource
// wins. The head must pass the stated checks and agree with known. Each
// other member of userChoice takes the place of its datasource's answer and
// attaches where it agrees with the claims already chosen. A confirmation
// writes only the claims of userChoice. The answer of a datasource with no
// member is checked for its findings, and none of its claims attach.
func choose(res *resolution, known *identityClaim, userChoice consistentClaims) claimVerdict {
	verdict := claimVerdict{winner: known, confirmed: userChoice != nil, counts: map[string]int{}, notNaming: map[string]int{}}
	var chosen []*identityClaim
	if known != nil {
		chosen = append(chosen, known)
	}
	if userChoice != nil {
		head := userChoice.head()
		if detail, ok := head.against(res); ok {
			verdict.refused = head.label(detail)
			return verdict
		}
		if known != nil {
			if detail, ok := head.inconsistent(known); ok {
				verdict.refused = head.label(detail)
				return verdict
			}
		}
		verdict.attached = append(verdict.attached, head)
		chosen = append(chosen, head)
		if verdict.winner == nil {
			verdict.winner = head
		}
	}
	var offered []*identityClaim
	for _, fk := range res.fetchKeys {
		if m := userChoice.member(fk); m != nil {
			if m == userChoice.head() {
				continue
			}
			if f, ok := m.rejects(chosen, res.mics); ok {
				verdict.findings = append(verdict.findings, f)
				continue
			}
			verdict.attached = append(verdict.attached, m)
			chosen = append(chosen, m)
			continue
		}
		best, more := verdict.bestClaim(fk, res, chosen)
		offered = append(offered, more...)
		if best == nil || userChoice != nil {
			continue
		}
		verdict.attached = append(verdict.attached, best)
		chosen = append(chosen, best)
		if verdict.winner == nil {
			verdict.winner = best
		}
	}
	verdict.offered = slices.DeleteFunc(gatherConsistentClaims(res, offered), func(a consistentClaims) bool { return !a.confirmable() })
	return verdict
}

// finding returns a finding of kind against fk's fetch key, with a step only
// when the kind is dropped.
func finding(fk *fetchKey, kind gen.FindingKind, step gen.DropStep, detail string) gen.CreateFindingParams {
	f := gen.CreateFindingParams{Kind: kind, FetchKeyID: ptr.To(fk.ID), Detail: ptr.To(detail)}
	if kind == gen.FindingKindDropped {
		f.Step = ptr.To(step)
	}
	return f
}

// dropped is a dropped finding against fk's fetch key at step.
func dropped(fk *fetchKey, step gen.DropStep, detail string) gen.CreateFindingParams {
	return finding(fk, gen.FindingKindDropped, step, detail)
}

// alternatives is what one served fetch key offers a key.
type alternatives struct {
	// count is the number of claims in the response, before any are dropped.
	count int
	// survivors name the identifier sent and agree with the statement.
	survivors []*identityClaim
	// offered holds the claims that agree with the statement but were
	// dropped only because the key was sent under a bare ticker. The user may
	// choose among them.
	offered []*identityClaim
	// fallback reports that the key was sent under a venue ticker the provider
	// filtered bare, and no claim names the venue, so every claim survives
	// the naming check.
	fallback bool
	// notNaming counts the claims dropped for not naming the identifier sent.
	notNaming int
	// findings records the claims dropped for disagreeing with the statement.
	findings []gen.CreateFindingParams
}

// filterAlternatives sorts the claims of fk into survivors, offered claims and
// findings for res. Under a global identifier, a claim survives if it names
// that identifier and agrees with the statement. If the provider filtered a
// venue ticker bare and no claim names the venue, every claim that agrees with
// the statement survives. Under a bare ticker no claim can name the identifier
// sent, so the claims that agree with the statement are offered instead. A
// confirmation, which confirmed reports, replaces the naming check for a bare
// ticker, so every claim that agrees with the statement survives.
func filterAlternatives(fk *fetchKey, res *resolution, confirmed bool) alternatives {
	sent := *fk.SentID
	gs := claims(fk, res.families)
	alts := alternatives{count: len(gs)}
	global := sent.IsGlobal()
	named := slices.ContainsFunc(gs, (*identityClaim).named)
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: sent.Value}
	alts.fallback = global && !named && sent.Type == types.IdentifierTypeMicTicker && slices.Contains(fk.Response.Filtered, ticker)
	for _, c := range gs {
		switch {
		case !global && !confirmed:
			alts.notNaming++
			if _, bad := c.against(res); !bad {
				alts.offered = append(alts.offered, c)
			}
			continue
		case global && !c.named() && !alts.fallback:
			alts.notNaming++
			continue
		}
		if detail, bad := c.against(res); bad {
			alts.findings = append(alts.findings, dropped(fk, gen.DropStepStated, c.label(detail)))
			continue
		}
		alts.survivors = append(alts.survivors, c)
	}
	return alts
}

// rejects returns the finding that drops c below chosen, which lists the
// winner first. It drops c when c contradicts a claim in chosen, or when c
// does not corroborate the winner. A contradiction of the database's claim is
// recorded as a contradiction rather than a drop.
func (c *identityClaim) rejects(chosen []*identityClaim, mics mic.MICMap) (gen.CreateFindingParams, bool) {
	for _, o := range chosen {
		if detail, ok := c.inconsistent(o); ok {
			kind := gen.FindingKindDropped
			if o.fetchKey == nil {
				kind = gen.FindingKindContradiction
			}
			return finding(c.fetchKey, kind, gen.DropStepPrecedence, c.label(detail)), true
		}
	}
	if len(chosen) > 0 && !c.corroborates(chosen[0], mics) {
		return dropped(c.fetchKey, gen.DropStepCorroboration, c.label("shares neither a stable identifier nor a same-day ticker in one market with "+chosen[0].ref())), true
	}
	return gen.CreateFindingParams{}, false
}

// rank orders the claims of one response for res, the best first: a claim
// naming the identifier sent, then one confirming more of the stated data.
// Callers keep the earlier of two claims that tie, so ties keep the
// response's order.
func rank(res *resolution) func(a, b *identityClaim) int {
	return func(a, b *identityClaim) int {
		if an, bn := a.named(), b.named(); an != bn {
			if an {
				return -1
			}
			return 1
		}
		return b.confirms(res) - a.confirms(res)
	}
}

// bestClaim returns the best surviving claim of fk for res, nil where none
// survives, and the claims fk offers the user. chosen holds the claims
// already chosen for the datasources above fk, the winner first. Under
// fallback with nothing chosen above fk, several survivors are offered rather
// than chosen among.
func (verdict *claimVerdict) bestClaim(fk *fetchKey, res *resolution, chosen []*identityClaim) (*identityClaim, []*identityClaim) {
	if fk.SentID == nil {
		return nil, nil
	}
	alts := filterAlternatives(fk, res, verdict.confirmed)
	verdict.counts[fk.Source] = alts.count
	if alts.notNaming > 0 {
		verdict.notNaming[fk.Source] += alts.notNaming
	}
	verdict.findings = append(verdict.findings, alts.findings...)
	offered := alts.offered
	survivors := slices.DeleteFunc(alts.survivors, func(c *identityClaim) bool {
		f, ok := c.rejects(chosen, res.mics)
		if ok {
			verdict.findings = append(verdict.findings, f)
		}
		return ok
	})
	if len(survivors) == 0 {
		return nil, offered
	}
	if alts.fallback && len(chosen) == 0 && len(survivors) > 1 {
		return nil, append(offered, survivors...)
	}
	slices.SortStableFunc(survivors, rank(res))
	return survivors[0], offered
}

// gatherConsistentClaims returns the sets of consistent claims among cs,
// which arrive in precedence order and then each datasource's order. Each
// set formed so far takes its member from the next datasource. A claim that
// joins no set heads a new one. Consistency between claims is not transitive,
// so precedence settles which claims a set holds, as it does in a run.
func gatherConsistentClaims(res *resolution, cs []*identityClaim) []consistentClaims {
	var out []consistentClaims
	for len(cs) > 0 {
		n := 1
		for n < len(cs) && cs[n].fetchKey == cs[0].fetchKey {
			n++
		}
		group := cs[:n]
		cs = cs[n:]
		joined := map[*identityClaim]bool{}
		for i, a := range out {
			var best *identityClaim
			for _, c := range group {
				if _, ok := c.rejects(a, res.mics); ok {
					continue
				}
				if best == nil || rank(res)(c, best) < 0 {
					best = c
				}
			}
			if best != nil {
				out[i] = append(a, best)
				joined[best] = true
			}
		}
		for _, c := range group {
			if !joined[c] {
				out = append(out, consistentClaims{c})
			}
		}
	}
	return out
}
