// Package tickervalidity derives the ticker validity of a MIC_TICKER
// identifier: the days over which it names one instrument. It derives ticker
// validity from identifier assertions, identifier events and their coverage
// each time it is read; see
// [010_ticker_validity.sql](../../db/migrations/010_ticker_validity.sql). A
// ticker's facts are read and written at its market, so a ticker stated at any
// venue of a market shares one ticker validity, and a listing that moves
// between those venues keeps its history. Ticker validity is never stored, so
// a change to its inputs, such as a reorder of the datasources, takes effect
// at the next read.
// An event takes effect at the start of its day, so it precedes an assertion
// on the same day.
//
// On each day one assertion wins; see winners. Ticker validity is confirmed on
// each day the winning assertion names the instrument. Two consecutive
// winning assertions of one instrument that an event does not separate
// confirm every day between them, on the assumption that the ticker never
// moved away from the instrument and back.
//
// A confirmed range extends through covered days, before and after it, up to
// the nearest event or the next assertion of anything else. Beyond that,
// ticker validity extends provisionally forward to the same limit, or without
// bound. It extends back provisionally only from the first assertion after an
// event, or from the first assertion of all.
//
// Two consecutive winning assertions that name different things imply an
// inferred event. It took effect at the start of a day after the earlier
// assertion, and no later than the later one. Where the earlier assertion
// names an instrument, that instrument keeps the days between: confirmed
// where they are covered, and provisional elsewhere. The days between
// therefore name one instrument before and after the later assertion arrives.
package tickervalidity

import (
	"time"

	"github.com/google/uuid"

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
	Instrument   uuid.UUID
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

// outranks reports whether s wins over o where they disagree on one day.
func (s Source) outranks(o Source) bool {
	if s.Attested != o.Attested {
		return s.Attested
	}
	return s.Precedence < o.Precedence
}

// Assertion is one assertion of a ticker. A nil Instrument asserts that the
// ticker names nothing.
type Assertion struct {
	At         time.Time
	Instrument *uuid.UUID
	Source     Source
}

// Event is a witnessed event of a ticker, as the ticker or as the ticker a
// rename moved to. It takes effect at the start of On.
type Event struct {
	On       time.Time
	FetchKey uuid.UUID
}

// Coverage is one period over which a datasource reported every event of
// the ticker or of its market.
type Coverage struct {
	Period     types.Period
	Datasource string
	FetchKey   uuid.UUID
}

// Facts are the rows of one ticker.
type Facts struct {
	Assertions []Assertion
	Events     []Event
	Coverage   []Coverage
}

// Derived is what the facts of one ticker imply: its intervals, the events
// two assertions infer, and the conflicts among the facts.
type Derived struct {
	Intervals     []Interval
	Inferred      []Inferred
	Disagreements []Disagreement
	Broken        []BrokenBracket
	Missed        []MissedEvent
}

// Disagreement is two datasources' assertions on one day that name different
// things. Winner is the one precedence kept. A day an identifier attestation
// wins holds none, since an administrator checked it.
type Disagreement struct {
	On            time.Time
	Winner, Loser Assertion
}

// BrokenBracket is a witnessed event that falls between two winning
// assertions of one instrument, First before the event and Last on or after
// it. The event wins over the bracket.
type BrokenBracket struct {
	Event       Event
	Instrument  uuid.UUID
	First, Last time.Time
}

// Inferred is an event that two winning assertions, Earlier and Later,
// imply. It took effect at the start of a day in Period. A nil From or To is
// the ticker naming nothing.
type Inferred struct {
	Period         types.Period
	From, To       *uuid.UUID
	Earlier, Later Assertion
}

// MissedEvent is an inferred event whose possible days one covered period
// holds in full. The datasource that reported the coverage reported no event
// on those days, and two assertions imply one. Coverage is the row that holds
// the first of the days.
type MissedEvent struct {
	Inferred Inferred
	Coverage Coverage
}
