// Package massive is the identity, identifier events and corporate actions
// integration with Massive's API, which covers listed US stock, ETFs and
// funds.
package massive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"maps"
	"net/http"
	"net/url"
	"slices"
	"strings"
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
	mics   mic.MICMap
	venues OperatingMICSet
	// home is the market of every venue, where each identifier fact is
	// stated.
	home     string
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
// not sell. It also fails unless Massive's venues cover one market whole,
// since Massive states each fact for the market.
func New(cfg market.Config, mics mic.MICMap, h *http.Client) (*Client, error) {
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
	venues := operating(mics)
	home, err := homeOf(venues, mics)
	if err != nil {
		return nil, err
	}
	c := &Client{
		mics:     mics,
		venues:   venues,
		home:     home,
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

// OperatingMICSet is the operating MICs of the US venues where Massive lists
// stock. A MIC is in the set where it maps to true.
type OperatingMICSet map[string]bool

// operating returns the operating MIC of every venue mics holds.
func operating(mics mic.MICMap) OperatingMICSet {
	out := OperatingMICSet{}
	for v := range venues {
		if op, ok := mics.Operating(v); ok {
			out[op] = true
		}
	}
	return out
}

// homeOf returns the one market of venues. It fails where venues span two
// markets, or where the market holds an operating MIC outside venues.
func homeOf(venues OperatingMICSet, mics mic.MICMap) (string, error) {
	var home string
	for _, op := range slices.Sorted(maps.Keys(venues)) {
		m, _ := mics.Market(op)
		if home != "" && m != home {
			return "", fmt.Errorf("massive venues span the markets %s and %s", home, m)
		}
		home = m
	}
	for _, v := range mics.Venues(home) {
		if op, _ := mics.Operating(v); !venues[op] {
			return "", fmt.Errorf("massive does not list %s of the market %s", op, home)
		}
	}
	return home, nil
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

// Limit is the request rate of the account's plan. Massive counts the ticker
// calls in a reference data bucket, separate from the asset class buckets.
// Massive publishes the limit of neither that bucket nor the ticker events
// call, so the plan's rate for stock stands in for both.
func (c *Client) Limit() (rate.Limit, int) { return c.limit, 1 }

// Window is the first day Massive answers for, on every day. Every plan
// answers back to the first day of Massive's data, whatever the endpoint
// pages state for Basic.
func (c *Client) Window(time.Time) time.Time { return since }

// Endpoint is the address the Client calls.
func (c *Client) Endpoint() string { return c.endpoint }

// Batch is one, since Massive answers one ticker or CUSIP per request.
func (c *Client) Batch() int { return 1 }

// notFound reports whether err is Massive's answer for an unknown ticker or
// entity. Such a 404 carries the status NOT_FOUND in its body. A 404 for a
// path Massive does not serve lacks it. It returns any other error
// unchanged.
func notFound(err error) (bool, error) {
	var status market.StatusError
	var body struct {
		Status string `json:"status"`
	}
	if errors.As(err, &status) && status.Code == http.StatusNotFound && json.Unmarshal([]byte(status.Body), &body) == nil && body.Status == "NOT_FOUND" {
		return true, nil
	}
	return false, err
}

// unknownKey reports whether e is the splits endpoint's refusal of the
// credential. The endpoint answers 404 where the reference endpoints answer
// 401. Massive does not document this.
func unknownKey(e market.StatusError) bool {
	var body struct {
		Status string `json:"status"`
		Error  string `json:"error"`
	}
	return e.Code == http.StatusNotFound && json.Unmarshal([]byte(e.Body), &body) == nil &&
		body.Status == "ERROR" && body.Error == "Unknown API Key"
}

// Classify scopes a failure by its status, and a 403 or 404 by its body as
// well; see forbidden and unknownKey. A refused credential blocks the
// datasource, because the credential serves every key. An answer that runs
// past one page fails its key. A 429 or 5xx is temporary. Massive's 429 omits
// Retry-After, so the market package's retry schedule applies.
func (c *Client) Classify(err error) market.Failure {
	if errors.Is(err, errPaged) {
		return market.Failure{Scope: market.ScopeKey}
	}
	var status market.StatusError
	if !errors.As(err, &status) {
		return market.Failure{Temporary: true, Scope: market.ScopeKey}
	}
	switch {
	case status.Code == http.StatusUnauthorized, unknownKey(status):
		return market.Failure{Scope: market.ScopeDatasource}
	case status.Code == http.StatusForbidden:
		return market.Failure{Scope: forbidden(status)}
	case status.Code == http.StatusTooManyRequests || status.Code >= http.StatusInternalServerError:
		return market.Failure{Temporary: true, Scope: market.ScopeKey}
	}
	return market.Failure{Scope: market.ScopeKey}
}

// forbidden reads the scope of a 403 from its body. Massive's two refusals
// both carry the status NOT_AUTHORIZED, and only the message tells them
// apart. A plan that lacks the data would refuse every key of the kind, so
// that refusal blocks the datasource. A refusal for dates outside the plan's
// history concerns only the request. When the integration does not
// recognise the wording of a 403, only the key is lost.
func forbidden(e market.StatusError) market.Scope {
	var body struct {
		Status  string `json:"status"`
		Message string `json:"message"`
	}
	switch {
	case json.Unmarshal([]byte(e.Body), &body) != nil || body.Status != "NOT_AUTHORIZED":
		return market.ScopeKey
	case strings.Contains(body.Message, "timeframe"):
		return market.ScopeRequest
	case strings.Contains(body.Message, "not entitled"):
		return market.ScopeDatasource
	}
	return market.ScopeKey
}

// Fetch asks Massive about each request in turn.
func (c *Client) Fetch(ctx context.Context, reqs []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	out := make([]market.Response[market.IdentityResult], len(reqs))
	for i, r := range reqs {
		recs, unknown, err := c.records(ctx, r.Sent)
		if err != nil {
			return nil, err
		}
		out[i].Value = identity(r.Sent, recs, c.mics)
		out[i].Value.Unknown = unknown
	}
	return out, nil
}

// records returns what Massive holds for id: the record of a ticker, or the
// records the cusip filter of the ticker list matches. The second result
// reports whether Massive stated that it does not know the ticker. Massive
// answers as at the latest date, so an unknown ticker names nothing there on
// the day of the answer. A cusip that matches nothing is not unknown, since
// no ticker was sent.
func (c *Client) records(ctx context.Context, id types.Identifier) ([]record, bool, error) {
	if id.Type == types.IdentifierTypeCusip {
		q := url.Values{"apiKey": {c.key}, "cusip": {id.Value}, "market": {"stocks"}}
		var page struct {
			Results []record `json:"results"`
		}
		if err := c.get(ctx, "/v3/reference/tickers", q, &page); err != nil {
			return nil, false, err
		}
		return page.Results, false, nil
	}
	ticker, _ := market.WithClassSep(id.Value, '.')
	rec, unknown, err := c.ticker(ctx, ticker, time.Time{})
	if err != nil || rec == nil {
		return nil, unknown, err
	}
	return []record{*rec}, false, nil
}

// ticker returns the record of ticker as at day, or as at the latest date
// when day is zero. The second result reports whether Massive answered that
// it does not know the ticker, or that the ticker was not in use on day.
func (c *Client) ticker(ctx context.Context, ticker string, day time.Time) (*record, bool, error) {
	q := url.Values{"apiKey": {c.key}}
	if !day.IsZero() {
		q.Set("date", day.Format(time.DateOnly))
	}
	var body struct {
		Results *record `json:"results"`
	}
	err := c.get(ctx, "/v3/reference/tickers/"+url.PathEscape(ticker), q, &body)
	if unknown, err := notFound(err); unknown || err != nil {
		return nil, unknown, err
	}
	return body.Results, false, nil
}

// get sends one request and decodes a 200's body into v.
func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	return market.Get(ctx, c.http, "massive", c.endpoint+path+"?"+q.Encode(), v)
}
