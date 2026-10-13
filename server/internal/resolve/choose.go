package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/mirror"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// result is what one datasource served for a key.
type result = market.Result[gen.StatedKey, market.IdentityResult]

// idSet is one instrument a response describes: the candidates sharing an
// instrument grain identifier, transitively, collapsed to the listing grain
// identifiers of one listing per currency family.
type idSet struct {
	// r is the result that built the set, nil for the set of the instrument the
	// database names.
	r *result
	// order is the position of the set's first candidate in the response.
	order int
	// class is the class of the first candidate stating one.
	class      types.AssetClass
	instrument []types.Identifier
	// listings is keyed by currency family, noFamily where the candidates
	// carry no known currency.
	listings map[string][]types.Identifier
	// primary is the primary venue of each family's listing, from the
	// first candidate of the family stating one.
	primary map[string]string
	// named reports whether the identifier sent identifies the set.
	named bool
}

// choice is the outcome of choosing among the sets of identifiers of every
// response.
type choice struct {
	// winner is nil where no set survived.
	winner *idSet
	// picked reports a confirmation, whose pick replaces the naming check
	// for a key sent under an identifier that is not a GUID.
	picked bool
	// refused says why the pick of a confirmation fails a check, and is
	// empty where there is no pick or it passes.
	refused string
	// attached is the best set of each datasource in precedence order, the
	// winner among them where the database named no instrument.
	attached []*idSet
	findings []gen.CreateFindingParams
	// setCounts counts the sets of each datasource's response, and notNaming
	// those dropped for not naming the identifier sent. The reason of a key no
	// set won summarises both.
	setCounts map[string]int
	notNaming map[string]int
	// offered holds the agreements from which the user may choose. They are
	// gathered from the sets that the resolution dropped only because the key
	// was sent under a bare ticker. Where the key was sent under a venue ticker
	// that the provider filtered bare and no set names the venue, they are
	// gathered from the survivors.
	offered []agreement
}

// agreement is a head set and its members. For each datasource below the
// head, the member is the best ranked set that is consistent with every
// member above it and shares a stable identifier with the head. That is the
// set a run would attach to the head as the winner. The sets are in
// precedence order, and one set may belong to several agreements.
type agreement []*idSet

// head is the set of the highest precedence datasource, which a
// confirmation writes as the winner.
func (a agreement) head() *idSet { return a[0] }

// member returns the set of a that r served, nil where it has none.
func (a agreement) member(r *result) *idSet {
	for _, g := range a {
		if g.r == r {
			return g
		}
	}
	return nil
}

// idSets builds the sets of identifiers of r. The candidates of r carry
// currency families, not currency codes. Every set carries the instrument
// grain identifiers the call filtered on.
func idSets(r *result) []*idSet {
	var strict []types.Identifier
	for _, id := range r.Response.Filtered {
		if grain(id) == gen.IdentifierGrainInstrument {
			strict = append(strict, id)
		}
	}
	cs := r.Response.Candidates
	roots := partition(cs, len(strict) > 0)
	byRoot := map[int]*idSet{}
	var out []*idSet
	for i, c := range cs {
		g := byRoot[roots[i]]
		if g == nil {
			g = &idSet{r: r, order: i, listings: map[string][]types.Identifier{}}
			g.instrument = append(g.instrument, strict...)
			byRoot[roots[i]] = g
			out = append(out, g)
		}
		g.add(c, c.Currency)
	}
	if r.Sent != nil {
		for _, g := range out {
			g.named = g.identifiedBy(*r.Sent)
		}
	}
	return out
}

// partition returns, for each candidate, the index of the candidate
// representing its set: a union over the instrument grain identifiers
// candidates share, transitively, or one set where the call was strict.
func partition(cs []market.Candidate, strict bool) []int {
	parent := make([]int, len(cs))
	owner := map[types.Identifier]int{}
	for i, c := range cs {
		parent[i] = i
		if strict {
			union(parent, 0, i)
		}
		for _, id := range c.Identifiers {
			if grain(id) != gen.IdentifierGrainInstrument {
				continue
			}
			if j, ok := owner[id]; ok {
				union(parent, j, i)
			} else {
				owner[id] = i
			}
		}
	}
	roots := make([]int, len(cs))
	for i := range cs {
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

// add folds c into g as a candidate of the listing of fam.
func (g *idSet) add(c market.Candidate, fam string) {
	if g.class == "" || g.class == types.AssetClassUnknown {
		g.class = c.Class
	}
	l := g.listings[fam]
	for _, id := range c.Identifiers {
		if grain(id) == gen.IdentifierGrainInstrument {
			g.instrument = appendUnique(g.instrument, id)
		} else {
			l = appendUnique(l, id)
		}
	}
	g.listings[fam] = l
	if c.Primary != "" && g.primary[fam] == "" {
		if g.primary == nil {
			g.primary = map[string]string{}
		}
		g.primary[fam] = c.Primary
	}
}

func appendUnique(ids []types.Identifier, id types.Identifier) []types.Identifier {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

// all yields every identifier of g at either grain.
func (g *idSet) all(yield func(types.Identifier) bool) {
	for _, id := range g.instrument {
		if !yield(id) {
			return
		}
	}
	for _, f := range slices.Sorted(maps.Keys(g.listings)) {
		for _, id := range g.listings[f] {
			if !yield(id) {
				return
			}
		}
	}
}

// identifiedBy reports whether id is among g's identifiers at either grain.
func (g *idSet) identifiedBy(id types.Identifier) bool {
	for h := range g.all {
		if h == id {
			return true
		}
	}
	return false
}

// listed returns the families where g has a listing, sorted, leaving out
// the listing of no family.
func (g *idSet) listed() []string {
	var out []string
	for f := range g.listings {
		if f != noFamily {
			out = append(out, f)
		}
	}
	slices.Sort(out)
	return out
}

// contradicts reports whether g names id's subject by another value,
// returning the value that differs. Only the identifiers that may name what
// id names are compared: the instrument's for an instrument grain id; for a
// listing grain id, those of the listing in family fam and of the listing
// of no family, or of every listing where fam is noFamily.
func (g *idSet) contradicts(id types.Identifier, fam string) (types.Identifier, bool) {
	if !exclusive(id) {
		return types.Identifier{}, false
	}
	pool := g.instrument
	if grain(id) == gen.IdentifierGrainListing {
		pool = nil
		for _, f := range slices.Sorted(maps.Keys(g.listings)) {
			if fam == noFamily || f == fam || f == noFamily {
				pool = append(pool, g.listings[f]...)
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

// from names where g came from: the database for the instrument the key
// names, else the datasource that served it.
func (g *idSet) from() string {
	if g.r == nil {
		return "database"
	}
	return g.r.Source
}

// strongest returns the identifier of g that names it most firmly, the
// first of equal strength, and false where g carries none.
func (g *idSet) strongest() (types.Identifier, bool) {
	var best types.Identifier
	found := false
	for id := range g.all {
		if !found || strength(id) < strength(best) {
			best, found = id, true
		}
	}
	return best, found
}

// label leads a detail about g with its strongest identifier.
func (g *idSet) label(detail string) string {
	if id, ok := g.strongest(); ok {
		return name(id) + ": " + detail
	}
	return detail
}

// ref names g as the other side of a comparison: the instrument or the
// candidate, its strongest identifier where it has one, and its source.
func (g *idSet) ref() string {
	what := "the instrument"
	if g.r != nil {
		what = "the candidate"
	}
	if id, ok := g.strongest(); ok {
		what += " identified by " + name(id)
	}
	return fmt.Sprintf("%s (%s)", what, g.from())
}

// against returns why what res states contradicts g, if it does. The stated
// side leads the detail, and the datasource that served g closes it.
func (g *idSet) against(res *resolution) (string, bool) {
	detail, ok := g.contradicted(res)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s (%s)", detail, g.r.Source), true
}

// contradicted returns why what res states contradicts g. A limited answer
// is silent about the listings it lacks, so it never contradicts the stated
// currency family.
func (g *idSet) contradicted(res *resolution) (string, bool) {
	k, fam := res.row, res.fam
	if listed := g.listed(); !g.r.Response.Limited && fam != noFamily && len(listed) > 0 && !slices.Contains(listed, fam) {
		if _, unknown := g.listings[noFamily]; !unknown {
			return fmt.Sprintf("stated %s has no listing among %s", fam, strings.Join(listed, ", ")), true
		}
	}
	if k.AssetClass != nil && mirror.Disjoint(g.class, *k.AssetClass) {
		return fmt.Sprintf("stated %s contradicts the class %s", *k.AssetClass, g.class), true
	}
	for _, id := range k.Identifiers {
		if other, ok := g.contradicts(id, fam); ok {
			return fmt.Sprintf("stated %s contradicts %s", name(id), name(other)), true
		}
	}
	return "", false
}

// inconsistent returns why g contradicts a, a set of identifiers chosen above
// it, if it does. The detail closes with a's source.
func (g *idSet) inconsistent(a *idSet) (string, bool) {
	if mirror.Disjoint(g.class, a.class) {
		return fmt.Sprintf("class %s contradicts the class %s (%s)", g.class, a.class, a.from()), true
	}
	for _, id := range a.instrument {
		if other, ok := g.contradicts(id, noFamily); ok {
			return fmt.Sprintf("%s contradicts %s (%s)", name(other), name(id), a.from()), true
		}
	}
	for _, f := range slices.Sorted(maps.Keys(a.listings)) {
		for _, id := range a.listings[f] {
			if other, ok := g.contradicts(id, f); ok {
				return fmt.Sprintf("%s contradicts %s (%s)", name(other), name(id), a.from()), true
			}
		}
	}
	return "", false
}

// corroborates reports whether g shares a stable identifier with a.
func (g *idSet) corroborates(a *idSet) bool {
	for id := range g.all {
		if stable(id) && a.identifiedBy(id) {
			return true
		}
	}
	return false
}

// confirms counts the stated identifiers identifying g, and the stated family
// where it has that listing.
func (g *idSet) confirms(res *resolution) int {
	n := 0
	for _, id := range res.row.Identifiers {
		if g.identifiedBy(id) {
			n++
		}
	}
	if _, ok := g.listings[res.fam]; ok && res.fam != noFamily {
		n++
	}
	return n
}

// choose picks the winner for res. The database's set, db, wins where it is
// not nil. Otherwise the head of the user's confirmed agreement, pick, wins
// where pick is not nil. Otherwise the best set of the highest precedence
// datasource wins. The head must pass the stated checks and agree with db.
// Each other member of pick takes the place of its datasource's answer. It
// attaches where it agrees with the sets that choose has already chosen. A
// confirmation writes only the sets of pick. The answer of a datasource with
// no member is checked for its findings, and none of its sets attach.
func choose(res *resolution, db *idSet, pick agreement) choice {
	c := choice{winner: db, picked: pick != nil, setCounts: map[string]int{}, notNaming: map[string]int{}}
	var chosen []*idSet
	if db != nil {
		chosen = append(chosen, db)
	}
	if pick != nil {
		head := pick.head()
		if detail, ok := head.against(res); ok {
			c.refused = head.label(detail)
			return c
		}
		if db != nil {
			if detail, ok := head.inconsistent(db); ok {
				c.refused = head.label(detail)
				return c
			}
		}
		c.attached = append(c.attached, head)
		chosen = append(chosen, head)
		if c.winner == nil {
			c.winner = head
		}
	}
	var offered []*idSet
	for _, r := range res.results {
		if m := pick.member(r); m != nil {
			if m == pick.head() {
				continue
			}
			if f, ok := m.rejects(chosen); ok {
				c.findings = append(c.findings, f)
				continue
			}
			c.attached = append(c.attached, m)
			chosen = append(chosen, m)
			continue
		}
		best, more := c.bestSet(r, res, chosen)
		offered = append(offered, more...)
		if best == nil || pick != nil {
			continue
		}
		c.attached = append(c.attached, best)
		chosen = append(chosen, best)
		if c.winner == nil {
			c.winner = best
		}
	}
	c.offered = gather(res, offered)
	return c
}

// finding returns a finding of kind against r's fetch key, with a step only
// when the kind is dropped.
func finding(r *result, kind gen.FindingKind, step gen.DropStep, detail string) gen.CreateFindingParams {
	f := gen.CreateFindingParams{Kind: kind, FetchKeyID: ptr.To(r.ID), Detail: ptr.To(detail)}
	if kind == gen.FindingKindDropped {
		f.Step = ptr.To(step)
	}
	return f
}

// dropped is a dropped finding against r's fetch key at step.
func dropped(r *result, step gen.DropStep, detail string) gen.CreateFindingParams {
	return finding(r, gen.FindingKindDropped, step, detail)
}

// alternatives is what one served result offers a key.
type alternatives struct {
	// setCount counts the sets of the result.
	setCount int
	// survivors name the identifier sent and agree with the statement.
	survivors []*idSet
	// offered holds the sets that agree with the statement but were dropped
	// only because the key was sent under a bare ticker. The user may choose
	// among them.
	offered []*idSet
	// fallback reports that the key was sent under a venue ticker the provider
	// filtered bare, and no set names the venue, so every set survives the
	// naming check.
	fallback bool
	// notNaming counts the sets dropped for not naming the identifier sent.
	notNaming int
	// findings records the sets dropped for disagreeing with the statement.
	findings []gen.CreateFindingParams
}

// filterAlternatives sorts the sets of identifiers of r into survivors,
// offers and findings for res. Under a GUID, a set survives if it names the
// GUID and agrees with the statement. If the provider filtered a venue
// ticker bare and no set names the venue, every set that agrees with the
// statement survives. Under a bare ticker no set can name the identifier
// sent, so the sets that agree with the statement are offered instead. A
// confirmation, which picked reports, replaces the naming check for a bare
// ticker, so every set that agrees with the statement survives.
func filterAlternatives(r *result, res *resolution, picked bool) alternatives {
	sent := *r.Sent
	gs := idSets(r)
	alts := alternatives{setCount: len(gs)}
	guid := mirror.IsGUID(sent)
	named := slices.ContainsFunc(gs, func(g *idSet) bool { return g.named })
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: sent.Value}
	alts.fallback = guid && !named && sent.Type == types.IdentifierTypeMicTicker && slices.Contains(r.Response.Filtered, ticker)
	for _, g := range gs {
		switch {
		case !guid && !picked:
			alts.notNaming++
			if _, bad := g.against(res); !bad {
				alts.offered = append(alts.offered, g)
			}
			continue
		case guid && !g.named && !alts.fallback:
			alts.notNaming++
			continue
		}
		if detail, bad := g.against(res); bad {
			alts.findings = append(alts.findings, dropped(r, gen.DropStepStated, g.label(detail)))
			continue
		}
		alts.survivors = append(alts.survivors, g)
	}
	return alts
}

// rejects returns the finding that drops g below chosen, which lists the
// winner first. It drops g when g contradicts a set in chosen, or when g
// shares no stable identifier with the winner. A contradiction of the
// database's set is recorded as a contradiction rather than a drop.
func (g *idSet) rejects(chosen []*idSet) (gen.CreateFindingParams, bool) {
	for _, o := range chosen {
		if detail, ok := g.inconsistent(o); ok {
			kind := gen.FindingKindDropped
			if o.r == nil {
				kind = gen.FindingKindContradiction
			}
			return finding(g.r, kind, gen.DropStepPrecedence, g.label(detail)), true
		}
	}
	if len(chosen) > 0 && !g.corroborates(chosen[0]) {
		return dropped(g.r, gen.DropStepCorroboration, g.label("shares no stable identifier with "+chosen[0].ref())), true
	}
	return gen.CreateFindingParams{}, false
}

// rank orders the sets of one response for res, the best first: a set
// naming the identifier sent, then one confirming more of the stated data,
// then the response's order.
func rank(res *resolution) func(a, b *idSet) int {
	return func(a, b *idSet) int {
		if a.named != b.named {
			if a.named {
				return -1
			}
			return 1
		}
		if d := b.confirms(res) - a.confirms(res); d != 0 {
			return d
		}
		return a.order - b.order
	}
}

// bestSet returns the best surviving set of r for res, nil where none
// survives, and the sets r offers the user. chosen holds the sets already
// chosen for the datasources above r, the winner first. Under fallback with nothing chosen above r, several
// survivors are offered rather than chosen among.
func (c *choice) bestSet(r *result, res *resolution, chosen []*idSet) (*idSet, []*idSet) {
	if r.Sent == nil {
		return nil, nil
	}
	alts := filterAlternatives(r, res, c.picked)
	c.setCounts[r.Source] = alts.setCount
	if alts.notNaming > 0 {
		c.notNaming[r.Source] += alts.notNaming
	}
	c.findings = append(c.findings, alts.findings...)
	offered := alts.offered
	survivors := slices.DeleteFunc(alts.survivors, func(g *idSet) bool {
		f, ok := g.rejects(chosen)
		if ok {
			c.findings = append(c.findings, f)
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

// gather returns the agreements among sets, which arrive in precedence order
// and then each datasource's order. Each agreement formed so far takes its
// member from the next datasource. A set that joins no agreement heads a new
// one. Agreement between sets is not transitive, so precedence settles which
// sets an agreement holds, as it does in a run.
func gather(res *resolution, sets []*idSet) []agreement {
	var out []agreement
	for len(sets) > 0 {
		n := 1
		for n < len(sets) && sets[n].r == sets[0].r {
			n++
		}
		group := sets[:n]
		sets = sets[n:]
		joined := map[*idSet]bool{}
		for i, a := range out {
			var best *idSet
			for _, g := range group {
				if _, ok := g.rejects(a); ok {
					continue
				}
				if best == nil || rank(res)(g, best) < 0 {
					best = g
				}
			}
			if best != nil {
				out[i] = append(a, best)
				joined[best] = true
			}
		}
		for _, g := range group {
			if !joined[g] {
				out = append(out, agreement{g})
			}
		}
	}
	return out
}
