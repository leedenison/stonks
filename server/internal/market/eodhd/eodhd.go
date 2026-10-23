// Package eodhd is the identity integration with EODHD's reference data API,
// which covers listed stock, ETFs and funds on EODHD's exchanges worldwide.
//
// An answer costs up to four calls against EODHD's daily quota. Search
// returns the listings an identifier names, and the other calls add what
// search leaves out: the venue of a US listing, and a CUSIP.
package eodhd

import (
	"errors"
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
	now      func() time.Time
}

// New returns a Client for cfg that sends its requests through h. It fails
// when cfg lacks a credential or its endpoint is not a URL.
func New(cfg market.Config, mics mic.MICMap, h *http.Client) (*Client, error) {
	if cfg.Credential == "" {
		return nil, errors.New("eodhd needs an API key")
	}
	ep, err := market.Endpoint(cfg, endpoint)
	if err != nil {
		return nil, err
	}
	c := &Client{mics: mics, exchange: exchanges(mics), http: h, endpoint: ep, key: cfg.Credential, now: time.Now}
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
