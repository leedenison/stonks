package eodhd

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

var _ market.Prices = (*Client)(nil)

// usCurrency is the trading currency of EODHD's code for the US venues.
const usCurrency = "USD"

// bars is EODHD's side of the prices kind.
type bars struct{ c *Client }

// bar is one day as EODHD states it. A bar does not state its currency.
type bar struct {
	Date string `json:"date"`
	// Close is the close as traded. EODHD states the close adjusted for
	// splits and dividends in another member, which is left unread.
	Close json.Number `json:"close"`
}

// Prices is EODHD's server of end of day prices and rates.
func (c *Client) Prices() market.Server[market.PriceRequest, market.PriceResult] {
	return bars{c: c}
}

// Serves returns the identifier to send for r, or an error when EODHD cannot
// answer r. A ticker goes out as EODHD's symbol; see Client.sent. A currency
// goes out as it is. It refuses every request where the account's plan lacks
// daily bars. It refuses a period that ends before the window of
// the plan, so such a request costs no call.
func (b bars) Serves(r market.PriceRequest) (types.Identifier, error) {
	id := r.Identifier
	switch {
	case !b.c.plan.prices:
		return types.Identifier{}, errors.New("prices rejected: the plan lacks daily bars")
	case id.Type == types.IdentifierTypeCurrency && id.Value == usCurrency:
		return types.Identifier{}, errors.New("currency 'USD' rejected: every rate is against USD")
	case id.Type != types.IdentifierTypeCurrency && id.Type != types.IdentifierTypeMicTicker:
		return types.Identifier{}, fmt.Errorf("identifier type '%s' rejected", id.Type)
	}
	if w := b.c.plan.window(b.c.now()); !r.Period.Before.After(w) {
		return types.Identifier{}, fmt.Errorf("period before %s rejected: the plan's bars start on %s", r.Period.Before.Format(time.DateOnly), w.Format(time.DateOnly))
	}
	if id.Type == types.IdentifierTypeCurrency {
		return id, nil
	}
	return b.c.sent(id)
}

// symbol is how EODHD names the bars of sent: the symbol of a ticker, or the
// pair that quotes a currency in USD. EODHD lists that pair for each currency,
// so the close of the pair is the rate and the client does not invert it.
func symbol(sent types.Identifier) string {
	if sent.Type == types.IdentifierTypeCurrency {
		return sent.Value + usCurrency + ".FOREX"
	}
	return sent.Value
}

// Fetch asks EODHD for the daily bars of each symbol or currency in turn. A
// request omits its dates, and EODHD returns every bar the plan serves. A
// request therefore does not depend on the asked period, and a recording of
// it replays on any later day. A symbol or pair EODHD does not know is
// answered with an empty answer and nil coverage.
func (b bars) Fetch(ctx context.Context, reqs []market.Request[market.PriceRequest]) ([]market.Response[market.PriceResult], error) {
	out := make([]market.Response[market.PriceResult], len(reqs))
	for i, r := range reqs {
		code, err := b.currency(ctx, r)
		if err != nil {
			return nil, err
		}
		if code == "" {
			continue
		}
		var bs []bar
		err = b.c.get(ctx, "/api/eod/"+url.PathEscape(symbol(r.Sent)), url.Values{}, &bs)
		if unknown(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		res, err := fromBars(bs, b.c.plan.window(b.c.now()), code)
		if err != nil {
			return nil, err
		}
		out[i].Value = res
	}
	return out, nil
}

// currency returns the code in which EODHD quotes the bars of r, or "" where
// EODHD does not list the symbol. A rate and a US symbol are quoted in USD. The currency
// of any other symbol comes from a search, which costs a call, because
// symbols on one exchange are quoted in different currencies. London quotes
// most in pence and some in dollars.
func (b bars) currency(ctx context.Context, r market.Request[market.PriceRequest]) (string, error) {
	if r.Sent.Type == types.IdentifierTypeCurrency || strings.HasSuffix(r.Sent.Value, "."+usCode) {
		return usCurrency, nil
	}
	rows, err := b.c.searchTicker(ctx, r.Value.Identifier)
	if err != nil || len(rows) != 1 {
		return "", err
	}
	return rows[0].Currency, nil
}

// fromBars converts the whole history bs of one symbol into its prices from
// window on, each quoted in the currency code. Coverage runs from the later
// of window and the first bar, to the day of the last bar. EODHD does not say
// where its history of a symbol starts, so the days before the first bar are
// left uncovered.
func fromBars(bs []bar, window time.Time, code string) (market.PriceResult, error) {
	if len(bs) == 0 {
		return market.PriceResult{}, nil
	}
	prices := make([]market.DayPrice, len(bs))
	first, last := time.Time{}, time.Time{}
	for i, b := range bs {
		day, err := time.Parse(time.DateOnly, b.Date)
		if err != nil {
			return market.PriceResult{}, fmt.Errorf("bar date: %w", err)
		}
		price, err := decimal.NewFromString(b.Close.String())
		if err != nil {
			return market.PriceResult{}, fmt.Errorf("bar close: %w", err)
		}
		prices[i] = market.DayPrice{Day: day, Price: price, AsAt: day}
		if i == 0 || day.Before(first) {
			first = day
		}
		if day.After(last) {
			last = day
		}
	}
	if window.After(first) {
		first = window
	}
	coverage := types.Period{From: first, Before: last.AddDate(0, 0, 1)}
	if !coverage.From.Before(coverage.Before) {
		return market.PriceResult{}, nil
	}
	var kept []market.DayPrice
	for _, p := range prices {
		if coverage.Contains(p.Day) {
			kept = append(kept, p)
		}
	}
	return market.PriceResult{Prices: kept, Currency: code, Coverage: &coverage}, nil
}
