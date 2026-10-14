package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// fetchKey is one datasource asked about one stated key, and what it served.
type fetchKey = market.FetchKey[gen.StatedKey, market.IdentityResult]

// identityClaim is one source's account of which instrument a stated key
// names. In this package a claim is an identity claim. A response's claim
// gathers the returned listings sharing an instrument grain identifier,
// transitively, into one claim listing per currency family. The database's claim
// is the instrument it names for the key.
type identityClaim struct {
	// fetchKey is the fetch key whose response made the claim, nil for the
	// database's.
	fetchKey *fetchKey
	types.Instrument
}

// named reports whether the identifier sent identifies c.
func (c *identityClaim) named() bool {
	return c.fetchKey != nil && c.fetchKey.SentID != nil && c.IdentifiedBy(*c.fetchKey.SentID)
}

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
	// offered holds the sets of consistent claims put to the user. They are
	// gathered from the claims that the resolution dropped only because the
	// key was sent under a bare ticker. Where the key was sent under a venue
	// ticker that the provider filtered bare and no claim names the venue,
	// they are gathered from the survivors.
	offered []consistentClaims
}

// consistentClaims is a head claim and its members. For each datasource
// below the head, the member is the best ranked claim that is consistent
// with every member above it and shares a stable identifier with the head.
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

// claims builds the claims of fk in the order of their first returned
// listings. The listings of fk carry currency families, not currency codes. Every claim
// carries the instrument grain identifiers the call filtered on.
func claims(fk *fetchKey) []*identityClaim {
	var strict []types.Identifier
	for _, id := range fk.Response.Filtered {
		if id.Grain() == types.IdentifierGrainInstrument {
			strict = append(strict, id)
		}
	}
	ls := fk.Response.Listings
	roots := partition(ls, len(strict) > 0)
	byRoot := map[int]*identityClaim{}
	var out []*identityClaim
	for i, listing := range ls {
		c := byRoot[roots[i]]
		if c == nil {
			c = &identityClaim{fetchKey: fk, Instrument: types.Instrument{Listings: map[string]types.Listing{}}}
			c.Identifiers = append(c.Identifiers, strict...)
			byRoot[roots[i]] = c
			out = append(out, c)
		}
		c.add(listing, listing.Currency)
	}
	return out
}

// partition returns, for each returned listing, the index of the listing
// representing its claim: a union over the instrument grain identifiers
// the listings share, transitively, or one claim where the call was strict.
func partition(ls []market.Listing, strict bool) []int {
	parent := make([]int, len(ls))
	owner := map[types.Identifier]int{}
	for i, c := range ls {
		parent[i] = i
		if strict {
			union(parent, 0, i)
		}
		for _, id := range c.Identifiers {
			if id.Grain() != types.IdentifierGrainInstrument {
				continue
			}
			if j, ok := owner[id]; ok {
				union(parent, j, i)
			} else {
				owner[id] = i
			}
		}
	}
	roots := make([]int, len(ls))
	for i := range ls {
		roots[i] = root(parent, i)
	}
	return roots
}

func root(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}
	return i
}

func union(parent []int, a, b int) {
	ra, rb := root(parent, a), root(parent, b)
	if ra != rb {
		parent[rb] = ra
	}
}

// add folds a returned listing into the claim's listing of fam. The first
// returned listing to state a class sets the claim's class. The first of the
// family to state a primary venue sets the primary venue of the claim's
// listing.
func (c *identityClaim) add(listing market.Listing, fam string) {
	if c.Class == "" || c.Class == types.AssetClassUnknown {
		c.Class = listing.Class
	}
	l := c.Listings[fam]
	for _, id := range listing.Identifiers {
		if id.Grain() == types.IdentifierGrainInstrument {
			c.Identifiers = appendUnique(c.Identifiers, id)
		} else {
			l.Identifiers = appendUnique(l.Identifiers, id)
		}
	}
	if l.Primary == "" {
		l.Primary = listing.Primary
	}
	c.Listings[fam] = l
}

func appendUnique(ids []types.Identifier, id types.Identifier) []types.Identifier {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

// listed returns the families where c has a listing, sorted, leaving out
// the listing of no family.
func (c *identityClaim) listed() []string {
	var out []string
	for f := range c.Listings {
		if f != noFamily {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// contradicts reports whether c names id's subject by another value,
// returning the value that differs. Only the identifiers that may name what
// id names are compared: the instrument's for an instrument grain id; for a
// listing grain id, those of the listing in family fam and of the listing
// of no family, or of every listing where fam is noFamily.
func (c *identityClaim) contradicts(id types.Identifier, fam string) (types.Identifier, bool) {
	if !id.Exclusive() {
		return types.Identifier{}, false
	}
	pool := c.Identifiers
	if id.Grain() == types.IdentifierGrainListing {
		pool = nil
		for _, f := range slices.Sorted(maps.Keys(c.Listings)) {
			if fam == noFamily || f == fam || f == noFamily {
				pool = append(pool, c.Listings[f].Identifiers...)
			}
		}
	}
	var other types.Identifier
	found := false
	for _, h := range pool {
		if h.Type != id.Type || h.Domain != id.Domain {
			continue
		}
		if h.Value == id.Value {
			return types.Identifier{}, false
		}
		other, found = h, true
	}
	return other, found
}

// from names where c came from: the database for the instrument the key
// names, else the datasource that served it.
func (c *identityClaim) from() string {
	if c.fetchKey == nil {
		return "database"
	}
	return c.fetchKey.Source
}

// strongest returns the identifier of c that names it most firmly, the
// first of equal strength, and false where c carries none.
func (c *identityClaim) strongest() (types.Identifier, bool) {
	var best types.Identifier
	found := false
	for id := range c.AllIDs() {
		if !found || id.Strength() < best.Strength() {
			best, found = id, true
		}
	}
	return best, found
}

// label leads a detail about c with its strongest identifier.
func (c *identityClaim) label(detail string) string {
	if id, ok := c.strongest(); ok {
		return id.Name() + ": " + detail
	}
	return detail
}

// ref names c as the other side of a comparison: the instrument or the
// candidate, its strongest identifier where it has one, and its source.
func (c *identityClaim) ref() string {
	what := "the instrument"
	if c.fetchKey != nil {
		what = "the candidate"
	}
	if id, ok := c.strongest(); ok {
		what += " identified by " + id.Name()
	}
	return fmt.Sprintf("%s (%s)", what, c.from())
}

// against returns why what res states contradicts c, if it does. The stated
// side leads the detail, and the datasource that served c closes it.
func (c *identityClaim) against(res *resolution) (string, bool) {
	detail, ok := c.contradicted(res)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s (%s)", detail, c.fetchKey.Source), true
}

// contradicted returns why what res states contradicts c. A limited answer
// is silent about the listings it lacks, so it never contradicts the stated
// currency family.
func (c *identityClaim) contradicted(res *resolution) (string, bool) {
	k, fam := res.row, res.family
	if listed := c.listed(); !c.fetchKey.Response.Limited && fam != noFamily && len(listed) > 0 && !slices.Contains(listed, fam) {
		if _, unknown := c.Listings[noFamily]; !unknown {
			return fmt.Sprintf("stated %s has no listing among %s", fam, strings.Join(listed, ", ")), true
		}
	}
	if k.AssetClass != nil && c.Class.Disjoint(*k.AssetClass) {
		return fmt.Sprintf("stated %s contradicts the class %s", *k.AssetClass, c.Class), true
	}
	for _, id := range k.Identifiers {
		if other, ok := c.contradicts(id, fam); ok {
			return fmt.Sprintf("stated %s contradicts %s", id.Name(), other.Name()), true
		}
	}
	return "", false
}

// inconsistent returns why c contradicts a, a claim chosen above it, if it
// does. The detail closes with a's source.
func (c *identityClaim) inconsistent(a *identityClaim) (string, bool) {
	if c.Class.Disjoint(a.Class) {
		return fmt.Sprintf("class %s contradicts the class %s (%s)", c.Class, a.Class, a.from()), true
	}
	for _, id := range a.Identifiers {
		if other, ok := c.contradicts(id, noFamily); ok {
			return fmt.Sprintf("%s contradicts %s (%s)", other.Name(), id.Name(), a.from()), true
		}
	}
	for _, f := range slices.Sorted(maps.Keys(a.Listings)) {
		for _, id := range a.Listings[f].Identifiers {
			if other, ok := c.contradicts(id, f); ok {
				return fmt.Sprintf("%s contradicts %s (%s)", other.Name(), id.Name(), a.from()), true
			}
		}
	}
	return "", false
}

// corroborates reports whether c shares a stable identifier with a.
func (c *identityClaim) corroborates(a *identityClaim) bool {
	for id := range c.AllIDs() {
		if id.Stable() && a.IdentifiedBy(id) {
			return true
		}
	}
	return false
}

// confirms counts the stated identifiers identifying c, and the stated family
// where it has that listing.
func (c *identityClaim) confirms(res *resolution) int {
	n := 0
	for _, id := range res.row.Identifiers {
		if c.IdentifiedBy(id) {
			n++
		}
	}
	if _, ok := c.Listings[res.family]; ok && res.family != noFamily {
		n++
	}
	return n
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
			if f, ok := m.rejects(chosen); ok {
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
	verdict.offered = gatherConsistentClaims(res, offered)
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
	gs := claims(fk)
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
// shares no stable identifier with the winner. A contradiction of the
// database's claim is recorded as a contradiction rather than a drop.
func (c *identityClaim) rejects(chosen []*identityClaim) (gen.CreateFindingParams, bool) {
	for _, o := range chosen {
		if detail, ok := c.inconsistent(o); ok {
			kind := gen.FindingKindDropped
			if o.fetchKey == nil {
				kind = gen.FindingKindContradiction
			}
			return finding(c.fetchKey, kind, gen.DropStepPrecedence, c.label(detail)), true
		}
	}
	if len(chosen) > 0 && !c.corroborates(chosen[0]) {
		return dropped(c.fetchKey, gen.DropStepCorroboration, c.label("shares no stable identifier with "+chosen[0].ref())), true
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
		f, ok := c.rejects(chosen)
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
				if _, ok := c.rejects(a); ok {
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
