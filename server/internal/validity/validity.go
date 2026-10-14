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

// Source is where an assertion came from: a datasource with its precedence,
// or an administrator.
type Source struct {
	Datasource string
	// Precedence orders datasources; the lower number wins.
	Precedence int32
	Attested   bool
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

// Facts are the rows of one value.
type Facts struct {
	Assertions []Assertion
	// Events are the days on which an event of the value took effect, as the
	// value or as the value a rename moved to.
	Events []time.Time
	// Coverage is the periods covered for the value or its domain.
	Coverage []types.Period
}

// Inferred is an event that two assertions imply. It took effect at the
// start of a day in Period. A nil From or To is the value naming nothing.
type Inferred struct {
	Period   types.Period
	From, To *uuid.UUID
}
