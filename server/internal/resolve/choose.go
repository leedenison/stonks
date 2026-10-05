package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// result is what one datasource served for a key.
type result = market.Result[gen.StatedKey, market.IdentityResult]

// group is one instrument a response describes: the candidates sharing an
// instrument grain identifier, transitively, collapsed to the listing grain
// identifiers of one listing per currency family.
type group struct {
	// r is the result the group was built from, nil for the group of the
	// instrument the database names.
	r *result
	// order is the position of the group's first candidate in the response.
	order int
	// class is the class of the first candidate stating one.
	class      gen.AssetClass
	instrument []types.Identifier
	// listings is keyed by currency family, "" where the candidates carry
	// no currency.
	listings map[string][]types.Identifier
	// named reports whether the identifier the key was sent under
	// identifies the group.
	named bool
}

// choice is the outcome of choosing among the groups of every response.
type choice struct {
	// winner is nil where no group survived.
	winner *group
	// attached is the best group of each datasource in precedence order,
	// the winner among them where the database named no instrument.
	attached []*group
	findings []gen.CreateFindingParams
	// groups counts the groups of each datasource's response, and notNaming
	// those dropped for not naming the identifier sent. The reason of a key
	// no group won summarises both.
	groups    map[string]int
	notNaming map[string]int
}

// groups builds the groups of r. Every group carries the instrument grain
// identifiers the call filtered on.
func groups(r *result, fams families) []*group {
	var strict []types.Identifier
	for _, id := range r.Response.Filtered {
		if grain(id) == gen.IdentifierGrainInstrument {
			strict = append(strict, id)
		}
	}
	cs := r.Response.Candidates
	roots := partition(cs, len(strict) > 0)
	byRoot := map[int]*group{}
	var out []*group
	for i, c := range cs {
		g := byRoot[roots[i]]
		if g == nil {
			g = &group{r: r, order: i, listings: map[string][]types.Identifier{}}
			g.instrument = append(g.instrument, strict...)
			byRoot[roots[i]] = g
			out = append(out, g)
		}
		g.add(c, fams[c.Currency])
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
func (g *group) add(c market.Candidate, fam string) {
	if g.class == "" || g.class == gen.AssetClassUnknown {
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
}

func appendUnique(ids []types.Identifier, id types.Identifier) []types.Identifier {
	if slices.Contains(ids, id) {
		return ids
	}
	return append(ids, id)
}

// all yields every identifier of g at either grain.
func (g *group) all(yield func(types.Identifier) bool) {
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
func (g *group) identifiedBy(id types.Identifier) bool {
	for h := range g.all {
		if h == id {
			return true
		}
	}
	return false
}

// families returns the families where g has a listing, sorted, leaving out
// the listing of no family.
func (g *group) families() []string {
	var out []string
	for f := range g.listings {
		if f != "" {
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
// of no family, or of every listing where fam is empty.
func (g *group) contradicts(id types.Identifier, fam string) (types.Identifier, bool) {
	if !exclusive(id) {
		return types.Identifier{}, false
	}
	pool := g.instrument
	if grain(id) == gen.IdentifierGrainListing {
		pool = nil
		for _, f := range slices.Sorted(maps.Keys(g.listings)) {
			if fam == "" || f == fam || f == "" {
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
func (g *group) from() string {
	if g.r == nil {
		return "database"
	}
	return g.r.Source
}

// strongest returns the identifier of g that names it most firmly, the
// first of equal strength, and false where g carries none.
func (g *group) strongest() (types.Identifier, bool) {
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
func (g *group) label(detail string) string {
	if id, ok := g.strongest(); ok {
		return name(id) + ": " + detail
	}
	return detail
}

// ref names g as the other side of a comparison: the instrument or the
// candidate, its strongest identifier where it has one, and its source.
func (g *group) ref() string {
	what := "the instrument"
	if g.r != nil {
		what = "the candidate"
	}
	if id, ok := g.strongest(); ok {
		what += " identified by " + name(id)
	}
	return fmt.Sprintf("%s (%s)", what, g.from())
}

// against returns why what k states contradicts g, if it does. The stated
// side leads the detail, and the datasource that served g closes it.
func (g *group) against(k gen.StatedKey, fam string) (string, bool) {
	detail, ok := g.contradicted(k, fam)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s (%s)", detail, g.r.Source), true
}

func (g *group) contradicted(k gen.StatedKey, fam string) (string, bool) {
	if fams := g.families(); fam != "" && len(fams) > 0 && !slices.Contains(fams, fam) {
		if _, unknown := g.listings[""]; !unknown {
			return fmt.Sprintf("stated %s has no listing among %s", fam, strings.Join(fams, ", ")), true
		}
	}
	if k.AssetClass != nil && disjoint(g.class, *k.AssetClass) {
		return fmt.Sprintf("stated %s contradicts the class %s", *k.AssetClass, g.class), true
	}
	for _, id := range k.Identifiers {
		if other, ok := g.contradicts(id, fam); ok {
			return fmt.Sprintf("stated %s contradicts %s", name(id), name(other)), true
		}
	}
	return "", false
}

// inconsistent returns why g contradicts a, a group chosen above it, if it
// does. The detail closes with a's source.
func (g *group) inconsistent(a *group) (string, bool) {
	if disjoint(g.class, a.class) {
		return fmt.Sprintf("class %s contradicts the class %s (%s)", g.class, a.class, a.from()), true
	}
	for _, id := range a.instrument {
		if other, ok := g.contradicts(id, ""); ok {
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
func (g *group) corroborates(a *group) bool {
	for id := range g.all {
		if stable(id) && a.identifiedBy(id) {
			return true
		}
	}
	return false
}

// confirms counts the stated identifiers identifying g, and the stated family
// where it has that listing.
func (g *group) confirms(k gen.StatedKey, fam string) int {
	n := 0
	for _, id := range k.Identifiers {
		if g.identifiedBy(id) {
			n++
		}
	}
	if _, ok := g.listings[fam]; ok && fam != "" {
		n++
	}
	return n
}

// choose picks the winner for res. db, the group of the instrument the
// database names, wins where it is not nil; otherwise the best group of the
// highest precedence datasource does.
func choose(res *resolution, db *group) choice {
	c := choice{winner: db, groups: map[string]int{}, notNaming: map[string]int{}}
	var chosen []*group
	if db != nil {
		chosen = append(chosen, db)
	}
	k := res.row
	fam := res.fams.family(k)
	for _, r := range res.results {
		best := c.bestGroup(r, k, fam, res.fams, chosen)
		if best == nil {
			continue
		}
		c.attached = append(c.attached, best)
		chosen = append(chosen, best)
		if c.winner == nil {
			c.winner = best
		}
	}
	return c
}

// record appends a finding against r's fetch key, with a step only when the
// kind is dropped.
func (c *choice) record(r *result, kind gen.FindingKind, step gen.DropStep, detail string) {
	f := gen.CreateFindingParams{Kind: kind, FetchKeyID: ptr.To(r.ID), Detail: ptr.To(detail)}
	if kind == gen.FindingKindDropped {
		f.Step = ptr.To(step)
	}
	c.findings = append(c.findings, f)
}

// naming filters gs to the groups that carry the identifier r was sent.
func (c *choice) naming(r *result, gs []*group) ([]*group, bool) {
	sent := *r.Sent
	nonGUID := !market.IsGUID(sent)
	named := slices.ContainsFunc(gs, func(g *group) bool { return g.named })
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: sent.Value}
	fallback := !named && sent.Type == types.IdentifierTypeMicTicker && !nonGUID && slices.Contains(r.Response.Filtered, ticker)
	var survivors []*group
	for _, g := range gs {
		if nonGUID || (!g.named && !fallback) {
			c.notNaming[r.Source]++
			continue
		}
		survivors = append(survivors, g)
	}
	return survivors, fallback
}

// bestGroup returns the best surviving group of r for k, nil where none
// survives. chosen is the groups chosen above r, the winner first.
func (c *choice) bestGroup(r *result, k gen.StatedKey, fam string, fams families, chosen []*group) *group {
	if r.Sent == nil {
		return nil
	}
	gs := groups(r, fams)
	c.groups[r.Source] = len(gs)
	survivors, fallback := c.naming(r, gs)
	survivors = slices.DeleteFunc(survivors, func(g *group) bool {
		if detail, ok := g.against(k, fam); ok {
			c.record(r, gen.FindingKindDropped, gen.DropStepStated, g.label(detail))
			return true
		}
		for _, a := range chosen {
			if detail, ok := g.inconsistent(a); ok {
				kind := gen.FindingKindDropped
				if a.r == nil {
					kind = gen.FindingKindContradiction
				}
				c.record(r, kind, gen.DropStepPrecedence, g.label(detail))
				return true
			}
		}
		if len(chosen) > 0 && !g.corroborates(chosen[0]) {
			c.record(r, gen.FindingKindDropped, gen.DropStepCorroboration, g.label("shares no stable identifier with "+chosen[0].ref()))
			return true
		}
		return false
	})
	if len(survivors) == 0 {
		return nil
	}
	if fallback && len(chosen) == 0 && len(survivors) > 1 {
		c.notNaming[r.Source] += len(survivors)
		return nil
	}
	slices.SortStableFunc(survivors, func(a, b *group) int {
		if a.named != b.named {
			if a.named {
				return -1
			}
			return 1
		}
		if d := b.confirms(k, fam) - a.confirms(k, fam); d != 0 {
			return d
		}
		return a.order - b.order
	})
	return survivors[0]
}
