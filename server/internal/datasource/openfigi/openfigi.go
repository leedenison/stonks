// Package openfigi is the identity integration with the OpenFIGI mapping API.
//
// A stated key is sent under its strongest identifier OpenFIGI accepts, and
// every listing OpenFIGI maps it to is a candidate.  A candidate carries the
// share class and composite FIGIs, the ticker under OpenFIGI's exchange code,
// and the ticker under the operating MIC where the exchange code names exactly
// one venue.  A composite exchange code names a market rather than a venue, so
// its listings carry no MIC_TICKER.
//
// OpenFIGI answers no currency.  Where the key states one, the call filters on
// it strictly, so every candidate is in the stated currency.
//
// A ticker is sent in OpenFIGI's form, with a share class separated by a
// slash, and a MIC_TICKER is answered with the class separated by a dot.
package openfigi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/datasource"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
)

const endpoint = "https://api.openfigi.com"

var _ datasource.Identity = (*Client)(nil)

// Client is the OpenFIGI integration.
type Client struct {
	mics     mic.Table
	http     *http.Client
	endpoint string
	key      string
}

// Option adjusts a Client.
type Option func(*Client)

// WithHTTPClient sets the client requests are sent through.
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) { c.http = h }
}

// New returns a Client for cfg. The credential is optional.
func New(cfg datasource.Config, mics mic.Table, opts ...Option) *Client {
	c := &Client{mics: mics, http: &http.Client{Timeout: 30 * time.Second}, endpoint: endpoint, key: cfg.Credential}
	if cfg.Endpoint != "" {
		c.endpoint = cfg.Endpoint
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// Factory returns the factory of Clients normalising venues through mics.
func Factory(mics mic.Table) datasource.Factory {
	return func(cfg datasource.Config) (datasource.Integration, error) {
		return New(cfg, mics), nil
	}
}

// Limit is OpenFIGI's published rate.
func (c *Client) Limit() (rate.Limit, int) {
	if c.key == "" {
		return rate.Every(time.Minute / 25), 1
	}
	return rate.Every(6 * time.Second / 25), 1
}

// Batch is OpenFIGI's published limit on jobs per request.
func (c *Client) Batch() int {
	if c.key == "" {
		return 10
	}
	return 100
}

// statusError returned on a request error.
type statusError struct {
	code       int
	retryAfter time.Duration
	body       string
}

func (e statusError) Error() string {
	return fmt.Sprintf("openfigi answered %d: %s", e.code, e.body)
}

// jobError is a job OpenFIGI refused within a request it served.
type jobError string

func (e jobError) Error() string { return "openfigi refused the job: " + string(e) }

// Classify reads a failed request by its status. A whole request refused
// other than for its rate or the provider's health is malformed for every key
// or unauthorised, and so blocks the datasource.
func (c *Client) Classify(err error) datasource.Failure {
	var job jobError
	if errors.As(err, &job) {
		return datasource.Failure{Scope: gen.BlockScopeIdentifier}
	}
	var status statusError
	if !errors.As(err, &status) {
		return datasource.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	switch {
	case status.code == http.StatusTooManyRequests:
		return datasource.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier, RetryAfter: status.retryAfter}
	case status.code >= http.StatusInternalServerError:
		return datasource.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	return datasource.Failure{Scope: gen.BlockScopeDatasource}
}

// reply is OpenFIGI's answer to one job: data, a warning that nothing was
// found, or an error.
type reply struct {
	Data    []result `json:"data"`
	Warning string   `json:"warning"`
	Error   string   `json:"error"`
}

// Fetch sends one mapping request with a job per request.
func (c *Client) Fetch(ctx context.Context, reqs []datasource.FetchRequest[datasource.StatedKey]) ([]datasource.FetchResponse[datasource.IdentityResult], error) {
	jobs := make([]job, len(reqs))
	for i, r := range reqs {
		jobs[i] = jobOf(r.Sent, r.Value.Currency)
	}
	body, err := json.Marshal(jobs)
	if err != nil {
		return nil, fmt.Errorf("encode jobs: %w", err)
	}
	replies, err := c.post(ctx, body)
	if err != nil {
		return nil, err
	}
	out := make([]datasource.FetchResponse[datasource.IdentityResult], min(len(replies), len(reqs)))
	for i := range out {
		if replies[i].Error != "" {
			out[i].Err = jobError(replies[i].Error)
			continue
		}
		out[i].Value = answer(reqs[i].Sent, reqs[i].Value.Currency, replies[i].Data, c.mics)
	}
	return out, nil
}

// post sends one mapping request.
func (c *Client) post(ctx context.Context, body []byte) (replies []reply, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.endpoint+"/v3/mapping", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if c.key != "" {
		req.Header.Set("X-OPENFIGI-APIKEY", c.key)
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("openfigi: %w", err)
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		text, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		status := statusError{code: resp.StatusCode, retryAfter: reset(resp.Header), body: string(bytes.TrimSpace(text))}
		return nil, errors.Join(status, rerr)
	}
	if err := json.NewDecoder(resp.Body).Decode(&replies); err != nil {
		return nil, fmt.Errorf("decode openfigi answer: %w", err)
	}
	return replies, nil
}

// reset reads the seconds until the rate limit window reopens.
func reset(h http.Header) time.Duration {
	n, err := strconv.Atoi(h.Get("ratelimit-reset"))
	if err != nil || n <= 0 {
		return 0
	}
	return time.Duration(n) * time.Second
}

// result is one listing of a mapping job's answer.
type result struct {
	Ticker         string  `json:"ticker"`
	ExchCode       string  `json:"exchCode"`
	SecurityType   string  `json:"securityType"`
	SecurityType2  string  `json:"securityType2"`
	MarketSector   string  `json:"marketSector"`
	ShareClassFIGI *string `json:"shareClassFIGI"`
	CompositeFIGI  *string `json:"compositeFIGI"`
}
