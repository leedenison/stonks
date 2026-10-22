// Package openfigi is the identity integration with the OpenFIGI mapping API.
package openfigi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

const endpoint = "https://api.openfigi.com"

var (
	_ market.Identity                     = (*Client)(nil)
	_ market.Parameterised[gen.StatedKey] = (*Client)(nil)
)

// Client is the OpenFIGI integration.
type Client struct {
	mics     mic.Table
	prefer   market.Preference
	http     *http.Client
	endpoint string
	key      string
}

// New returns a Client for cfg that sends its requests through h. The
// credential is optional. It fails when cfg's endpoint is not a URL.
func New(cfg market.Config, mics mic.Table, h *http.Client) (*Client, error) {
	ep, err := market.Endpoint(cfg, endpoint)
	if err != nil {
		return nil, err
	}
	order := make([]types.IdentifierType, len(idTypes))
	for i, t := range idTypes {
		order[i] = t.typ
	}
	return &Client{mics: mics, prefer: market.Preference{Order: order, Mics: mics}, http: h, endpoint: ep, key: cfg.Credential}, nil
}

// Factory returns the factory of Clients normalising venues through mics
// and sending through h.
func Factory(mics mic.Table, h *http.Client) market.Factory {
	return market.Factory{
		Endpoint: endpoint,
		New: func(cfg market.Config) (market.Integration, error) {
			return New(cfg, mics, h)
		},
	}
}

// Limit is OpenFIGI's published rate.
func (c *Client) Limit() (rate.Limit, int) {
	if c.key == "" {
		return rate.Every(time.Minute / 25), 1
	}
	return rate.Every(6 * time.Second / 25), 1
}

// Endpoint is the address the Client calls.
func (c *Client) Endpoint() string { return c.endpoint }

// Batch is OpenFIGI's published limit on jobs per request.
func (c *Client) Batch() int {
	if c.key == "" {
		return 10
	}
	return 100
}

// jobError is a job OpenFIGI refused within a request it served, with the
// text it gave.
type jobError string

func (e jobError) Error() string { return "openfigi rejected the identifier: " + string(e) }

// Classify reads a failed request by its status. A refusal for rate or for
// OpenFIGI's health is temporary. Any other refusal of a whole request is a
// defect of the request or its API key, as 401, 400, 413 and 415 are. A
// defect recurs on every request, so it blocks the datasource until an
// administrator clears it.
func (c *Client) Classify(err error) market.Failure {
	var job jobError
	if errors.As(err, &job) {
		return market.Failure{Scope: market.ScopeKey}
	}
	var status market.StatusError
	if !errors.As(err, &status) {
		return market.Failure{Temporary: true, Scope: market.ScopeKey}
	}
	switch {
	case status.Code == http.StatusTooManyRequests:
		return market.Failure{Temporary: true, Scope: market.ScopeKey, RetryAfter: market.Seconds(status.Header, "ratelimit-reset")}
	case status.Code >= http.StatusInternalServerError:
		return market.Failure{Temporary: true, Scope: market.ScopeKey}
	}
	return market.Failure{Scope: market.ScopeDatasource}
}

// openfigiResponse is OpenFIGI's response to one job: data, a warning that
// nothing was found, or an error. OpenFIGI documents the warning as the answer
// when no FIGI is found. Its documentation's sample shows an error for that
// case. An error also reports a failed job, so only the warning counts as
// nothing found.
type openfigiResponse struct {
	Data    []result `json:"data"`
	Warning string   `json:"warning"`
	Error   string   `json:"error"`
}

// Fetch sends one mapping request with a job per request.
func (c *Client) Fetch(ctx context.Context, reqs []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	jobs := make([]job, len(reqs))
	for i, r := range reqs {
		jobs[i] = jobOf(r.Sent, filter(r.Value))
	}
	body, err := json.Marshal(jobs)
	if err != nil {
		return nil, fmt.Errorf("encode jobs: %w", err)
	}
	responses, err := c.post(ctx, body)
	if err != nil {
		return nil, err
	}
	out := make([]market.Response[market.IdentityResult], min(len(responses), len(reqs)))
	for i := range out {
		if responses[i].Error != "" {
			out[i].Err = jobError(responses[i].Error)
			continue
		}
		out[i].Value = identity(reqs[i].Sent, currency(reqs[i].Value), responses[i].Data, c.mics) //nolint:gosec // out is no longer than reqs
		// When the job filters on currency, a warning says only that nothing
		// is listed in that currency.
		out[i].Value.Unknown = responses[i].Warning != "" && jobs[i].Currency == ""
	}
	return out, nil
}

// post sends one mapping request.
func (c *Client) post(ctx context.Context, body []byte) ([]openfigiResponse, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v3/mapping", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("X-OPENFIGI-APIKEY", c.key)
	}
	var responses []openfigiResponse
	if err := market.Do(c.http, req, "openfigi", &responses); err != nil {
		return nil, err
	}
	return responses, nil
}

// result is one listing of a mapping job's response.
type result struct {
	Ticker         string  `json:"ticker"`
	ExchCode       string  `json:"exchCode"`
	SecurityType   string  `json:"securityType"`
	SecurityType2  string  `json:"securityType2"`
	MarketSector   string  `json:"marketSector"`
	ShareClassFIGI *string `json:"shareClassFIGI"`
	CompositeFIGI  *string `json:"compositeFIGI"`
}

// currency returns the code the key states, "" where it states none.
func currency(k gen.StatedKey) string {
	if k.Currency == nil {
		return ""
	}
	return *k.Currency
}
