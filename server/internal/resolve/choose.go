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
// identifiers of one listing per currency family, the family empty where
// the candidates carry no currency. r is the result from which the group
// was built, nil for the group of the instrument found in the database.
// order is the position of its first candidate in the response, class the
// class of the first candidate stating one, and named whether the
// identifier the key was sent under identifies it.
type group struct {
	r          *result
	order      int
	class      gen.AssetClass
	instrument []types.Identifier
	listings   map[string][]types.Identifier
	named      bool
}

// choice is the outcome of choosing among the groups of every response.
// winner is nil where no group survived; attached are the best groups of the
// datasources in precedence order, the winner among them where the
// database named no instrument for the key.
// notNaming counts, per datasource, the groups dropped for not naming the
// identifier sent, which the reason of a key no group won summarises.
type choice struct {
	winner    *group
	attached  []*group
	findings  []gen.CreateFindingParams
	notNaming map[string]int
}

// groups builds the groups of r, one per set partition finds, each carrying
// the instrument grain identifiers of the call's filter. families maps a
// currency code to its family.
func groups(r *result, families func(string) string) []*group {
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
		family := ""
		if c.Currency != "" {
			family = families(c.Currency)
		}
		g.add(c, family)
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

// add folds c into g as a candidate of the listing of family.
func (g *group) add(c market.Candidate, family string) {
	if g.class == "" || g.class == gen.AssetClassUnknown {
		g.class = c.Class
	}
	l, ok := g.listings[family]
	if !ok {
		g.listings[family] = nil
	}
	for _, id := range c.Identifiers {
		if grain(id) == gen.IdentifierGrainInstrument {
			g.instrument = appendUnique(g.instrument, id)
		} else {
			l = appendUnique(l, id)
		}
	}
	g.listings[family] = l
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

// contradicts reports whether g carries an identifier of id's type and
// domain but none with id's value, returning one that differs.
func (g *group) contradicts(id types.Identifier) (types.Identifier, bool) {
	if multi[id.Type] {
		return types.Identifier{}, false
	}
	var other types.Identifier
	found := false
	for h := range g.all {
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

// who names g in a detail.
func (g *group) who() string {
	if g.r == nil {
		return "the instrument"
	}
	return g.r.Source
}

// against returns why g contradicts what k states, if it does: a listing in
// no stated family fam, a disjoint class, or an identifier of a stated type
// and domain with another value.
func (g *group) against(k gen.StatedKey, fam string) (string, bool) {
	if fams := g.families(); fam != "" && len(fams) > 0 && !slices.Contains(fams, fam) {
		if _, unknown := g.listings[""]; !unknown {
			return fmt.Sprintf("listings in %s, none in the stated %s", strings.Join(fams, ", "), fam), true
		}
	}
	if k.AssetClass != nil && Disjoint(g.class, *k.AssetClass) {
		return fmt.Sprintf("class %s contradicts the stated %s", g.class, *k.AssetClass), true
	}
	for _, id := range k.Identifiers {
		if other, ok := g.contradicts(id); ok {
			return fmt.Sprintf("%s contradicts the stated %s", name(other), name(id)), true
		}
	}
	return "", false
}

// inconsistent returns why g contradicts a, a group chosen above it, if it
// does: a disjoint class, or an identifier of one type and domain with
// another value at either grain.
func (g *group) inconsistent(a *group) (string, bool) {
	if Disjoint(g.class, a.class) {
		return fmt.Sprintf("class %s contradicts %s's %s", g.class, a.who(), a.class), true
	}
	for id := range a.all {
		if other, ok := g.contradicts(id); ok {
			return fmt.Sprintf("%s contradicts %s's %s", name(other), a.who(), name(id)), true
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

// choose chooses among the groups of results, taken in precedence order,
// for the key k. db is the group of the instrument the database names for
// the key, nil where it names none, and families maps a currency code to
// its family.
func choose(results []*result, k gen.StatedKey, db *group, families func(string) string) choice {
	c := choice{winner: db, notNaming: map[string]int{}}
	var chosen []*group
	if db != nil {
		chosen = append(chosen, db)
	}
	fam := family(k, families)
	for _, r := range results {
		best := c.bestGroup(r, k, fam, families, chosen)
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
	bare := !market.IsGUID(sent)
	named := slices.ContainsFunc(gs, func(g *group) bool { return g.named })
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: sent.Value}
	fallback := !named && sent.Type == types.IdentifierTypeMicTicker && !bare && slices.Contains(r.Response.Filtered, ticker)
	var survivors []*group
	for _, g := range gs {
		if bare || (!g.named && !fallback) {
			c.notNaming[r.Source]++
			continue
		}
		survivors = append(survivors, g)
	}
	return survivors, fallback
}

// bestGroup returns the best group of r for the key k stating the family
// fam, nil where none survives. chosen is the groups chosen above r, the
// winner first.
func (c *choice) bestGroup(r *result, k gen.StatedKey, fam string, families func(string) string, chosen []*group) *group {
	if r.Sent == nil {
		return nil
	}
	survivors, fallback := c.naming(r, groups(r, families))
	survivors = slices.DeleteFunc(survivors, func(g *group) bool {
		if detail, ok := g.against(k, fam); ok {
			c.record(r, gen.FindingKindDropped, gen.DropStepStated, detail)
			return true
		}
		for _, a := range chosen {
			if detail, ok := g.inconsistent(a); ok {
				kind := gen.FindingKindDropped
				if a.r == nil {
					kind = gen.FindingKindContradiction
				}
				c.record(r, kind, gen.DropStepPrecedence, detail)
				return true
			}
		}
		if len(chosen) > 0 && !g.corroborates(chosen[0]) {
			c.record(r, gen.FindingKindDropped, gen.DropStepCorroboration, fmt.Sprintf("shares no stable identifier with %s", chosen[0].who()))
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
