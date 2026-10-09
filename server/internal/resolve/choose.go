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
	// listings is keyed by currency family, noFamily where the candidates
	// carry no known currency.
	listings map[string][]types.Identifier
	// named reports whether the identifier the key was sent under
	// identifies the group.
	named bool
}

// choice is the outcome of choosing among the groups of every response.
type choice struct {
	// winner is nil where no group survived.
	winner *group
	// picked reports a confirmation, whose pick replaces the naming check
	// for a key sent under an identifier that is not a GUID.
	picked bool
	// refused says why the pick of a confirmation fails a check, and is
	// empty where there is no pick or it passes.
	refused string
	// attached is the best group of each datasource in precedence order,
	// the winner among them where the database named no instrument.
	attached []*group
	findings []gen.CreateFindingParams
	// groups counts the groups of each datasource's response, and notNaming
	// those dropped for not naming the identifier sent. The reason of a key
	// no group won summarises both.
	groups    map[string]int
	notNaming map[string]int
	// offered counts the groups dropped only because the key was sent under
	// a bare ticker. A confirmation lists them, and the user picks one.
	offered int
}

// groups builds the groups of r. The candidates of r carry currency
// families, not currency codes. Every group carries the instrument grain
// identifiers the call filtered on.
func groups(r *result) []*group {
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

// listed returns the families where g has a listing, sorted, leaving out
// the listing of no family.
func (g *group) listed() []string {
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
func (g *group) contradicts(id types.Identifier, fam string) (types.Identifier, bool) {
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

// against returns why what res states contradicts g, if it does. The stated
// side leads the detail, and the datasource that served g closes it.
func (g *group) against(res *resolution) (string, bool) {
	detail, ok := g.contradicted(res)
	if !ok {
		return "", false
	}
	return fmt.Sprintf("%s (%s)", detail, g.r.Source), true
}

// contradicted returns why what res states contradicts g. A limited answer
// is silent about the listings it lacks, so it never contradicts the stated
// currency family.
func (g *group) contradicted(res *resolution) (string, bool) {
	k, fam := res.row, res.fam
	if listed := g.listed(); !g.r.Response.Limited && fam != noFamily && len(listed) > 0 && !slices.Contains(listed, fam) {
		if _, unknown := g.listings[noFamily]; !unknown {
			return fmt.Sprintf("stated %s has no listing among %s", fam, strings.Join(listed, ", ")), true
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
func (g *group) confirms(res *resolution) int {
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

// choose picks the winner for res. db, the group of the instrument the
// database names, wins where it is not nil; otherwise pick, the group the
// user confirms, does where it is not nil; otherwise the best group of the
// highest precedence datasource does. A pick must pass the stated checks
// and agree with the database group, and it takes the place of its
// datasource's answer.
func choose(res *resolution, db, pick *group) choice {
	c := choice{winner: db, picked: pick != nil, groups: map[string]int{}, notNaming: map[string]int{}}
	var chosen []*group
	if db != nil {
		chosen = append(chosen, db)
	}
	if pick != nil {
		if detail, ok := pick.against(res); ok {
			c.refused = pick.label(detail)
			return c
		}
		if db != nil {
			if detail, ok := pick.inconsistent(db); ok {
				c.refused = pick.label(detail)
				return c
			}
		}
		c.attached = append(c.attached, pick)
		chosen = append(chosen, pick)
		if c.winner == nil {
			c.winner = pick
		}
	}
	for _, r := range res.results {
		if pick != nil && r == pick.r {
			continue
		}
		best := c.bestGroup(r, res, chosen)
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

// dropped is a dropped finding against r's fetch key at step.
func dropped(r *result, step gen.DropStep, detail string) gen.CreateFindingParams {
	return gen.CreateFindingParams{Kind: gen.FindingKindDropped, FetchKeyID: ptr.To(r.ID), Detail: ptr.To(detail), Step: ptr.To(step)}
}

// naming filters gs to the groups that carry the identifier r was sent.
// When that identifier is not a GUID, naming drops every group and counts
// as offered each one that agrees with the statement.
func (c *choice) naming(r *result, gs []*group, res *resolution) ([]*group, bool) {
	sent := *r.Sent
	nonGUID := !market.IsGUID(sent)
	named := slices.ContainsFunc(gs, func(g *group) bool { return g.named })
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: sent.Value}
	fallback := !named && sent.Type == types.IdentifierTypeMicTicker && !nonGUID && slices.Contains(r.Response.Filtered, ticker)
	var survivors []*group
	for _, g := range gs {
		if nonGUID {
			c.notNaming[r.Source]++
			if _, bad := g.against(res); !bad {
				c.offered++
			}
			continue
		}
		if !g.named && !fallback {
			c.notNaming[r.Source]++
			continue
		}
		survivors = append(survivors, g)
	}
	return survivors, fallback
}

// bestGroup returns the best surviving group of r for res, nil where none
// survives. chosen is the groups chosen above r, the winner first.
func (c *choice) bestGroup(r *result, res *resolution, chosen []*group) *group {
	if r.Sent == nil {
		return nil
	}
	gs := groups(r)
	c.groups[r.Source] = len(gs)
	survivors, fallback := gs, false
	if !c.picked || market.IsGUID(*r.Sent) {
		survivors, fallback = c.naming(r, gs, res)
	}
	survivors = slices.DeleteFunc(survivors, func(g *group) bool {
		if detail, ok := g.against(res); ok {
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
		if d := b.confirms(res) - a.confirms(res); d != 0 {
			return d
		}
		return a.order - b.order
	})
	return survivors[0]
}
