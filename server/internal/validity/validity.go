// Package validity derives the validity of a MIC_TICKER value: the days over
// which it names one instrument. It derives validity from identifier
// assertions, identifier events and their coverage each time it is read; see
// [010_validity.sql](../migrations/010_validity.sql). An event takes effect
// at the start of its day, so it precedes an assertion on the same day.
//
// On each day one assertion wins; see winners. Validity is confirmed on each
// day the winning assertion names the instrument. Two consecutive winning
// assertions of one instrument that an event does not separate confirm every
// day between them, on the assumption that the value never moved away from
// the instrument and back.
//
// A confirmed range extends through covered days, before and after it, up to
// the nearest event or the next assertion of anything else. Beyond that,
// validity extends provisionally forward to the same limit, or without bound.
// It extends back provisionally only from the first assertion after an event,
// or from the first assertion of all.
//
// Two consecutive winning assertions that name different things imply an
// inferred event. It took effect at the start of a day after the earlier
// assertion, and no later than the later one. Where the earlier assertion
// names an instrument, that instrument keeps the days between: confirmed
// where they are covered, and provisional elsewhere.
package validity

import (
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Interval is a range of days over which a value names one instrument. A nil
// bound is open.
type Interval struct {
	From, Before *time.Time
	Instrument   uuid.UUID
	Validity     gen.Validity
}

// Source is where an assertion came from: a datasource's fetch key with the
// datasource's precedence, or an administrator.
type Source struct {
	Datasource string
	// Precedence orders datasources; the lower number wins.
	Precedence int32
	// FetchKey made the assertion. It is zero for an attestation.
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

// Assertion is one assertion of a value. A nil Instrument asserts that the
// value names nothing.
type Assertion struct {
	At         time.Time
	Instrument *uuid.UUID
	Source     Source
}

// Event is a witnessed event of a value, as the value or as the value a
// rename moved to. It takes effect at the start of On.
type Event struct {
	On       time.Time
	FetchKey uuid.UUID
}

// Facts are the rows of one value.
type Facts struct {
	Assertions []Assertion
	Events     []Event
	// Coverage is the periods covered for the value or its domain.
	Coverage []types.Period
}

// Derived is what the facts of one value imply: its intervals, the events
// two assertions infer, and the conflicts among the facts.
type Derived struct {
	Intervals     []Interval
	Inferred      []Inferred
	Disagreements []Disagreement
	Broken        []BrokenBracket
}

// Disagreement is two datasources' assertions on one day that name different
// things. Winner is the one precedence kept. A day an attestation wins holds
// none, since an administrator checked it.
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

// Inferred is an event that two assertions imply. It took effect at the
// start of a day in Period. A nil From or To is the value naming nothing.
type Inferred struct {
	Period   types.Period
	From, To *uuid.UUID
}
