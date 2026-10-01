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
type result = market.Result[market.StatedKey, market.IdentityResult]

// group is one instrument a response describes: the candidates sharing an
// instrument grain identifier, transitively, collapsed to the listing grain
// identifiers of one listing per currency family, the family empty where
// the candidates carry no currency. r is the result the group came from,
// nil for the group of what the database holds. order is the position of
// its first candidate in the response, class the class of the first
// candidate stating one, and named whether it holds the identifier the key
// was sent under.
type group struct {
	r          *result
	order      int
	class      gen.AssetClass
	instrument []types.Identifier
	listings   map[string][]types.Identifier
	named      bool
}

// routine counts the drops of one step that are summarised rather than
// recorded: those not naming the identifier sent, and those outranked.
type routine struct {
	source string
	step   gen.DropStep
}

// choice is the outcome of choosing among the groups of every response.
// winner is nil where no group survived; attached are the top groups of the
// datasources in precedence order, the winner among them where the
// database did not hold the key. findings are the drops worth recording,
// each with its kind, fetch key, step and detail and awaiting the run and
// the stated key.
type choice struct {
	winner   *group
	attached []*group
	findings []gen.CreateFindingParams
	routine  map[routine]int
}

// groups builds the groups of r: a union over the instrument grain
// identifiers candidates share, every candidate one group where the call
// filtered on an instrument grain identifier. families maps a currency code
// to its family.
func groups(r *result, families func(string) string) []*group {
	var strict []types.Identifier
	for _, id := range r.Response.Filtered {
		if grain(id) == gen.IdentifierGrainInstrument {
			strict = append(strict, id)
		}
	}
	cs := r.Response.Candidates
	parent := make([]int, len(cs))
	owner := map[types.Identifier]int{}
	for i, c := range cs {
		parent[i] = i
		if len(strict) > 0 {
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
	byRoot := map[int]*group{}
	var out []*group
	for i, c := range cs {
		root := find(parent, i)
		g := byRoot[root]
		if g == nil {
			g = &group{r: r, order: i, listings: map[string][]types.Identifier{}}
			g.instrument = append(g.instrument, strict...)
			byRoot[root] = g
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
			g.named = g.holds(*r.Sent)
		}
	}
	return out
}

func find(parent []int, i int) int {
	for parent[i] != i {
		parent[i] = parent[parent[i]]
		i = parent[i]
	}
	return i
}

func union(parent []int, a, b int) {
	ra, rb := find(parent, a), find(parent, b)
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

func (g *group) holds(id types.Identifier) bool {
	for held := range g.all {
		if held == id {
			return true
		}
	}
	return false
}

// families returns the families g has a listing in, sorted, leaving out
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

// contradicts returns the value g holds of id's type and domain where none
// equals id's. A type of which a listing holds several values contradicts
// nothing.
func (g *group) contradicts(id types.Identifier) (types.Identifier, bool) {
	if multi[id.Type] {
		return types.Identifier{}, false
	}
	var held types.Identifier
	found := false
	for h := range g.all {
		if h.Type != id.Type || h.Domain != id.Domain {
			continue
		}
		if h.Value == id.Value {
			return types.Identifier{}, false
		}
		held, found = h, true
	}
	return held, found
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
		if held, ok := g.contradicts(id); ok {
			return fmt.Sprintf("%s contradicts the stated %s", name(held), name(id)), true
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
		if held, ok := g.contradicts(id); ok {
			return fmt.Sprintf("%s contradicts %s's %s", name(held), a.who(), name(id)), true
		}
	}
	return "", false
}

// corroborates reports whether g shares a stable identifier with a.
func (g *group) corroborates(a *group) bool {
	for id := range g.all {
		if stable(id) && a.holds(id) {
			return true
		}
	}
	return false
}

// confirms counts the stated identifiers g holds, and the stated family
// where it has that listing.
func (g *group) confirms(k gen.StatedKey, fam string) int {
	n := 0
	for _, id := range k.Identifiers {
		if g.holds(id) {
			n++
		}
	}
	if _, ok := g.listings[fam]; ok && fam != "" {
		n++
	}
	return n
}

// choose chooses among the groups of results, taken in precedence order,
// for the key k. db is the group of the instrument the database holds for
// the key, nil where it holds none, and families maps a currency code to
// its family.
func choose(results []*result, k gen.StatedKey, db *group, families func(string) string) choice {
	c := choice{winner: db, routine: map[routine]int{}}
	var chosen []*group
	if db != nil {
		chosen = append(chosen, db)
	}
	fam := family(k, families)
	for _, r := range results {
		top := c.pick(r, groups(r, families), k, fam, chosen)
		if top == nil {
			continue
		}
		c.attached = append(c.attached, top)
		chosen = append(chosen, top)
		if c.winner == nil {
			c.winner = top
		}
	}
	return c
}

// pick returns the top group of r for the key k stating the family fam,
// nil where none survives. chosen holds the groups chosen above r, the
// winner first.
func (c *choice) pick(r *result, gs []*group, k gen.StatedKey, fam string, chosen []*group) *group {
	if r.Sent == nil {
		return nil
	}
	record := func(kind gen.FindingKind, step gen.DropStep, detail string) {
		c.findings = append(c.findings, gen.CreateFindingParams{Kind: kind, FetchKeyID: ptr.To(r.ID), Step: ptr.To(step), Detail: ptr.To(detail)})
	}
	count := func(step gen.DropStep, n int) {
		if n > 0 {
			c.routine[routine{r.Source, step}] += n
		}
	}
	var survivors []*group
	sent := *r.Sent
	bare := sent.Domain == "" && grain(sent) == gen.IdentifierGrainListing
	named := slices.ContainsFunc(gs, func(g *group) bool { return g.named })
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: sent.Value}
	fallback := !named && sent.Type == types.IdentifierTypeMicTicker && !bare && slices.Contains(r.Response.Filtered, ticker)
	for _, g := range gs {
		if bare || (!g.named && !fallback) {
			count(gen.DropStepNaming, 1)
			continue
		}
		survivors = append(survivors, g)
	}
	survivors = slices.DeleteFunc(survivors, func(g *group) bool {
		if detail, ok := g.against(k, fam); ok {
			record(gen.FindingKindDropped, gen.DropStepStated, detail)
			return true
		}
		for _, a := range chosen {
			if detail, ok := g.inconsistent(a); ok {
				kind := gen.FindingKindDropped
				if a.r == nil {
					kind = gen.FindingKindContradiction
				}
				record(kind, gen.DropStepPrecedence, detail)
				return true
			}
		}
		if len(chosen) > 0 && !g.corroborates(chosen[0]) {
			record(gen.FindingKindDropped, gen.DropStepCorroboration, fmt.Sprintf("shares no stable identifier with %s", chosen[0].who()))
			return true
		}
		return false
	})
	if len(survivors) == 0 {
		return nil
	}
	if fallback && len(chosen) == 0 && len(survivors) > 1 {
		count(gen.DropStepNaming, len(survivors))
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
	count(gen.DropStepRank, len(survivors)-1)
	return survivors[0]
}
