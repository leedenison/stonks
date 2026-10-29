package tickervalidity

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/types"
)

// derive returns what the facts of one ticker imply: the intervals in order,
// the changes they infer, and the conflicts among them.
func derive(f facts) derived {
	as, es := current(f.Assertions)
	ws, disagreements := winners(as, es)
	cover := types.Union(periods(f.AssertionCoverage))
	var cutting []Edge
	for _, e := range es {
		if !overruled(e, as) {
			cutting = append(cutting, e)
		}
	}
	bounds := cuts(cutting)
	var out []Interval
	var inferred []Inferred
	var start *time.Time
	i := 0
	for s := 0; s <= len(bounds); s++ {
		var end *time.Time
		if s < len(bounds) {
			end = &bounds[s]
		}
		var seg []winner
		for i < len(ws) && (end == nil || ws[i].from.Before(*end)) {
			seg = append(seg, ws[i])
			i++
		}
		ivs, inf := segment(spans(seg), start, end, cover)
		out = append(out, ivs...)
		inferred = append(inferred, inf...)
		start = end
	}
	return derived{
		Diagnosis: Diagnosis{
			Disagreements: disagreements, Broken: broken(ws, cutting),
			Missed: missed(inferred, f.AssertionCoverage, cover),
		},
		Intervals: join(out), Inferred: inferred, winners: ws,
	}
}

// overruled reports whether identifier attestations overrule the edge e by
// a bracket: the nearest attested days on either side of the cut name one
// instrument. One attestation that holds the days either side of the cut is
// such a bracket by itself. An overruled edge does not cut.
func overruled(e Edge, as []Assertion) bool {
	c := e.At
	var before, after *Assertion
	for i := range as {
		x := &as[i]
		if !x.Source.attested() {
			continue
		}
		if x.Period.From.Before(c) && (before == nil || !endBefore(*x, c).Before(endBefore(*before, c))) {
			before = x
		}
		if x.Period.Before.After(c) && (after == nil || startAfter(*x, c).Before(startAfter(*after, c))) {
			after = x
		}
	}
	return before != nil && after != nil && before.Instr != nil && same(before.Instr, after.Instr)
}

// endBefore returns the day after the last day of a that falls before c.
func endBefore(a Assertion, c time.Time) time.Time {
	if a.Period.Before.Before(c) {
		return a.Period.Before
	}
	return c
}

// startAfter returns the first day of a that falls on or after c.
func startAfter(a Assertion, c time.Time) time.Time {
	if a.Period.From.After(c) {
		return a.Period.From
	}
	return c
}

// missed returns each inferred change whose possible days one period of
// cover holds in full. cover is the union of the assertion coverage. Each
// change comes with the first period of assertion coverage that holds the
// first of those days.
func missed(inferred []Inferred, rows []AssertionCoverage, cover []types.Period) []MissedEvent {
	var out []MissedEvent
	for _, inf := range inferred {
		held := slices.ContainsFunc(cover, func(p types.Period) bool {
			return !inf.Period.From.Before(p.From) && !p.Before.Before(inf.Period.Before)
		})
		i := slices.IndexFunc(rows, func(p AssertionCoverage) bool { return p.Period.Contains(inf.Period.From) })
		if held && i >= 0 {
			out = append(out, MissedEvent{Inferred: inf, By: rows[i]})
		}
	}
	return out
}

// periods returns the period of each row of assertion coverage.
func periods(rows []AssertionCoverage) []types.Period {
	out := make([]types.Period, len(rows))
	for i, p := range rows {
		out[i] = p.Period
	}
	return out
}

// edges returns the edges the assertions state, in order of the assertions.
func edges(as []Assertion) []Edge {
	var out []Edge
	for _, a := range as {
		if a.EdgeFrom {
			out = append(out, Edge{At: a.Period.From, Assertion: a})
		}
		if a.EdgeBefore {
			out = append(out, Edge{At: a.Period.Before, Assertion: a})
		}
	}
	return out
}

// current returns what the later answers of each source leave of the
// assertions, and the edges they leave. The reach of an answer is the periods
// of its assertions and of its request. An assertion can come back as several
// pieces or none, and a piece keeps an edge only on a bound of the original.
// Pieces and edges keep the order of the assertions.
func current(as []Assertion) ([]Assertion, []Edge) {
	idx := make([]int, len(as))
	for i := range idx {
		idx[i] = i
	}
	slices.SortStableFunc(idx, func(i, j int) int {
		switch {
		case as[i].Source.newer(as[j].Source):
			return -1
		case as[j].Source.newer(as[i].Source):
			return 1
		}
		return 0
	})
	taken := map[Source][]types.Period{}
	pieces := make([][]Assertion, len(as))
	kept := make([][]Edge, len(as))
	for k := 0; k < len(idx); {
		j := k
		for j < len(idx) && !as[idx[k]].Source.newer(as[idx[j]].Source) {
			j++
		}
		for _, i := range idx[k:j] {
			later := types.Union(taken[as[i].Source.group()])
			pieces[i] = clip(as[i], later)
			for _, e := range edges(as[i : i+1]) {
				if !types.AnyContains(later, prev(e.At)) || !types.AnyContains(later, e.At) {
					kept[i] = append(kept[i], e)
				}
			}
		}
		for _, i := range idx[k:j] {
			g := as[i].Source.group()
			taken[g] = append(taken[g], as[i].Period)
			if asked := as[i].Source.Asked; asked.From.Before(asked.Before) {
				taken[g] = append(taken[g], asked)
			}
		}
		k = j
	}
	var out []Assertion
	var es []Edge
	for i := range as {
		out = append(out, pieces[i]...)
		es = append(es, kept[i]...)
	}
	return out, es
}

// clip returns the pieces of a outside the disjoint periods taken, which are
// in order.
func clip(a Assertion, taken []types.Period) []Assertion {
	var out []Assertion
	from := a.Period.From
	add := func(before time.Time) {
		if !from.Before(before) {
			return
		}
		p := a
		p.Period = types.Period{From: from, Before: before}
		p.EdgeFrom = a.EdgeFrom && from.Equal(a.Period.From)
		p.EdgeBefore = a.EdgeBefore && before.Equal(a.Period.Before)
		out = append(out, p)
	}
	for _, t := range taken {
		if !t.Before.After(from) {
			continue
		}
		if !t.From.Before(a.Period.Before) {
			break
		}
		if t.From.After(from) {
			add(t.From)
		}
		from = t.Before
	}
	add(a.Period.Before)
	return out
}

// cuts returns the distinct days of the edges in order.
func cuts(es []Edge) []time.Time {
	out := make([]time.Time, len(es))
	for i, e := range es {
		out[i] = e.At
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return slices.CompactFunc(out, func(a, b time.Time) bool { return a.Equal(b) })
}

// broken returns each edge that cuts between two consecutive winners of one
// instrument.
func broken(ws []winner, es []Edge) []BrokenBracket {
	var out []BrokenBracket
	for _, e := range es {
		i := slices.IndexFunc(ws, func(w winner) bool { return !w.from.Before(e.At) })
		if i <= 0 {
			continue
		}
		before, after := ws[i-1], ws[i]
		if before.instr != nil && same(before.instr, after.instr) {
			out = append(out, BrokenBracket{Edge: e, Instr: *before.instr, First: prev(before.before), Last: after.from})
		}
	}
	return out
}

// winner is what a ticker names over a run of days, by the assertion that won
// them. A nil instrument is nothing. A winner never spans a cut.
type winner struct {
	from, before time.Time
	instr        *uuid.UUID
	assertion    Assertion
}

// winners returns one winner per run of days that some counting assertion
// holds, in order of day. A datasource's negative assertion counts only after
// the same datasource asserted the ticker: a datasource that never knew a
// ticker cannot show that it retired. An administrator's negative assertion
// counts after any positive one. Where assertions that hold one day disagree,
// better picks the winner. winners also returns the datasources' assertions
// that name something other than the winner of their days.
func winners(as []Assertion, es []Edge) ([]winner, []Disagreement) {
	as = slices.Clone(as)
	slices.SortStableFunc(as, func(a, b Assertion) int { return a.Period.From.Compare(b.Period.From) })
	first := map[string]time.Time{}
	var earliest *time.Time
	for _, a := range as {
		if a.Instr == nil {
			continue
		}
		if earliest == nil {
			on := a.Period.From
			earliest = &on
		}
		if a.Source.attested() {
			continue
		}
		if _, ok := first[a.Source.Datasource]; !ok {
			first[a.Source.Datasource] = a.Period.From
		}
	}
	counts := func(a Assertion) bool {
		if a.Instr != nil {
			return true
		}
		if a.Source.attested() {
			return earliest != nil && earliest.Before(a.Period.From)
		}
		on, ok := first[a.Source.Datasource]
		return ok && on.Before(a.Period.From)
	}
	var counted []int
	var bounds []time.Time
	for i, a := range as {
		if counts(a) {
			counted = append(counted, i)
			bounds = append(bounds, a.Period.From, a.Period.Before)
		}
	}
	cut := map[time.Time]bool{}
	for _, e := range es {
		cut[e.At] = true
		bounds = append(bounds, e.At)
	}
	slices.SortFunc(bounds, func(a, b time.Time) int { return a.Compare(b) })
	bounds = slices.CompactFunc(bounds, func(a, b time.Time) bool { return a.Equal(b) })
	var out []winner
	var won []int
	var disagreements []Disagreement
	var lost [][2]int
	for k := 0; k+1 < len(bounds); k++ {
		from, before := bounds[k], bounds[k+1]
		best := -1
		var day []int
		for _, i := range counted {
			if as[i].Period.Contains(from) {
				day = append(day, i)
				if best < 0 || better(as[i], as[best]) {
					best = i
				}
			}
		}
		if best < 0 {
			continue
		}
		if n := len(out); n > 0 && won[n-1] == best && out[n-1].before.Equal(from) && !cut[from] {
			out[n-1].before = before
		} else {
			out = append(out, winner{from: from, before: before, instr: as[best].Instr, assertion: as[best]})
			won = append(won, best)
		}
		if as[best].Source.attested() {
			continue
		}
		for _, i := range day {
			if as[i].Source.attested() || same(as[i].Instr, as[best].Instr) {
				continue
			}
			pair := [2]int{best, i}
			if j := slices.Index(lost, pair); j >= 0 && disagreements[j].Period.Before.Equal(from) {
				disagreements[j].Period.Before = before
				continue
			}
			disagreements = append(disagreements, Disagreement{Period: types.Period{From: from, Before: before}, Winner: as[best], Loser: as[i]})
			lost = append(lost, pair)
		}
	}
	return out, disagreements
}

// better reports whether a wins over b on their day: the higher standing, then
// the lower precedence number, then a positive assertion over a negative one,
// then the lower instrument id.
func better(a, b Assertion) bool {
	switch {
	case a.Source.outranks(b.Source):
		return true
	case b.Source.outranks(a.Source):
		return false
	case (a.Instr == nil) != (b.Instr == nil):
		return a.Instr != nil
	case a.Instr == nil:
		return false
	}
	return a.Instr.String() < b.Instr.String()
}

// span is consecutive winners that name one thing, from the assertion that
// won the first day to the one that won the last.
type span struct {
	instr        *uuid.UUID
	from, before time.Time
	head, tail   Assertion
}

func spans(ws []winner) []span {
	var out []span
	for _, w := range ws {
		if n := len(out); n > 0 && same(out[n-1].instr, w.instr) {
			out[n-1].before, out[n-1].tail = w.before, w.assertion
			continue
		}
		out = append(out, span{instr: w.instr, from: w.from, before: w.before, head: w.assertion, tail: w.assertion})
	}
	return out
}

func same(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// segment derives the intervals of the spans between two cuts, start and
// end. A nil start or end is an open end of the ticker.
func segment(ss []span, start, end *time.Time, cover []types.Period) ([]Interval, []Inferred) {
	var out []Interval
	var inferred []Inferred
	// floor is how far back the next span may extend: an earlier instrument
	// keeps the days up to the next assertion, and a retirement keeps the
	// days after it.
	floor := start
	for k, r := range ss {
		ceil := end
		if k+1 < len(ss) {
			ceil = &ss[k+1].from
		}
		if r.instr == nil {
			after := r.before
			floor = &after
		} else {
			from := back(r.from, floor, cover)
			to := forward(r.before, ceil, cover)
			if k == 0 && (floor == nil || floor.Before(from)) {
				out = append(out, Interval{From: floor, Before: &from, Instr: *r.instr, Validity: Provisional})
			}
			out = append(out, Interval{From: &from, Before: &to, Instr: *r.instr, Validity: Confirmed})
			if ceil == nil || to.Before(*ceil) {
				out = append(out, Interval{From: &to, Before: ceil, Instr: *r.instr, Validity: Provisional})
			}
			floor = ceil
		}
		if k+1 < len(ss) {
			took := r.before
			if r.instr != nil {
				took = forward(took, ceil, cover)
			}
			inferred = append(inferred, Inferred{
				Period: types.Period{From: took, Before: next(ss[k+1].from)},
				From:   r.instr, To: ss[k+1].instr,
				Earlier: r.tail, Later: ss[k+1].head,
			})
		}
	}
	return out, inferred
}

// forward extends the end of a confirmed range through covered days, no
// further than limit.
func forward(d time.Time, limit *time.Time, cover []types.Period) time.Time {
	for _, p := range cover {
		if p.Contains(d) {
			d = p.Before
		}
	}
	if limit != nil && limit.Before(d) {
		return *limit
	}
	return d
}

// back extends the start of a confirmed range through covered days, no
// further than limit.
func back(d time.Time, limit *time.Time, cover []types.Period) time.Time {
	for _, p := range slices.Backward(cover) {
		if p.Contains(d.AddDate(0, 0, -1)) {
			d = p.From
		}
	}
	if limit != nil && d.Before(*limit) {
		return *limit
	}
	return d
}

// join merges intervals that meet and agree on instrument and ticker validity.
func join(ivs []Interval) []Interval {
	var out []Interval
	for _, iv := range ivs {
		if n := len(out); n > 0 {
			prev := &out[n-1]
			if prev.Instr == iv.Instr && prev.Validity == iv.Validity && prev.Before != nil && iv.From != nil && prev.Before.Equal(*iv.From) {
				prev.Before = iv.Before
				continue
			}
		}
		out = append(out, iv)
	}
	return out
}

func next(d time.Time) time.Time { return d.AddDate(0, 0, 1) }

func prev(d time.Time) time.Time { return d.AddDate(0, 0, -1) }
