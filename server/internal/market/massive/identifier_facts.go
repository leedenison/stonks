package massive

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/leedenison/stonks/server/internal/db/gen"
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
// identifier facts at the market home. An oldest event on or before p's
// first day marks where Massive's data begins, so it assigns nothing. The
// newest ticker is also asserted on p's last day, so the chain asserts a held
// ticker on the present day. That holds because Massive answers not found
// for the chain of a delisted entity. If a chain holds a ticker the system
// cannot spell, chainFacts returns an empty result, since a partial chain
// would claim coverage around the event it dropped.
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
	var out market.IdentifierFactResult
	today := p.Before.AddDate(0, 0, -1)
	for i, l := range links {
		before := p.Before
		if i+1 < len(links) {
			before = links[i+1].from
		}
		t := types.MarketTicker{Market: home, Value: l.ticker}
		switch {
		case i > 0:
			prev := types.MarketTicker{Market: home, Value: links[i-1].ticker}
			out.Events = append(out.Events, market.IdentifierEvent{Ticker: prev, Action: gen.IdentifierEventActionRenamed, ToValue: l.ticker, On: l.from})
		case l.from.After(p.From):
			out.Events = append(out.Events, market.IdentifierEvent{Ticker: t, Action: gen.IdentifierEventActionAssigned, On: l.from})
		}
		out.Assertions = append(out.Assertions, market.Assertion{Ticker: t, Of: market.Listing{Identifiers: []types.Identifier{sent}}, At: l.from})
		if i == len(links)-1 && today.After(l.from) {
			out.Assertions = append(out.Assertions, market.Assertion{Ticker: t, Of: market.Listing{Identifiers: []types.Identifier{sent}}, At: today})
		}
		if c, ok := clip(types.Period{From: l.from, Before: before}, p); ok {
			out.Coverage = append(out.Coverage, market.Coverage{Market: home, Ticker: &t, Period: c})
		}
	}
	return out, nil
}

// clip returns the overlap of a and b, and false when they do not overlap.
func clip(a, b types.Period) (types.Period, bool) {
	out := a
	if b.From.After(out.From) {
		out.From = b.From
	}
	if b.Before.Before(out.Before) {
		out.Before = b.Before
	}
	return out, out.From.Before(out.Before)
}

// lookupFacts converts the record of ticker on day into an assertion of the
// entity it names, at the market home. A record the integration cannot
// convert, such as a warrant or an OTC listing, yields an empty result.
func lookupFacts(ticker string, rec *record, unknown bool, day time.Time, home string, mics mic.MICMap) market.IdentifierFactResult {
	a := market.Assertion{Ticker: types.MarketTicker{Market: home, Value: ticker}, At: day, Negative: unknown}
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
