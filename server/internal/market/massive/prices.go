package massive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

var _ market.Prices = (*Client)(nil)

// until is the last day every bars request names. It lies far past any day
// Massive holds.
var until = time.Date(2099, time.December, 31, 0, 0, 0, 0, time.UTC)

// barLimit is the most daily bars Massive returns on one page, which is more
// than its history holds for one ticker.
const barLimit = "50000"

// bars is Massive's side of the prices kind.
type bars struct{ c *Client }

// bar is one day as Massive states it.
type bar struct {
	// T is the start of the day in Unix milliseconds. A stock day starts at
	// midnight in New York and a currency day at midnight UTC, so the UTC
	// date is the day of the bar for both.
	T int64 `json:"t"`
	// C is the close: of the regular session for a stock, and of the UTC day
	// for a currency.
	C json.Number `json:"c"`
}

// Prices is Massive's server of end of day prices and rates.
func (c *Client) Prices() market.Server[market.PriceRequest, market.PriceResult] {
	return bars{c: c}
}

// Serves returns the identifier to send for r, or an error when Massive
// cannot answer r. A ticker goes out without its venue, and a currency goes
// out as it is. It refuses a period that ends before the window of the
// account's plan for the product, so such a request costs no call.
func (b bars) Serves(r market.PriceRequest) (types.Identifier, error) {
	id := r.Identifier
	switch {
	case id.Type == types.IdentifierTypeCurrency && id.Value == "USD":
		return types.Identifier{}, errors.New("currency 'USD' rejected: every rate is against USD")
	case id.Type != types.IdentifierTypeCurrency && id.Type != types.IdentifierTypeMicTicker:
		return types.Identifier{}, fmt.Errorf("identifier type '%s' rejected", id.Type)
	}
	if w := b.window(id); !r.Period.Before.After(w) {
		return types.Identifier{}, fmt.Errorf("period before %s rejected: the plan's bars start on %s", r.Period.Before.Format(time.DateOnly), w.Format(time.DateOnly))
	}
	if id.Type == types.IdentifierTypeCurrency {
		return id, nil
	}
	return b.c.sent(id)
}

// window is the first day the account's plan serves a bar of id for, as of
// now.
func (b bars) window(id types.Identifier) time.Time {
	if id.Type == types.IdentifierTypeCurrency {
		return b.c.currencies.window(currenciesSince, b.c.now())
	}
	return b.c.stocks.window(since, b.c.now())
}

// symbol is how Massive names the bars of sent: the ticker, or the pair that
// quotes a currency in USD. Massive lists every pair in both directions, so
// the close of the pair is the rate and nothing is inverted.
func symbol(sent types.Identifier) string {
	if sent.Type == types.IdentifierTypeCurrency {
		return "C:" + sent.Value + "USD"
	}
	return sent.Value
}

// Fetch asks Massive for the daily bars of each ticker or currency in turn.
// Every request names the same days, from since to until, and Massive returns
// the bars the plan serves. A request therefore does not depend on the asked
// period, and a recording of it replays on any later day. Massive does not
// document that it serves part of a range in this way. The bars are as
// traded. Massive adjusts a bar for splits alone, so an as traded bar is free
// of any dividend adjustment.
func (b bars) Fetch(ctx context.Context, reqs []market.Request[market.PriceRequest]) ([]market.Response[market.PriceResult], error) {
	out := make([]market.Response[market.PriceResult], len(reqs))
	for i, r := range reqs {
		q := url.Values{"apiKey": {b.c.key}, "adjusted": {"false"}, "sort": {"asc"}, "limit": {barLimit}}
		path := "/v2/aggs/ticker/" + url.PathEscape(symbol(r.Sent)) + "/range/1/day/" + since.Format(time.DateOnly) + "/" + until.Format(time.DateOnly)
		var page struct {
			Results []bar  `json:"results"`
			NextURL string `json:"next_url"`
		}
		if err := b.c.get(ctx, path, q, &page); err != nil {
			return nil, err
		}
		if page.NextURL != "" {
			out[i].Err = errPaged
			continue
		}
		res, err := fromBars(page.Results, r.Value.Period, b.window(r.Sent))
		if err != nil {
			return nil, err
		}
		out[i].Value = res
	}
	return out, nil
}

// fromBars converts the whole history bs of one symbol into the prices of the
// asked period. Coverage is the asked period from window to the day of the
// last bar. Massive answers an unknown ticker or pair as one that has not
// traded, so an answer without a bar is uncovered.
func fromBars(bs []bar, asked types.Period, window time.Time) (market.PriceResult, error) {
	prices := make([]market.DayPrice, len(bs))
	var last time.Time
	for i, b := range bs {
		day := types.Day(time.UnixMilli(b.T).UTC())
		price, err := decimal.NewFromString(b.C.String())
		if err != nil {
			return market.PriceResult{}, fmt.Errorf("bar close: %w", err)
		}
		prices[i] = market.DayPrice{Day: day, Price: price, AsAt: day}
		if day.After(last) {
			last = day
		}
	}
	if len(bs) == 0 {
		return market.PriceResult{}, nil
	}
	coverage, ok := asked.Clip(types.Period{From: window, Before: last.AddDate(0, 0, 1)})
	if !ok {
		return market.PriceResult{}, nil
	}
	var kept []market.DayPrice
	for _, p := range prices {
		if coverage.Contains(p.Day) {
			kept = append(kept, p)
		}
	}
	return market.PriceResult{Prices: kept, Currency: "USD", Coverage: &coverage}, nil
}
