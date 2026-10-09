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
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
)

const endpoint = "https://eodhd.com"

var _ market.Identity = (*Client)(nil)

// Client is the EODHD integration.
type Client struct {
	mics mic.Table
	// exchange maps an operating MIC to the EODHD codes that list it.
	exchange map[string][]string
	http     *http.Client
	endpoint string
	key      string
}

// Option adjusts a Client.
type Option func(*Client)

// WithHTTPClient sets the client that sends requests.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New returns a Client for cfg. It fails when cfg lacks a credential or its
// endpoint is not an absolute URL.
func New(cfg market.Config, mics mic.Table, opts ...Option) (*Client, error) {
	if cfg.Credential == "" {
		return nil, errors.New("eodhd needs an API key")
	}
	ep, err := market.Endpoint(cfg, endpoint)
	if err != nil {
		return nil, err
	}
	c := &Client{
		mics:     mics,
		exchange: exchanges(mics),
		http:     &http.Client{Timeout: 30 * time.Second},
		endpoint: ep,
		key:      cfg.Credential,
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// Factory returns a market.Factory that builds Clients which normalise
// venues through mics.
func Factory(mics mic.Table) market.Factory {
	return func(cfg market.Config) (market.Integration, error) {
		return New(cfg, mics)
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
		return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	switch {
	case status.Code == http.StatusUnauthorized:
		return market.Failure{Scope: gen.BlockScopeDatasource}
	case status.Code == http.StatusPaymentRequired:
		return market.Failure{Temporary: true, Scope: gen.BlockScopeDatasource, RetryAfter: untilMidnight(time.Now())}
	case status.Code == http.StatusTooManyRequests:
		return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier, RetryAfter: retryAfter(status.Header)}
	case status.Code >= http.StatusInternalServerError:
		return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	return market.Failure{Scope: gen.BlockScopeIdentifier}
}

// untilMidnight returns the duration to the next midnight UTC.
func untilMidnight(now time.Time) time.Duration {
	now = now.UTC()
	next := time.Date(now.Year(), now.Month(), now.Day()+1, 0, 0, 0, 0, time.UTC)
	return next.Sub(now)
}

// retryAfter reads the seconds a Retry-After header gives.
func retryAfter(h http.Header) time.Duration {
	n, err := strconv.Atoi(h.Get("Retry-After"))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}
