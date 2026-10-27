// Package tickervalidity derives the ticker validity of a MIC_TICKER
// identifier: the days over which it names one instrument. It derives ticker
// validity from identifier assertions and period assertions each time it is
// read; see
// [010_ticker_validity.sql](../../db/migrations/010_ticker_validity.sql). A
// ticker's facts are read and written at its market, so a ticker stated at any
// venue of a market shares one ticker validity, and a listing that moves
// between those venues keeps its history. Ticker validity is never stored, so
// a change to its inputs, such as a reorder of the datasources, takes effect
// at the next read.
//
// An edge cuts the timeline. A first cuts at the start of its day, and a last
// at the start of the next day. Every range stops at a cut. An assertion
// with an edge also competes for its day as a point does.
//
// A stronger source overrules an edge by a bracket or by a period; see
// overruled. An overruled edge does not cut, and its assertion still
// competes for its day as a point. An administrator overrules a wrong edge by
// attesting one instrument on the days around it.
//
// On each day one assertion wins; see winners. Ticker validity is confirmed on
// each day the winning assertion names the instrument. Two consecutive
// winning assertions of one instrument that a cut does not separate confirm
// every day between them, on the assumption that the ticker never moved away
// from the instrument and back.
//
// A confirmed range extends through period-asserted days, before and after
// it, up to the nearest cut or the next assertion of anything else. Beyond
// that, ticker validity extends provisionally forward to the same limit, or
// without bound. It extends back provisionally only from the first assertion
// after a cut, or from the first assertion of all.
//
// Two consecutive winning assertions that name different things imply an
// inferred change. It took effect at the start of a day after the earlier
// assertion, and no later than the later one. Where the earlier assertion
// names an instrument, that instrument keeps the days between: confirmed
// where a period assertion holds them, and provisional elsewhere. The days
// between therefore name one instrument before and after the later assertion
// arrives.
package tickervalidity

import (
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Validity is how firmly a ticker's facts establish what it names over a
// range of days.
type Validity string

// The validities: confirmed where the facts name the instrument, and
// provisional where ticker validity extends on an assumption.
const (
	Confirmed   Validity = "confirmed"
	Provisional Validity = "provisional"
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

// Source is where an assertion came from: a datasource's fetch key with the
// datasource's precedence, or an administrator.
type Source struct {
	Datasource string
	// Precedence orders datasources; the lower number wins.
	Precedence int32
	// FetchKey made the assertion. It is zero for an identifier attestation.
	FetchKey uuid.UUID
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

// outranks reports whether s wins over o where they disagree on one day, and
// whether s may overrule an edge of o. An identifier attestation outranks a
// datasource, and a datasource outranks one with a higher precedence number.
func (s Source) outranks(o Source) bool {
	if s.Attested != o.Attested {
		return s.Attested
	}
	return s.Precedence < o.Precedence
}

// Assertion is one assertion of a ticker. A nil Instr asserts that the
// ticker names nothing. Edge is empty for a point.
type Assertion struct {
	At     time.Time
	Instr  *uuid.UUID
	Edge   gen.IdentifierEdge
	Source Source
}

// PeriodAssertion is one period over which a datasource reported every edge
// of the ticker or of its market.
type PeriodAssertion struct {
	Period types.Period
	Source Source
}

// Facts are the rows of one ticker.
type Facts struct {
	Assertions       []Assertion
	PeriodAssertions []PeriodAssertion
}

// Derived is what the facts of one ticker imply: its intervals, the changes
// two assertions infer, and the conflicts among the facts.
type Derived struct {
	Intervals     []Interval
	Inferred      []Inferred
	Disagreements []Disagreement
	Broken        []BrokenBracket
	Missed        []MissedEvent
	Overruled     []OverruledEdge
}

// Disagreement is two datasources' assertions on one day that name different
// things. Winner is the one precedence kept. A day an identifier attestation
// wins holds none, since an administrator checked it.
type Disagreement struct {
	On            time.Time
	Winner, Loser Assertion
}

// BrokenBracket is an edge that cuts between two winning assertions of one
// instrument, First before the cut and Last on or after it. Only an equal or
// weaker source asserted the bracket, so the edge cuts it.
type BrokenBracket struct {
	Edge        Assertion
	Instr       uuid.UUID
	First, Last time.Time
}

// OverruledEdge is an edge that a stronger source overrules, so it does not
// cut. By holds the source of each fact that overrules it: the two
// assertions of a bracket, or the period assertion that holds the cut.
type OverruledEdge struct {
	Edge Assertion
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

// MissedEvent is an inferred change whose possible days the period
// assertions hold in full. The datasource that made the period assertion did
// not report an edge on those days, and two assertions imply a change. By is
// the period assertion that holds the first of the days.
type MissedEvent struct {
	Inferred Inferred
	By       PeriodAssertion
}
