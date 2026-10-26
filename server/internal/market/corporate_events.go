package market

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// CorporateEventRequest asks for the corporate events of one ticker over one
// period. Ticker is a MIC_TICKER at an operating MIC.
type CorporateEventRequest struct {
	Ticker types.Identifier
	Period types.Period
}

// CorporateEvent is a corporate event a provider reported for the ticker of a
// request.
type CorporateEvent struct {
	ExDate time.Time
	Ratio  types.Ratio
	// Type is the provider's own word for the event, empty where it states
	// none.
	Type string
	// Handled is whether the system restates a quantity by the event.
	Handled bool
}

// CorporateEventResult is what was returned for one request.
type CorporateEventResult struct {
	Events []CorporateEvent
	// Coverage is the period over which the provider reported every corporate
	// event of the ticker, nil where it reported none.
	Coverage *types.Period
}

// CorporateEvents is an integration that reports the corporate events of
// tickers. An integration answers for the market of the ticker, across every
// operating MIC of that market. It decides which events are handled, since
// only it knows the provider's types, and states each ratio as units before
// and after.
type CorporateEvents interface {
	Integration
	CorporateEvents() Server[CorporateEventRequest, CorporateEventResult]
}

// corporateEventsKind is the corporate events of tickers. No stated key asks
// for one. The cache key includes the period.
var corporateEventsKind = Kind[CorporateEventRequest, CorporateEventResult]{
	name: gen.FetchKindCorporateEvents,
	server: func(e *Entry) Server[CorporateEventRequest, CorporateEventResult] {
		if s, ok := e.Integration.(CorporateEvents); ok {
			return s.CorporateEvents()
		}
		return nil
	},
	statedKey: func(CorporateEventRequest) *uuid.UUID { return nil },
	count:     func(r CorporateEventResult) int { return len(r.Events) },
	params: func(r CorporateEventRequest) []string {
		return []string{r.Period.From.Format(time.DateOnly), r.Period.Before.Format(time.DateOnly)}
	},
}

// CorporateEvents fetches the corporate events of reqs from e, as a child run
// of parent.
func (f *Fetcher) CorporateEvents(ctx context.Context, parent gen.Run, e *Entry, reqs []CorporateEventRequest) ([]FetchKey[CorporateEventRequest, CorporateEventResult], error) {
	return fetch(ctx, f, parent, e, corporateEventsKind, reqs)
}
