package validity

import (
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Derive returns the intervals the facts of one value imply, in order, and the
// events they infer.
func Derive(f Facts) ([]Interval, []Inferred) {
	ws := winners(f.Assertions)
	cover := union(f.Coverage)
	bounds := days(f.Events)
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
	return join(out), inferred
}

// winner is what a value names on one day, by the assertion that won it. A nil
// instrument is nothing.
type winner struct {
	at         time.Time
	instrument *uuid.UUID
}

// winners returns one winner per day that has an assertion that counts, in
// order of day. A datasource's negative assertion counts only after the same
// datasource asserted the value: a datasource that never knew a value cannot
// show that it retired. An administrator's negative assertion counts after
// any positive one. Where assertions of one day disagree, better picks the
// winner.
func winners(as []Assertion) []winner {
	as = slices.Clone(as)
	slices.SortStableFunc(as, func(a, b Assertion) int { return a.At.Compare(b.At) })
	first := map[string]time.Time{}
	var earliest *time.Time
	for _, a := range as {
		if a.Instrument == nil {
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
		if a.Instrument != nil {
			return true
		}
		if a.Source.Attested {
			return earliest != nil && earliest.Before(a.At)
		}
		on, ok := first[a.Source.Datasource]
		return ok && on.Before(a.At)
	}
	var out []winner
	var best *Assertion
	flush := func() {
		if best != nil {
			out = append(out, winner{at: best.At, instrument: best.Instrument})
		}
		best = nil
	}
	for i := range as {
		a := &as[i]
		if best != nil && !a.At.Equal(best.At) {
			flush()
		}
		if counts(*a) && (best == nil || better(*a, *best)) {
			best = a
		}
	}
	flush()
	return out
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
	case (a.Instrument == nil) != (b.Instrument == nil):
		return a.Instrument != nil
	case a.Instrument == nil:
		return false
	}
	return a.Instrument.String() < b.Instrument.String()
}

// span is consecutive winners that name one thing.
type span struct {
	instrument  *uuid.UUID
	first, last time.Time
}

func spans(ws []winner) []span {
	var out []span
	for _, w := range ws {
		if n := len(out); n > 0 && same(out[n-1].instrument, w.instrument) {
			out[n-1].last = w.at
			continue
		}
		out = append(out, span{instrument: w.instrument, first: w.at, last: w.at})
	}
	return out
}

func same(a, b *uuid.UUID) bool {
	if a == nil || b == nil {
		return a == nil && b == nil
	}
	return *a == *b
}

// segment derives the intervals of the spans between two events, start and
// end. A nil start or end is an open end of the value.
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
		if r.instrument == nil {
			after := next(r.last)
			floor = &after
		} else {
			from := back(r.first, floor, cover)
			to := forward(next(r.last), ceil, cover)
			if k == 0 && (floor == nil || floor.Before(from)) {
				out = append(out, Interval{From: floor, Before: &from, Instrument: *r.instrument, Validity: gen.ValidityProvisional})
			}
			out = append(out, Interval{From: &from, Before: &to, Instrument: *r.instrument, Validity: gen.ValidityConfirmed})
			if ceil == nil || to.Before(*ceil) {
				out = append(out, Interval{From: &to, Before: ceil, Instrument: *r.instrument, Validity: gen.ValidityProvisional})
			}
			floor = ceil
		}
		if k+1 < len(ss) {
			took := next(r.last)
			if r.instrument != nil {
				took = forward(took, ceil, cover)
			}
			inferred = append(inferred, Inferred{
				Period: types.Period{From: took, Before: next(ss[k+1].first)},
				From:   r.instrument, To: ss[k+1].instrument,
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

// days returns the distinct days in order.
func days(ds []time.Time) []time.Time {
	out := slices.Clone(ds)
	slices.SortFunc(out, func(a, b time.Time) int { return a.Compare(b) })
	return slices.CompactFunc(out, func(a, b time.Time) bool { return a.Equal(b) })
}

// join merges intervals that meet and agree on instrument and validity.
func join(ivs []Interval) []Interval {
	var out []Interval
	for _, iv := range ivs {
		if n := len(out); n > 0 {
			prev := &out[n-1]
			if prev.Instrument == iv.Instrument && prev.Validity == iv.Validity && prev.Before != nil && iv.From != nil && prev.Before.Equal(*iv.From) {
				prev.Before = iv.Before
				continue
			}
		}
		out = append(out, iv)
	}
	return out
}

func next(d time.Time) time.Time { return d.AddDate(0, 0, 1) }
