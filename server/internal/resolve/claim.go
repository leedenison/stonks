package resolve

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
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

// contradicts reports whether c identifies what id identifies under a
// different value of id's type, returning the value that differs. Only the
// identifiers that may name what id names are compared: the instrument's for
// an instrument grain id; for a listing grain id, those of the listing in
// family fam and of the listing of no family, or of every listing where fam is
// noFamily.
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

// corroborates reports whether c shares a stable identifier with a, or a
// venue ticker both answered on one day.
func (c *identityClaim) corroborates(a *identityClaim) bool {
	return c.sharesStable(a) || c.sameDayVenueTicker(a)
}

// sharesStable reports whether c shares a stable identifier with a.
func (c *identityClaim) sharesStable(a *identityClaim) bool {
	for id := range c.AllIDs() {
		if id.Stable() && a.IdentifiedBy(id) {
			return true
		}
	}
	return false
}

// sameDayVenueTicker reports whether c and a are answers made on one UTC day
// that carry one MIC_TICKER.
func (c *identityClaim) sameDayVenueTicker(a *identityClaim) bool {
	if c.fetchKey == nil || a.fetchKey == nil || !types.Day(c.fetchKey.AnsweredAt).Equal(types.Day(a.fetchKey.AnsweredAt)) {
		return false
	}
	for id := range c.AllIDs() {
		if id.Type == types.IdentifierTypeMicTicker && a.IdentifiedBy(id) {
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
