package tickervalidity

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Derive returns what the facts of one ticker imply: the intervals in order,
// the changes they infer, and the conflicts among them.
func Derive(f Facts) Derived {
	ws, disagreements := winners(f.Assertions)
	cover := union(periods(f.PeriodAssertions))
	var edges []Assertion
	var over []OverruledEdge
	for _, e := range edged(f.Assertions) {
		if by := overruled(e, cut(e), f); by != nil {
			over = append(over, OverruledEdge{Edge: e, By: by})
			continue
		}
		edges = append(edges, e)
	}
	bounds := cuts(edges)
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
		for i < len(ws) && (end == nil || ws[i].at.Before(*end)) {
			seg = append(seg, ws[i])
			i++
		}
		ivs, inf := segment(spans(seg), start, end, cover)
		out = append(out, ivs...)
		inferred = append(inferred, inf...)
		start = end
	}
	return Derived{
		Intervals: join(out), Inferred: inferred, Disagreements: disagreements,
		Broken: broken(ws, edges), Missed: missed(inferred, f.PeriodAssertions, cover), Overruled: over,
	}
}

// overruled returns the sources of the facts that overrule the edge a, whose
// cut is c. A source that outranks a's overrules it by a bracket: its nearest
// assertions on either side of c name one instrument. A datasource also
// overrules it by a period; see spanned. The strongest such source is the one
// returned.
func overruled(a Assertion, c time.Time, f Facts) []Source {
	var stronger []Source
	add := func(src Source) bool {
		if !src.outranks(a.Source) {
			return false
		}
		if !slices.Contains(stronger, src) {
			stronger = append(stronger, src)
		}
		return true
	}
	before := map[Source]Assertion{}
	after := map[Source]Assertion{}
	for _, x := range f.Assertions {
		src := x.Source.group()
		if !add(src) {
			continue
		}
		if x.At.Before(c) {
			if b, ok := before[src]; !ok || !x.At.Before(b.At) {
				before[src] = x
			}
		} else if n, ok := after[src]; !ok || x.At.Before(n.At) {
			after[src] = x
		}
	}
	for _, p := range f.PeriodAssertions {
		add(p.Source.group())
	}
	slices.SortStableFunc(stronger, func(x, y Source) int {
		switch {
		case x.outranks(y):
			return -1
		case y.outranks(x):
			return 1
		}
		return 0
	})
	for _, src := range stronger {
		b, okb := before[src]
		n, okn := after[src]
		if okb && okn && b.Instr != nil && same(b.Instr, n.Instr) {
			return []Source{b.Source, n.Source}
		}
		if p, ok := spanned(src, c, f); ok {
			return []Source{p.Source}
		}
	}
	return nil
}

// spanned returns the period assertion of the datasource src that holds the
// cut c, when src overrules an edge there by a period. Its period assertions
// together hold c and the day before, and src does not state an edge at c.
func spanned(src Source, c time.Time, f Facts) (PeriodAssertion, bool) {
	if src.Attested {
		return PeriodAssertion{}, false
	}
	var rows []PeriodAssertion
	for _, p := range f.PeriodAssertions {
		if p.Source.group() == src {
			rows = append(rows, p)
		}
	}
	held := slices.ContainsFunc(union(periods(rows)), func(p types.Period) bool {
		return p.Contains(c.AddDate(0, 0, -1)) && p.Contains(c)
	})
	if !held {
		return PeriodAssertion{}, false
	}
	if slices.ContainsFunc(f.Assertions, func(x Assertion) bool {
		return x.Edge != "" && x.Source.group() == src && cut(x).Equal(c)
	}) {
		return PeriodAssertion{}, false
	}
	i := slices.IndexFunc(rows, func(p PeriodAssertion) bool { return p.Period.Contains(c) })
	return rows[i], true
}

// missed returns each inferred change whose possible days one period of
// cover holds in full. cover is the union of the period assertions. Each
// change comes with the first period assertion that holds the first of those
// days.
func missed(inferred []Inferred, rows []PeriodAssertion, cover []types.Period) []MissedEvent {
	var out []MissedEvent
	for _, inf := range inferred {
		held := slices.ContainsFunc(cover, func(p types.Period) bool {
			return !inf.Period.From.Before(p.From) && !p.Before.Before(inf.Period.Before)
		})
		i := slices.IndexFunc(rows, func(p PeriodAssertion) bool { return p.Period.Contains(inf.Period.From) })
		if held && i >= 0 {
			out = append(out, MissedEvent{Inferred: inf, By: rows[i]})
		}
	}
	return out
}

// periods returns the period of each period assertion.
func periods(rows []PeriodAssertion) []types.Period {
	out := make([]types.Period, len(rows))
	for i, p := range rows {
		out[i] = p.Period
	}
	return out
}

// edged returns the assertions that carry an edge.
func edged(as []Assertion) []Assertion {
	var out []Assertion
	for _, a := range as {
		if a.Edge != "" {
			out = append(out, a)
		}
	}
	return out
}

// cut returns the day at whose start the edge a cuts the timeline: the day of
// a first, and the day after a last.
func cut(a Assertion) time.Time {
	if a.Edge == gen.IdentifierEdgeLast {
		return next(a.At)
	}
	return a.At
}

// cuts returns the distinct cuts of the edges in order.
func cuts(edges []Assertion) []time.Time {
	out := make([]time.Time, len(edges))
	for i, e := range edges {
		out[i] = cut(e)
	}
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return slices.CompactFunc(out, func(a, b time.Time) bool { return a.Equal(b) })
}

// broken returns each edge that cuts between two consecutive winners of one
// instrument. A cut at the start of a winner's day precedes it, so that winner
// is the later one.
func broken(ws []winner, edges []Assertion) []BrokenBracket {
	var out []BrokenBracket
	for _, e := range edges {
		c := cut(e)
		i := slices.IndexFunc(ws, func(w winner) bool { return !w.at.Before(c) })
		if i <= 0 {
			continue
		}
		before, after := ws[i-1], ws[i]
		if before.instr != nil && same(before.instr, after.instr) {
			out = append(out, BrokenBracket{Edge: e, Instr: *before.instr, First: before.at, Last: after.at})
		}
	}
	return out
}

// winner is what a ticker names on one day, by the assertion that won it. A nil
// instrument is nothing.
type winner struct {
	at        time.Time
	instr     *uuid.UUID
	assertion Assertion
}

// winners returns one winner per day that has an assertion that counts, in
// order of day. A datasource's negative assertion counts only after the same
// datasource asserted the ticker: a datasource that never knew a ticker cannot
// show that it retired. An administrator's negative assertion counts after
// any positive one. Where assertions of one day disagree, better picks the
// winner. winners also returns the datasources' assertions that name
// something other than the winner of their day.
func winners(as []Assertion) ([]winner, []Disagreement) {
	as = slices.Clone(as)
	slices.SortStableFunc(as, func(a, b Assertion) int { return a.At.Compare(b.At) })
	first := map[string]time.Time{}
	var earliest *time.Time
	for _, a := range as {
		if a.Instr == nil {
			continue
		}
		if earliest == nil {
			on := a.At
			earliest = &on
		}
		if a.Source.Attested {
			continue
		}
		if _, ok := first[a.Source.Datasource]; !ok {
			first[a.Source.Datasource] = a.At
		}
	}
	counts := func(a Assertion) bool {
		if a.Instr != nil {
			return true
		}
		if a.Source.Attested {
			return earliest != nil && earliest.Before(a.At)
		}
		on, ok := first[a.Source.Datasource]
		return ok && on.Before(a.At)
	}
	var out []winner
	var disagreements []Disagreement
	var best *Assertion
	var day []Assertion
	flush := func() {
		if best != nil {
			out = append(out, winner{at: best.At, instr: best.Instr, assertion: *best})
			for _, a := range day {
				if !best.Source.Attested && !same(a.Instr, best.Instr) {
					disagreements = append(disagreements, Disagreement{On: best.At, Winner: *best, Loser: a})
				}
			}
		}
		best, day = nil, nil
	}
	for i := range as {
		a := &as[i]
		if best != nil && !a.At.Equal(best.At) {
			flush()
		}
		if !counts(*a) {
			continue
		}
		if !a.Source.Attested {
			day = append(day, *a)
		}
		if best == nil || better(*a, *best) {
			best = a
		}
	}
	flush()
	return out, disagreements
}

// better reports whether a wins over b on their day: an administrator over a
// datasource, then the lower precedence number, then a positive assertion over
// a negative one, then the lower instrument id.
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
	instr       *uuid.UUID
	first, last time.Time
	head, tail  Assertion
}

func spans(ws []winner) []span {
	var out []span
	for _, w := range ws {
		if n := len(out); n > 0 && same(out[n-1].instr, w.instr) {
			out[n-1].last, out[n-1].tail = w.at, w.assertion
			continue
		}
		out = append(out, span{instr: w.instr, first: w.at, last: w.at, head: w.assertion, tail: w.assertion})
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
			ceil = &ss[k+1].first
		}
		if r.instr == nil {
			after := next(r.last)
			floor = &after
		} else {
			from := back(r.first, floor, cover)
			to := forward(next(r.last), ceil, cover)
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
			took := next(r.last)
			if r.instr != nil {
				took = forward(took, ceil, cover)
			}
			inferred = append(inferred, Inferred{
				Period: types.Period{From: took, Before: next(ss[k+1].first)},
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

// union returns the days the periods hold as disjoint periods, in order, none
// of which meet.
func union(ps []types.Period) []types.Period {
	ps = slices.Clone(ps)
	slices.SortFunc(ps, func(a, b types.Period) int { return a.From.Compare(b.From) })
	var out []types.Period
	for _, p := range ps {
		if n := len(out); n > 0 && !out[n-1].Before.Before(p.From) {
			if out[n-1].Before.Before(p.Before) {
				out[n-1].Before = p.Before
			}
			continue
		}
		out = append(out, p)
	}
	return out
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
