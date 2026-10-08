// Package massive is the identity integration with Massive's reference data
// API, which covers listed US stock, ETFs and funds.
//
// Massive keys a record by ticker alone.
package massive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
)

const endpoint = "https://api.massive.com"

var _ market.Identity = (*Client)(nil)

// Client is the Massive integration.
type Client struct {
	mics     mic.Table
	venues   map[string]bool
	limit    rate.Limit
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

// config is the datasource row's config: the account's plan, and the rate
// of each plan Massive sells.
type config struct {
	Plan  string          `json:"plan"`
	Plans map[string]plan `json:"plans"`
}

// plan is one of Massive's plans. A plan without PerMinute is unlimited.
type plan struct {
	PerMinute int `json:"perMinute"`
}

// New returns a Client for cfg. It fails when cfg has no credential, when
// its endpoint is not an absolute URL, when its config has an unknown
// member, or when the config names a plan that it does not describe or whose
// rate is negative.
func New(cfg market.Config, mics mic.Table, opts ...Option) (*Client, error) {
	if cfg.Credential == "" {
		return nil, errors.New("massive needs an API key")
	}
	dec := json.NewDecoder(bytes.NewReader(cfg.JSON))
	dec.DisallowUnknownFields()
	var conf config
	if err := dec.Decode(&conf); err != nil {
		return nil, fmt.Errorf("massive config: %w", err)
	}
	p, ok := conf.Plans[conf.Plan]
	if !ok {
		return nil, fmt.Errorf("massive config names plan %q, which it does not describe", conf.Plan)
	}
	if p.PerMinute < 0 {
		return nil, fmt.Errorf("massive plan %q has a negative rate", conf.Plan)
	}
	c := &Client{
		mics:     mics,
		venues:   operating(mics),
		limit:    rate.Inf,
		http:     &http.Client{Timeout: 30 * time.Second},
		endpoint: endpoint,
		key:      cfg.Credential,
	}
	if p.PerMinute > 0 {
		c.limit = rate.Every(time.Minute / time.Duration(p.PerMinute))
	}
	if cfg.Endpoint != "" {
		// A request URL that fails to parse is quoted in its error, key and
		// all, so the endpoint is checked here, before any key is added.
		if u, err := url.Parse(cfg.Endpoint); err != nil || !u.IsAbs() {
			return nil, fmt.Errorf("massive endpoint %q is not an absolute URL", cfg.Endpoint)
		}
		c.endpoint = cfg.Endpoint
	}
	for _, o := range opts {
		o(c)
	}
	return c, nil
}

// operating returns the operating MIC of every venue mics holds.
func operating(mics mic.Table) map[string]bool {
	out := map[string]bool{}
	for v := range venues {
		if op, ok := mics.Operating(v); ok {
			out[op] = true
		}
	}
	return out
}

// Factory returns a market.Factory that builds Clients which normalise
// venues through mics.
func Factory(mics mic.Table) market.Factory {
	return func(cfg market.Config) (market.Integration, error) {
		return New(cfg, mics)
	}
}

// Limit is the request rate of the account's plan. Massive rates stock
// requests separately from other asset classes, and this integration sends
// only stock requests.
func (c *Client) Limit() (rate.Limit, int) { return c.limit, 1 }

// Endpoint is the address the Client calls.
func (c *Client) Endpoint() string { return c.endpoint }

// Batch is one, since Massive answers one ticker or CUSIP per request.
func (c *Client) Batch() int { return 1 }

// statusError is a response from Massive with a status other than 200.
type statusError struct {
	code int
	body string
}

// unknown reports whether the body is Massive's answer for a ticker it does
// not know.
func (e statusError) unknown() bool {
	var body struct {
		Status string `json:"status"`
	}
	return json.Unmarshal([]byte(e.body), &body) == nil && body.Status == "NOT_FOUND"
}

// Error names the status in words, with the body where there is one:
// "massive returned too many requests".
func (e statusError) Error() string {
	text := strings.ToLower(http.StatusText(e.code))
	if text == "" {
		text = strconv.Itoa(e.code)
	}
	if e.body == "" {
		return "massive returned " + text
	}
	return fmt.Sprintf("massive returned %s: %s", text, e.body)
}

// Classify scopes a failure by its status. A 401 or 403 blocks the
// datasource, because the credential fails for every key. Any other status
// concerns only the key requested. A 429 or 5xx is temporary. Massive's 429
// omits Retry-After, so the market package's retry schedule applies.
func (c *Client) Classify(err error) market.Failure {
	var status statusError
	if !errors.As(err, &status) {
		return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	switch {
	case status.code == http.StatusUnauthorized || status.code == http.StatusForbidden:
		return market.Failure{Scope: gen.BlockScopeDatasource}
	case status.code == http.StatusTooManyRequests || status.code >= http.StatusInternalServerError:
		return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	return market.Failure{Scope: gen.BlockScopeIdentifier}
}

// Fetch asks Massive about each request in turn.
func (c *Client) Fetch(ctx context.Context, reqs []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	out := make([]market.Response[market.IdentityResult], len(reqs))
	for i, r := range reqs {
		recs, err := c.records(ctx, r.Sent)
		if err != nil {
			return nil, err
		}
		out[i].Value = identity(r.Sent, recs, c.mics)
	}
	return out, nil
}

// records returns what Massive holds for id: the record of a ticker, or the
// records the cusip filter of the ticker list matches. An unknown ticker
// returns an empty result. Massive marks an unknown ticker with the status
// NOT_FOUND in the body, which distinguishes it from a 404 for a path that
// Massive does not serve.
func (c *Client) records(ctx context.Context, id types.Identifier) ([]record, error) {
	q := url.Values{"apiKey": {c.key}}
	if id.Type == types.IdentifierTypeCusip {
		q.Set("cusip", id.Value)
		q.Set("market", "stocks")
		var page struct {
			Results []record `json:"results"`
		}
		if err := c.get(ctx, "/v3/reference/tickers", q, &page); err != nil {
			return nil, err
		}
		return page.Results, nil
	}
	ticker, _ := market.WithClassSep(id.Value, '.')
	var one struct {
		Results *record `json:"results"`
	}
	err := c.get(ctx, "/v3/reference/tickers/"+url.PathEscape(ticker), q, &one)
	var status statusError
	switch {
	case errors.As(err, &status) && status.code == http.StatusNotFound && status.unknown():
		return nil, nil
	case err != nil:
		return nil, err
	case one.Results == nil:
		return nil, nil
	}
	return []record{*one.Results}, nil
}

// get sends one request and decodes a 200's body into v.
func (c *Client) get(ctx context.Context, path string, q url.Values, v any) (err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", redacted(err))
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("massive: %w", redacted(err))
	}
	defer func() { err = errors.Join(err, resp.Body.Close()) }()
	if resp.StatusCode != http.StatusOK {
		text, rerr := io.ReadAll(io.LimitReader(resp.Body, 512))
		return errors.Join(statusError{code: resp.StatusCode, body: string(bytes.TrimSpace(text))}, rerr)
	}
	if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
		return fmt.Errorf("decode massive response: %w", err)
	}
	return nil
}

// redacted drops the URL from a transport error, since the URL carries the
// API key and the error reaches the fetch records.
func redacted(err error) error {
	var u *url.Error
	if errors.As(err, &u) {
		return u.Err
	}
	return err
}
