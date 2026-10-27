package market

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// CorpActRequest asks for the corporate actions of one ticker over one
// period. Ticker is a MIC_TICKER at an operating MIC.
type CorpActRequest struct {
	Ticker types.Identifier
	Period types.Period
}

// CorpAct is a corporate action a provider reported for the ticker of a
// request.
type CorpAct struct {
	ExDate time.Time
	Ratio  types.Ratio
	// Type is the provider's own word for the action, empty where it states
	// none.
	Type string
	// Handled is whether the provider's type of action restates a quantity.
	// Package corpact then tests the ratio.
	Handled bool
}

// CorpActResult is what was returned for one request.
type CorpActResult struct {
	Acts []CorpAct
	// Coverage is the period the provider answered for. Over it, the provider
	// reported every corporate action it holds for the ticker. It is nil where
	// the provider omits a period, or states that it does not know the
	// ticker.
	Coverage *types.Period
}

// CorpActs is an integration that reports the corporate actions of
// tickers. An integration answers for the market of the ticker, across every
// operating MIC of that market. It sets Handled from the provider's types,
// since only it knows them. It states each ratio as units before and after.
type CorpActs interface {
	Integration
	CorpActs() Server[CorpActRequest, CorpActResult]
}

// corpActsKind is the corporate actions of tickers. No stated key asks
// for one. The cache key includes the period.
var corpActsKind = Kind[CorpActRequest, CorpActResult]{
	name: gen.FetchKindCorpActs,
	server: func(e *Entry) Server[CorpActRequest, CorpActResult] {
		if s, ok := e.Integration.(CorpActs); ok {
			return s.CorpActs()
		}
		return nil
	},
	statedKey: func(CorpActRequest) *uuid.UUID { return nil },
	count:     func(r CorpActResult) int { return len(r.Acts) },
	params: func(r CorpActRequest) []string {
		return []string{r.Period.From.Format(time.DateOnly), r.Period.Before.Format(time.DateOnly)}
	},
}

// CorpActs fetches the corporate actions of reqs from e, as a child run
// of parent.
func (f *Fetcher) CorpActs(ctx context.Context, parent gen.Run, e *Entry, reqs []CorpActRequest) ([]FetchKey[CorpActRequest, CorpActResult], error) {
	return fetch(ctx, f, parent, e, corpActsKind, reqs)
}
