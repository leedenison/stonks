package market

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// IdentifierFactRequest asks for the identifier facts of one identifier
// over one period. An identifier with no value stands for the market of its
// domain, and asks for every ticker of the market. An identifier with a value
// over one day is a point in time lookup.
type IdentifierFactRequest struct {
	Identifier types.Identifier
	Period     types.Period
}

// Assertion is what a provider stated a ticker named on every day of Period:
// the listing of the entity, as far as the provider described it. The
// recorder looks its identifiers up to find the instrument. A negative
// assertion states that the ticker names nothing, and leaves Of empty.
// EdgeFrom and EdgeBefore state a change at the start of Period.From and at
// Period.Before.
type Assertion struct {
	Ticker               types.MarketTicker
	Of                   Listing
	Negative             bool
	Period               types.Period
	EdgeFrom, EdgeBefore bool
}

// AssertionCoverage is one period of assertion coverage: the days over which a
// provider reported every edge of Ticker, or of every ticker of Market where
// Ticker is nil.
type AssertionCoverage struct {
	Market string
	Ticker *types.MarketTicker
	Period types.Period
}

// IdentifierFactResult is what was returned for one request.
type IdentifierFactResult struct {
	Assertions        []Assertion
	AssertionCoverage []AssertionCoverage
}

// Tickers returns the tickers whose facts r states: the tickers of its
// assertions, and the ticker of each period of assertion coverage of one
// ticker.
func (r IdentifierFactResult) Tickers() []types.MarketTicker {
	var out []types.MarketTicker
	for _, a := range r.Assertions {
		out = append(out, a.Ticker)
	}
	for _, p := range r.AssertionCoverage {
		if p.Ticker != nil {
			out = append(out, *p.Ticker)
		}
	}
	return out
}

// IdentifierFacts is an integration that reports the identifier facts of
// tickers or markets. Its Facts method returns the server, so one integration
// can serve identity too. An integration reports each assertion and each period
// of assertion coverage once, for a market, across every operating MIC of that
// market. A negative assertion or a period of assertion coverage is therefore a
// claim about the whole market.
type IdentifierFacts interface {
	Integration
	Facts() Server[IdentifierFactRequest, IdentifierFactResult]
}

// Windowed is an IdentifierFacts integration whose answers reach back a
// limited way.
type Windowed interface {
	// Window is the first day the provider answers for, as of now. It is the
	// later of the endpoint's first day and the earliest day the subscription
	// allows.
	Window(now time.Time) time.Time
}

// identifierFactsKind is the identifier facts of tickers and markets. No
// stated key asks for one, and the period is part of the request as cached.
var identifierFactsKind = Kind[IdentifierFactRequest, IdentifierFactResult]{
	name: gen.FetchKindIdentifierFacts,
	server: func(e *Entry) Server[IdentifierFactRequest, IdentifierFactResult] {
		if s, ok := e.Integration.(IdentifierFacts); ok {
			return s.Facts()
		}
		return nil
	},
	statedKey: func(IdentifierFactRequest) *uuid.UUID { return nil },
	count:     func(r IdentifierFactResult) int { return len(r.Assertions) },
	params: func(r IdentifierFactRequest) []string {
		return []string{r.Period.From.Format(time.DateOnly), r.Period.Before.Format(time.DateOnly)}
	},
	asked: func(r IdentifierFactRequest) *types.Period { return &r.Period },
}

// IdentifierFacts fetches the identifier facts of reqs from e, as a child
// run of parent.
func (f *Fetcher) IdentifierFacts(ctx context.Context, parent gen.Run, e *Entry, reqs []IdentifierFactRequest) ([]FetchKey[IdentifierFactRequest, IdentifierFactResult], error) {
	return fetch(ctx, f, parent, e, identifierFactsKind, reqs)
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
