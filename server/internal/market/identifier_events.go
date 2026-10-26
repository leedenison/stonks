package market

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// IdentifierEventRequest asks for the identifier events of one identifier
// over one period. An identifier with no value stands for the market of its
// domain, and asks for every ticker of the market. An identifier with a value
// over one day is a point in time lookup.
type IdentifierEventRequest struct {
	Identifier types.Identifier
	Period     types.Period
}

// IdentifierEvent is an event a provider reported for a ticker at its
// market. ToValue is set only for a rename.
type IdentifierEvent struct {
	Ticker  types.MarketTicker
	Action  gen.IdentifierEventAction
	ToValue string
	On      time.Time
}

// Assertion is what a provider stated a ticker named on one day: the listing
// of the entity, as far as the provider described it. The recorder looks its
// identifiers up to find the instrument. A negative assertion states that the
// ticker names nothing, and leaves Of empty.
type Assertion struct {
	Ticker   types.MarketTicker
	Of       Listing
	Negative bool
	At       time.Time
}

// Coverage is a period over which a provider reported every event of
// Ticker, or of every ticker of Market where Ticker is nil.
type Coverage struct {
	Market string
	Ticker *types.MarketTicker
	Period types.Period
}

// IdentifierEventResult is what was returned for one request.
type IdentifierEventResult struct {
	Events     []IdentifierEvent
	Assertions []Assertion
	Coverage   []Coverage
}

// IdentifierEvents is an integration that reports the identifier events of
// tickers or markets. Its Events method returns the server, so one integration
// can serve identity too.
type IdentifierEvents interface {
	Integration
	Events() Server[IdentifierEventRequest, IdentifierEventResult]
}

// Windowed is an IdentifierEvents integration whose answers reach back a
// limited way.
type Windowed interface {
	// Window is the first day the provider answers for, as of now. It is the
	// later of the endpoint's first day and the earliest day the subscription
	// allows.
	Window(now time.Time) time.Time
}

// Ranged is a Windowed integration that answers for whole markets over a
// period.
type Ranged interface {
	Windowed
	// Markets are the markets it covers.
	Markets() []string
	// Lag is how long the provider may take to publish an event, unless the
	// datasource sets its own.
	Lag() time.Duration
}

// identifierEventsKind is the identifier events of tickers and markets. No
// stated key asks for one, and the period is part of the request as cached.
var identifierEventsKind = Kind[IdentifierEventRequest, IdentifierEventResult]{
	name: gen.FetchKindIdentifierEvents,
	server: func(e *Entry) Server[IdentifierEventRequest, IdentifierEventResult] {
		if s, ok := e.Integration.(IdentifierEvents); ok {
			return s.Events()
		}
		return nil
	},
	statedKey: func(IdentifierEventRequest) *uuid.UUID { return nil },
	count:     func(r IdentifierEventResult) int { return len(r.Events) + len(r.Assertions) },
	params: func(r IdentifierEventRequest) []string {
		return []string{r.Period.From.Format(time.DateOnly), r.Period.Before.Format(time.DateOnly)}
	},
}

// IdentifierEvents fetches the identifier events of reqs from e, as a child
// run of parent.
func (f *Fetcher) IdentifierEvents(ctx context.Context, parent gen.Run, e *Entry, reqs []IdentifierEventRequest) ([]FetchKey[IdentifierEventRequest, IdentifierEventResult], error) {
	return fetch(ctx, f, parent, e, identifierEventsKind, reqs)
}

// NextPeriod returns the period of the next fetch of a market. It ends with
// today. It starts at the later of the start of w's window and the end of the
// last fetch less the lag. Where there was no last fetch, the earliest
// transaction stands in for the end. The period is empty when its start is
// after today.
func NextPeriod(last *time.Time, earliest time.Time, w Windowed, lag time.Duration, now time.Time) types.Period {
	from := earliest
	if last != nil {
		from = last.Add(-lag)
	}
	before := tomorrow(now)
	from = types.Day(later(from, w.Window(now)))
	if from.After(before) {
		from = before
	}
	return types.Period{From: from, Before: before}
}

// LookupDay is the day a point in time lookup asks about for a ticker whose
// earliest transaction is on earliest: that day, or the start of w's window
// where the window starts later.
func LookupDay(earliest time.Time, w Windowed, now time.Time) time.Time {
	return types.Day(later(earliest, w.Window(now)))
}

// History is the period of a request for an entity's ticker history: from
// the start of w's window to today. Every such request asks for this period,
// so requests made on one day for one entity share a cached answer.
func History(w Windowed, now time.Time) types.Period {
	return types.Period{From: types.Day(w.Window(now)), Before: tomorrow(now)}
}

func later(a, b time.Time) time.Time {
	if b.After(a) {
		return b
	}
	return a
}

func tomorrow(now time.Time) time.Time { return types.Day(now).AddDate(0, 0, 1) }
