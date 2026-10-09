package eodhd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"slices"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
)

// searchLimit is the largest number of rows a search returns.
const searchLimit = "500"

// row is one listing a search returns. ISIN is null when EODHD states none.
type row struct {
	Code     string `json:"Code"`
	Exchange string `json:"Exchange"`
	Type     string `json:"Type"`
	Currency string `json:"Currency"`
	ISIN     string `json:"ISIN"`
}

// symbol is the row's ticker in EODHD's namespace, as in AAPL.US.
func (r row) symbol() string { return r.Code + "." + r.Exchange }

// mapping is one symbol id-mapping returns, with the identifiers EODHD gives
// it. The mapping's FIGI is left unread, since EODHD gives the composite FIGI
// for some symbols and an exchange's FIGI for others.
type mapping struct {
	Symbol string `json:"symbol"`
	ISIN   string `json:"isin"`
	CUSIP  string `json:"cusip"`
}

// answer is what EODHD returned for one identifier sent.
type answer struct {
	rows []row
	// mappings maps a symbol to what id-mapping gave it.
	mappings map[string]mapping
	// venues maps a US code to the name of its venue.
	venues map[string]string
}

// Fetch asks EODHD about each request in turn.
func (c *Client) Fetch(ctx context.Context, reqs []market.Request[gen.StatedKey]) ([]market.Response[market.IdentityResult], error) {
	out := make([]market.Response[market.IdentityResult], len(reqs))
	for i, r := range reqs {
		a, err := c.answer(ctx, r.Sent)
		if err != nil {
			return nil, err
		}
		out[i].Value = c.identity(r.Sent, a)
	}
	return out, nil
}

// answer returns what EODHD says about id. A CUSIP or composite FIGI reaches
// search through the one ISIN that id-mapping gives it.
func (c *Client) answer(ctx context.Context, id types.Identifier) (answer, error) {
	a := answer{mappings: map[string]mapping{}, venues: map[string]string{}}
	var err error
	switch id.Type {
	case types.IdentifierTypeIsin:
		a.rows, err = c.searchISIN(ctx, id.Value)
	case types.IdentifierTypeCusip, types.IdentifierTypeOpenfigiComposite:
		filter := "cusip"
		if id.Type == types.IdentifierTypeOpenfigiComposite {
			filter = "figi"
		}
		var maps []mapping
		if maps, err = c.idMapping(ctx, filter, id.Value); err != nil {
			return a, err
		}
		isin := sharedISIN(maps, func(m mapping) string { return m.ISIN })
		if isin == "" {
			return a, nil
		}
		a.rows, err = c.searchISIN(ctx, isin)
	default:
		a.rows, err = c.searchTicker(ctx, id)
	}
	if err != nil || len(a.rows) == 0 {
		return a, err
	}

	if isin := sharedISIN(a.rows, func(r row) string { return r.ISIN }); isin != "" {
		maps, err := c.idMapping(ctx, "isin", isin)
		if err != nil {
			return a, err
		}
		for _, m := range maps {
			prev, ok := a.mappings[m.Symbol]
			if ok && prev.CUSIP != m.CUSIP {
				// EODHD keeps a symbol's old CUSIP beside its current one.
				m.CUSIP = ""
			}
			a.mappings[m.Symbol] = m
		}
	}
	var us []string
	for _, r := range a.rows {
		if r.Exchange == usCode && !slices.Contains(us, r.Code) {
			us = append(us, r.Code)
		}
	}
	if len(us) > 0 {
		// Sorted, so that a recording matches the request again.
		slices.Sort(us)
		listed, err := c.symbols(ctx, us)
		if err != nil {
			return a, err
		}
		for _, l := range listed {
			a.venues[l.Code] = l.Exchange
		}
	}
	return a, nil
}

// sharedISIN returns the one ISIN every item carries, or "" where they carry
// none or several.
func sharedISIN[T any](items []T, isin func(T) string) string {
	out := ""
	for _, it := range items {
		v := isin(it)
		if v == "" || (out != "" && v != out) {
			return ""
		}
		out = v
	}
	return out
}

// searchISIN returns the listings that carry isin.
func (c *Client) searchISIN(ctx context.Context, isin string) ([]row, error) {
	rows, err := c.search(ctx, isin, "")
	return slices.DeleteFunc(rows, func(r row) bool { return r.ISIN != isin }), err
}

// searchTicker returns the listings whose code is the ticker id names. A
// venue narrows the search to the exchanges that list it.
func (c *Client) searchTicker(ctx context.Context, id types.Identifier) ([]row, error) {
	code, _ := market.WithClassSep(id.Value, '-')
	var exchanges []string
	switch {
	case id.Type == types.IdentifierTypeOpenfigiTicker:
		exchanges = []string{usCode}
	case id.Domain != "":
		exchanges = c.exchange[id.Domain]
	}
	filter := ""
	if len(exchanges) == 1 {
		filter = exchanges[0]
	}
	rows, err := c.search(ctx, code, filter)
	return slices.DeleteFunc(rows, func(r row) bool {
		return r.Code != code || (len(exchanges) > 0 && !slices.Contains(exchanges, r.Exchange))
	}), err
}

// search returns what EODHD's search finds for query, on exchange where it
// is set. The search matches names as readily as codes.
func (c *Client) search(ctx context.Context, query, exchange string) ([]row, error) {
	q := url.Values{"limit": {searchLimit}}
	if exchange != "" {
		q.Set("exchange", exchange)
	}
	var rows []row
	err := c.get(ctx, "/api/search/"+url.PathEscape(query), q, &rows)
	return rows, err
}

// idMapping returns the symbols id-mapping matches with filter, one of
// isin, cusip or figi. Its CUSIP is the one identifier search lacks.
func (c *Client) idMapping(ctx context.Context, filter, value string) ([]mapping, error) {
	var page struct {
		Data []mapping `json:"data"`
	}
	err := c.get(ctx, "/api/id-mapping", url.Values{fmt.Sprintf("filter[%s]", filter): {value}}, &page)
	return page.Data, err
}

// listing is one row of the exchange symbol list.
type listing struct {
	Code     string `json:"Code"`
	Exchange string `json:"Exchange"`
}

// symbols returns the US listings of codes, each with its venue's name.
func (c *Client) symbols(ctx context.Context, codes []string) ([]listing, error) {
	var out []listing
	err := c.get(ctx, "/api/exchange-symbol-list/"+usCode, url.Values{"symbols": {strings.Join(codes, ",")}}, &out)
	return out, err
}

// get sends one request and decodes a 200's body into v. A 404 is EODHD's
// answer for a symbol it does not know, and leaves v empty.
func (c *Client) get(ctx context.Context, path string, q url.Values, v any) error {
	q.Set("api_token", c.key)
	q.Set("fmt", "json")
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.endpoint+path+"?"+q.Encode(), nil)
	if err != nil {
		return fmt.Errorf("build request: %w", market.WithoutURL(err))
	}
	err = market.Do(c.http, req, "eodhd", v)
	var status market.StatusError
	if errors.As(err, &status) && status.Code == http.StatusNotFound {
		return nil
	}
	return err
}
