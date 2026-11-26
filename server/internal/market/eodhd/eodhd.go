// Package eodhd is the identity, corporate actions and prices integration
// with EODHD's API, which covers listed stock, ETFs and funds on EODHD's
// exchanges worldwide. It also serves the rate of a currency against USD.
//
// An identity answer costs up to four calls against EODHD's daily quota.
// Search returns the listings an identifier names, and the other calls add
// what search leaves out: the venue of a US listing, and a CUSIP. A price
// answer costs one call for a US listing or a rate and two for any other
// listing, where a search states the currency of the price.
package eodhd

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

const endpoint = "https://eodhd.com"

var _ market.Identity = (*Client)(nil)

// Client is the EODHD integration.
type Client struct {
	mics     mic.MICMap
	exchange ExchangeCodeMap
	prefer   market.Preference
	http     *http.Client
	endpoint string
	key      string
	plan     plan
	now      func() time.Time
}

// config is the datasource row's config: the account's plan, which EODHD
// does not report for a credential.
type config struct {
	Plan string `json:"plan"`
}

// plan is one subscription EODHD sells.
type plan struct {
	// prices reports whether the plan serves daily bars.
	prices bool
	// years is how far back the plan's daily bars reach. Zero is the whole
	// history of a symbol.
	years int
}

// plans is each plan EODHD sells, from its pricing page. The free plan also
// allows only twenty calls a day.
var plans = map[string]plan{
	"free":               {prices: true, years: 1},
	"all_world":          {prices: true},
	"all_world_extended": {prices: true},
	"all_in_one":         {prices: true},
	"fundamentals":       {},
}

// window returns the first day for which the plan serves a daily bar as of
// now, or the zero time where the plan serves the whole history. The day is
// one later than the plan's years give. EODHD does not say whether its
// history ends at a day or at a moment, and the later day is inside the
// history under both readings.
func (p plan) window(now time.Time) time.Time {
	if p.years == 0 {
		return time.Time{}
	}
	return types.Day(now).AddDate(-p.years, 0, 1)
}

// New returns a Client for cfg that sends its requests through h. It fails
// when cfg is not valid, and when its config omits the plan or names one
// EODHD does not sell.
func New(cfg market.Config, mics mic.MICMap, h *http.Client) (*Client, error) {
	if cfg.Credential == "" {
		return nil, errors.New("eodhd needs an API key")
	}
	dec := json.NewDecoder(bytes.NewReader(cfg.JSON))
	dec.DisallowUnknownFields()
	var conf config
	if err := dec.Decode(&conf); err != nil {
		return nil, fmt.Errorf("eodhd config: %w", err)
	}
	p, ok := plans[conf.Plan]
	if !ok {
		return nil, fmt.Errorf("eodhd config names plan %q, which EODHD does not sell", conf.Plan)
	}
	ep, err := market.Endpoint(cfg, endpoint)
	if err != nil {
		return nil, err
	}
	c := &Client{mics: mics, exchange: exchanges(mics), http: h, endpoint: ep, key: cfg.Credential, plan: p, now: time.Now}
	c.prefer = market.Preference{
		Served: market.Equities,
		Order:  []types.IdentifierType{types.IdentifierTypeIsin, types.IdentifierTypeCusip, types.IdentifierTypeOpenfigiComposite, types.IdentifierTypeMicTicker},
		Lists:  func(op string) bool { return len(c.exchange[op]) > 0 },
		Mics:   mics,
	}
	return c, nil
}

// Factory returns a market.Factory that builds Clients which normalise
// venues through mics and send through h.
func Factory(mics mic.MICMap, h *http.Client) market.Factory {
	return market.Factory{
		Endpoint: endpoint,
		New: func(cfg market.Config) (market.Integration, error) {
			return New(cfg, mics, h)
		},
	}
}

// Limit allows a quarter of EODHD's 1,000 requests a minute, since one
// identifier costs up to four requests.
func (c *Client) Limit() (rate.Limit, int) { return rate.Every(4 * time.Minute / 1000), 1 }

// Endpoint is the address the Client calls.
func (c *Client) Endpoint() string { return c.endpoint }

// Batch is 1 because EODHD answers one identifier per request.
func (c *Client) Batch() int { return 1 }

// Classify reads a failure by its status. A 402 means the daily quota is
// spent, so the datasource pauses until midnight UTC, when EODHD resets it. A
// 403 means the plan does not cover the symbol, so it blocks that identifier
// alone.
func (c *Client) Classify(err error) market.Failure {
	var status market.StatusError
	if !errors.As(err, &status) {
		return market.Failure{Temporary: true, Scope: market.ScopeKey}
	}
	switch {
	case status.Code == http.StatusUnauthorized:
		return market.Failure{Scope: market.ScopeDatasource}
	case status.Code == http.StatusPaymentRequired:
		return market.Failure{Temporary: true, Scope: market.ScopeDatasource, RetryAfter: untilMidnight(c.now())}
	case status.Code == http.StatusTooManyRequests:
		return market.Failure{Temporary: true, Scope: market.ScopeKey, RetryAfter: market.Seconds(status.Header, "Retry-After")}
	case status.Code >= http.StatusInternalServerError:
		return market.Failure{Temporary: true, Scope: market.ScopeKey}
	}
	return market.Failure{Scope: market.ScopeKey}
}

// untilMidnight returns the duration to the next midnight UTC.
func untilMidnight(now time.Time) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(now)
}
