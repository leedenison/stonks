package market

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// IdentifierEventRequest asks for the identifier events of one subject over
// one period. A domain subject asks for every value of the domain. An
// identifier subject over one day is a point in time lookup.
type IdentifierEventRequest struct {
	Subject types.Identifier
	Period  types.Period
}

// IdentifierEvent is an event a provider reported for a MIC_TICKER value.
// ToValue is set only for a rename.
type IdentifierEvent struct {
	Value   types.Identifier
	Action  gen.IdentifierEventAction
	ToValue string
	On      time.Time
}

// Assertion is what a provider stated a value named on one day: the
// identifiers of the entity, which the recorder looks up to find the
// instrument. A negative assertion states that the value names nothing.
type Assertion struct {
	Value    types.Identifier
	Of       []types.Identifier
	Negative bool
	At       time.Time
}

// Coverage is a period over which a provider reported every event of a
// value, or of a domain where Subject is a domain.
type Coverage struct {
	Subject types.Identifier
	Period  types.Period
}

// IdentifierEventResult is what was returned for one request.
type IdentifierEventResult struct {
	Events     []IdentifierEvent
	Assertions []Assertion
	Coverage   []Coverage
}

// IdentifierEvents reports the identifier events of values or domains.
type IdentifierEvents interface {
	Integration
	Server[IdentifierEventRequest, IdentifierEventResult]
}

// Ranged is an IdentifierEvents server that answers for whole domains over a
// period.
type Ranged interface {
	// Domains is the operating MICs it covers.
	Domains() []string
	// Since is the first day the endpoint holds data for.
	Since() time.Time
	// Window is the earliest day the subscription allows on the day now.
	Window(now time.Time) time.Time
	// Lag is how long the provider may take to publish an event, unless the
	// datasource sets its own.
	Lag() time.Duration
}

// identifierEventsKind is the identifier events of values and domains. No
// stated key asks for one, and the period is part of the request as cached.
var identifierEventsKind = Kind[IdentifierEventRequest, IdentifierEventResult]{
	name: gen.FetchKindIdentifierEvents,
	server: func(e *Entry) Server[IdentifierEventRequest, IdentifierEventResult] {
		s, _ := e.Integration.(IdentifierEvents)
		return s
	},
	subject: func(IdentifierEventRequest) *uuid.UUID { return nil },
	count:   func(r IdentifierEventResult) int { return len(r.Events) + len(r.Assertions) },
	params: func(r IdentifierEventRequest) []string {
		return []string{r.Period.From.Format(time.DateOnly), r.Period.Before.Format(time.DateOnly)}
	},
}

// IdentifierEvents fetches the identifier events of reqs from e, as a child
// run of parent.
func (f *Fetcher) IdentifierEvents(ctx context.Context, parent gen.Run, e *Entry, reqs []IdentifierEventRequest) ([]FetchKey[IdentifierEventRequest, IdentifierEventResult], error) {
	return fetch(ctx, f, parent, e, identifierEventsKind, reqs)
}

// NextPeriod returns the period of the next fetch of a domain. It ends with
// today. Its start is the latest of three days. The first is the end of the
// last fetch less the lag, or the earliest transaction when there was no last
// fetch. The second is the first day the endpoint holds data for. The third is
// the earliest day the subscription allows. The period is empty when that day
// is after today.
func NextPeriod(last *time.Time, earliest, since, window time.Time, lag time.Duration, now time.Time) types.Period {
	from := earliest
	if last != nil {
		from = last.Add(-lag)
	}
	for _, d := range []time.Time{since, window} {
		if d.After(from) {
			from = d
		}
	}
	before := types.Day(now).AddDate(0, 0, 1)
	from = types.Day(from)
	if from.After(before) {
		from = before
	}
	return types.Period{From: from, Before: before}
}
