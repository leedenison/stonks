// Package tickervalidity derives the ticker validity of a MIC_TICKER
// identifier: the days over which it names one instrument. It derives ticker
// validity from identifier assertions and assertion coverage, and stores it in
// ticker_validity; see
// [010_ticker_validity.sql](../../db/migrations/010_ticker_validity.sql). A
// ticker's facts are read and written at its market, so a ticker stated at any
// venue of a market shares one ticker validity, and a listing that moves
// between those venues keeps its history. Only a rebuild derives ticker
// validity. Every reader reads the stored rows.
//
// Each write of facts rebuilds the ticker validity of the tickers it wrote,
// and the names that ticker validity implies, before it commits; see Rebuild.
// Assertion coverage of a whole market rebuilds every ticker of the market.
// The names are stored in resolved_tickers; see Resolve. A reorder of the
// datasources can change the ticker validity of any ticker. The change waits
// for a rebuild of every ticker.
//
// A rebuild must see every fact of its tickers, so each write of facts takes
// its locks in one order. It creates its ticker rows before its transaction;
// see tickers.Get. In the transaction it takes the advisory locks of its
// identifiers, then locks its ticker rows in id order, writes the facts, and
// rebuilds. The rebuild locks each instrument it rewrites, in id order. A
// second writer of one of the tickers, or of another ticker of one of the
// instruments, waits until the first commits. Under Read Committed its next
// statement then reads the first writer's facts.
//
// An edge cuts the timeline, and every range stops at a cut.
//
// A stronger source overrules an edge by a bracket or by a period; see
// overruled. An overruled edge does not cut, and its assertion still
// competes for its days. An administrator overrules a wrong edge by
// attesting one instrument on the days around it.
//
// A later answer replaces an earlier answer of its source within its reach;
// see current.
//
// On each day one assertion wins; see winners. Ticker validity is confirmed on
// each day the winning assertion names the instrument. Two consecutive
// winning assertions of one instrument that a cut does not separate confirm
// every day between them, on the assumption that the ticker never moved away
// from the instrument and back.
//
// A confirmed range extends through days of assertion coverage, before and
// after it, up to the nearest cut or the next assertion of anything else.
// Beyond that, ticker validity extends provisionally forward to the same limit,
// or without bound. It extends back provisionally only from the first assertion
// after a cut, or from the first assertion of all.
//
// Two consecutive winning assertions that name different things imply an
// inferred change. It took effect at the start of a day after the earlier
// assertion, and no later than the later one. Where the earlier assertion names
// an instrument, that instrument keeps the days between: confirmed where a
// period of assertion coverage holds them, and provisional elsewhere. The days
// between therefore name one instrument before and after the later assertion
// arrives.
package tickervalidity

import (
	"bytes"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Validity is the database's validity enum; see
// [010_ticker_validity.sql](../../db/migrations/010_ticker_validity.sql).
type Validity = gen.Validity

// The validities.
const (
	Confirmed   = gen.ValidityConfirmed
	Provisional = gen.ValidityProvisional
)

// Interval is a range of days over which a ticker names one instrument. A nil
// bound is open.
type Interval struct {
	From, Before *time.Time
	Instr        uuid.UUID
	Validity     Validity
}

// Contains reports whether i holds the day d.
func (i Interval) Contains(d time.Time) bool {
	return (i.From == nil || !d.Before(*i.From)) && (i.Before == nil || d.Before(*i.Before))
}

// Clip returns the days of i that p holds. It reports false when the clip
// is empty.
func (i Interval) Clip(p types.Period) (types.Period, bool) {
	out := p
	if i.From != nil && i.From.After(out.From) {
		out.From = *i.From
	}
	if i.Before != nil && i.Before.Before(out.Before) {
		out.Before = *i.Before
	}
	return out, out.From.Before(out.Before)
}

// Source is where an assertion came from: a datasource's answer with the
// datasource's precedence, or an administrator's attestation.
type Source struct {
	Datasource string
	// Precedence orders datasources; the lower number wins.
	Precedence int32
	// Answer is the id of the fetch key that made the assertion, or of the
	// run of an identifier attestation.
	Answer uuid.UUID
	// Answered is when the fetch key was answered, or when the attest
	// identifier run was created. A cached answer keeps the moment of the call
	// that filled the cache.
	Answered time.Time
	// Asked is the period of the request. It is zero where the request has
	// none.
	Asked    types.Period
	Attested bool
}

// group returns the source that s belongs to when sources are ranked: its
// datasource with its precedence, or every identifier attestation as one.
func (s Source) group() Source {
	if s.Attested {
		return Source{Attested: true}
	}
	return Source{Datasource: s.Datasource, Precedence: s.Precedence}
}

// newer reports whether the answer of s is later than the answer of o: by
// moment, then by id.
func (s Source) newer(o Source) bool {
	if !s.Answered.Equal(o.Answered) {
		return s.Answered.After(o.Answered)
	}
	return bytes.Compare(s.Answer[:], o.Answer[:]) > 0
}

// outranks reports whether s wins over o where they disagree on one day, and
// whether s may overrule an edge of o. An identifier attestation outranks a
// datasource, and a datasource outranks one with a higher precedence number.
func (s Source) outranks(o Source) bool {
	if s.Attested != o.Attested {
		return s.Attested
	}
	return s.Precedence < o.Precedence
}

// Stated is what a source stated beside a ticker: the operating MIC of the
// venue and the currency of the line. Each is empty where the source stated
// none. Derive ignores both.
type Stated struct {
	Venue, Currency string
}

// Assertion is one assertion of a ticker: what it names on every day of
// Period. A nil Instr asserts that the ticker names nothing. EdgeFrom and
// EdgeBefore state a change at the start of Period.From and at Period.Before.
type Assertion struct {
	Period               types.Period
	Instr                *uuid.UUID
	EdgeFrom, EdgeBefore bool
	Source               Source
	Stated               Stated
}

// Edge is a change in what the ticker names, which an assertion states at the
// start of the day At.
type Edge struct {
	At        time.Time
	Assertion Assertion
}

// AssertionCoverage is one period of assertion coverage: the days over which a
// datasource reported every edge of the ticker or of its market.
type AssertionCoverage struct {
	Period types.Period
	Source Source
}

// Facts are the rows of one ticker.
type Facts struct {
	Assertions        []Assertion
	AssertionCoverage []AssertionCoverage
}

// Derived is what the facts of one ticker imply: its intervals, the changes
// two assertions infer, and the conflicts among the facts.
type Derived struct {
	Diagnosis
	Intervals Intervals
	Inferred  []Inferred
}

// Diagnosis is the conflicts among the facts of one ticker that findings
// report.
type Diagnosis struct {
	Disagreements []Disagreement
	Broken        []BrokenBracket
	Missed        []MissedEvent
	Overruled     []OverruledEdge
}

// Disagreement is two datasources' assertions that name different things over
// the days of Period. Winner is the one precedence kept. A day an identifier
// attestation wins holds none, since an administrator checked it.
type Disagreement struct {
	Period        types.Period
	Winner, Loser Assertion
}

// BrokenBracket is an edge that cuts between two winning assertions of one
// instrument, First before the cut and Last on or after it. Only an equal or
// weaker source asserted the bracket, so the edge cuts it.
type BrokenBracket struct {
	Edge        Edge
	Instr       uuid.UUID
	First, Last time.Time
}

// OverruledEdge is an edge that a stronger source overrules, so it does not
// cut. By holds the source of each fact that overrules it: the two
// assertions of a bracket, or the assertion coverage that holds the cut.
type OverruledEdge struct {
	Edge Edge
	By   []Source
}

// Inferred is a change that two winning assertions, Earlier and Later,
// imply. It took effect at the start of a day in Period. A nil From or To is
// the ticker naming nothing.
type Inferred struct {
	Period         types.Period
	From, To       *uuid.UUID
	Earlier, Later Assertion
}

// MissedEvent is an inferred change whose possible days the assertion coverage
// holds in full. The datasource that made the period of assertion coverage did
// not report an edge on those days, and two assertions imply a change. By is
// the assertion coverage that holds the first of the days.
type MissedEvent struct {
	Inferred Inferred
	By       AssertionCoverage
}
