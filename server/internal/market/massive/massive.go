// Package massive is the identity integration with Massive's reference data
// API, which covers listed US stock, ETFs and funds.
package massive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

const endpoint = "https://api.massive.com"

// Redacted names the members of a Massive record that a recording replaces:
// the issuer's address, telephone number and description. The massive.vcr
// entry of docker/vcrproxy/hosts.json lists the same members, and
// TestMassiveRedaction checks that the two match.
var Redacted = []string{"address", "phone_number", "description"}

var _ market.Identity = (*Client)(nil)

// Client is the Massive integration.
type Client struct {
	mics     mic.Table
	venues   map[string]bool
	prefer   market.Preference
	limit    rate.Limit
	http     *http.Client
	endpoint string
	key      string
}

// config is the datasource row's config: the account's plan, which the
// credential does not reveal.
type config struct {
	Plan string `json:"plan"`
}

// plans is the rate of each plan Massive sells, from its pricing page:
// Basic allows five calls a minute and the paid plans are unlimited.
var plans = map[string]rate.Limit{
	"basic":     rate.Every(12 * time.Second),
	"starter":   rate.Inf,
	"developer": rate.Inf,
	"advanced":  rate.Inf,
}

// New returns a Client for cfg that sends its requests through h. It fails
// when cfg has no credential, when its endpoint is not a URL, when its
// config has an unknown member, or when the config names a plan Massive does
// not sell.
func New(cfg market.Config, mics mic.Table, h *http.Client) (*Client, error) {
	if cfg.Credential == "" {
		return nil, errors.New("massive needs an API key")
	}
	dec := json.NewDecoder(bytes.NewReader(cfg.JSON))
	dec.DisallowUnknownFields()
	var conf config
	if err := dec.Decode(&conf); err != nil {
		return nil, fmt.Errorf("massive config: %w", err)
	}
	limit, ok := plans[conf.Plan]
	if !ok {
		return nil, fmt.Errorf("massive config names plan %q, which Massive does not sell", conf.Plan)
	}
	ep, err := market.Endpoint(cfg, endpoint)
	if err != nil {
		return nil, err
	}
	c := &Client{
		mics:     mics,
		venues:   operating(mics),
		limit:    limit,
		http:     h,
		endpoint: ep,
		key:      cfg.Credential,
	}
	c.prefer = market.Preference{
		Served: market.Equities,
		Order:  []types.IdentifierType{types.IdentifierTypeCusip, types.IdentifierTypeMicTicker},
		Lists:  func(op string) bool { return c.venues[op] },
		Mics:   mics,
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
// venues through mics and send through h.
func Factory(mics mic.Table, h *http.Client) market.Factory {
	return market.Factory{
		Endpoint: endpoint,
		New: func(cfg market.Config) (market.Integration, error) {
			return New(cfg, mics, h)
		},
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

// unknown reports whether a refusal is Massive's answer for a ticker it does
// not know.
func unknown(e market.StatusError) bool {
	var body struct {
		Status string `json:"status"`
	}
	return e.Code == http.StatusNotFound && json.Unmarshal([]byte(e.Body), &body) == nil && body.Status == "NOT_FOUND"
}

// Classify scopes a failure by its status. A 401 or 403 blocks the
// datasource, because the credential fails for every key. Any other status
// concerns only the key requested. A 429 or 5xx is temporary. Massive's 429
// omits Retry-After, so the market package's retry schedule applies.
func (c *Client) Classify(err error) market.Failure {
	var status market.StatusError
	if !errors.As(err, &status) {
		return market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}
	}
	switch {
	case status.Code == http.StatusUnauthorized || status.Code == http.StatusForbidden:
		return market.Failure{Scope: gen.BlockScopeDatasource}
	case status.Code == http.StatusTooManyRequests || status.Code >= http.StatusInternalServerError:
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
	var status market.StatusError
	switch {
	case errors.As(err, &status) && unknown(status):
		return nil, nil
	case err != nil:
		return nil, err
	case one.Results == nil:
		return nil, nil
	}
	return []record{*one.Results}, nil
}

// get sends one request and decodes a 200's body into v.
func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	return market.Get(ctx, c.http, "massive", c.endpoint+path+"?"+q.Encode(), v)
}
