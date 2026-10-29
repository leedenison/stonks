package massive

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	_ market.IdentifierFacts = (*Client)(nil)
	_ market.Windowed        = (*Client)(nil)
)

// Facts is Massive's server of identifier facts.
func (c *Client) Facts() market.Server[market.IdentifierFactRequest, market.IdentifierFactResult] {
	return facts{c: c}
}

// Fetch states each identifier fact once, at the market of Massive's venues.
func (e facts) Fetch(ctx context.Context, reqs []market.Request[market.IdentifierFactRequest]) ([]market.Response[market.IdentifierFactResult], error) {
	home := e.c.home
	out := make([]market.Response[market.IdentifierFactResult], len(reqs))
	for i, r := range reqs {
		var err error
		switch p := r.Value.Period; callOf(r.Value) {
		case chainCall:
			var evs []event
			if evs, err = e.c.chain(ctx, r.Sent); err == nil {
				out[i].Value, err = chainFacts(r.Sent, evs, p, home)
			}
		case lookupCall:
			var rec *record
			var unknown bool
			if rec, unknown, err = e.c.ticker(ctx, r.Sent.Value, p.From); err == nil {
				out[i].Value = lookupFacts(r.Sent.Value, rec, unknown, p.From, home, e.c.mics)
			}
		}
		if err != nil {
			return nil, err
		}
	}
	return out, nil
}

// link is one ticker of an entity's chain and the day it began naming the
// entity.
type link struct {
	ticker string
	from   time.Time
}

// chainFacts converts the ticker events of the entity sent names into
// identifier facts at the market home. Each ticker is asserted over the days
// it named the entity, with an edge at each rename. The oldest event on or
// before p's first day marks where Massive's data begins, so its assertion has
// no edge at its start. The newest ticker is asserted up to the end of p, so
// the chain asserts a held ticker on the present day. That holds because
// Massive answers not found for the chain of a delisted entity. If a chain
// holds a ticker the system cannot spell, chainFacts returns an empty result,
// since a partial chain would put a period of assertion coverage around the
// edge it dropped.
func chainFacts(sent types.Identifier, evs []event, p types.Period, home string) (market.IdentifierFactResult, error) {
	var links []link
	for _, e := range slices.Backward(evs) {
		if e.Type != "ticker_change" {
			continue
		}
		on, err := time.Parse(time.DateOnly, e.Date)
		if err != nil {
			return market.IdentifierFactResult{}, fmt.Errorf("massive: event date: %w", err)
		}
		t, ok := market.WithClassSep(e.TickerChange.Ticker, '.')
		if !ok || preferred(t) {
			return market.IdentifierFactResult{}, nil
		}
		links = append(links, link{ticker: t, from: on})
	}
	of := market.Listing{Identifiers: []types.Identifier{sent}}
	var out market.IdentifierFactResult
	for i, l := range links {
		before := p.Before
		if i+1 < len(links) {
			before = links[i+1].from
		}
		t := types.MarketTicker{Market: home, Value: l.ticker}
		// Skip a ticker that a rename replaced on its first day: it named
		// the entity on no day.
		if l.from.Before(before) {
			out.Assertions = append(out.Assertions, market.Assertion{
				Ticker: t, Of: of, Period: types.Period{From: l.from, Before: before},
				EdgeFrom: i > 0 || l.from.After(p.From), EdgeBefore: i+1 < len(links),
			})
		}
		if c, ok := (types.Period{From: l.from, Before: before}).Clip(p); ok {
			out.AssertionCoverage = append(out.AssertionCoverage, market.AssertionCoverage{Ticker: t, Period: c})
		}
	}
	return out, nil
}

// lookupFacts converts the record of ticker on day into an assertion of the
// entity it names, at the market home. A record the integration cannot
// convert, such as a warrant or an OTC listing, yields an empty result.
func lookupFacts(ticker string, rec *record, unknown bool, day time.Time, home string, mics mic.MICMap) market.IdentifierFactResult {
	a := market.Assertion{Ticker: types.MarketTicker{Market: home, Value: ticker}, Period: types.OneDay(day), Negative: unknown}
	if !unknown {
		if rec == nil {
			return market.IdentifierFactResult{}
		}
		l, ok := toListing(*rec, mics)
		if !ok || len(l.Identifiers) == 0 {
			return market.IdentifierFactResult{}
		}
		a.Of = l
	}
	return market.IdentifierFactResult{Assertions: []market.Assertion{a}}
}
