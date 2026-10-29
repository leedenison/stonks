package eodhd

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

var _ market.CorpActs = (*Client)(nil)

// unknownSymbol is the body of the 404 that answers a symbol EODHD does not
// know.
const unknownSymbol = "Symbol not found"

// splits is EODHD's side of the corporate actions kind.
type splits struct{ c *Client }

// split is one corporate action as EODHD states it. Split is new over old,
// as in "7.000000/1.000000".
type split struct {
	Date  string `json:"date"`
	Split string `json:"split"`
}

// CorpActs is EODHD's server of corporate actions.
func (c *Client) CorpActs() market.Server[market.CorpActRequest, market.CorpActResult] {
	return splits{c: c}
}

// Serves returns the symbol to send for r, as a DATASOURCE_TICKER in EODHD's
// namespace, or an error when EODHD cannot answer r. A venue that several
// EODHD exchanges list is refused, since the ticker does not say which.
func (s splits) Serves(r market.CorpActRequest) (types.Identifier, error) {
	t := r.Ticker
	if t.IsDomain() {
		return types.Identifier{}, fmt.Errorf("domain '%s' rejected: the datasource answers for one ticker", t.Domain)
	}
	codes := s.c.exchange[t.Domain]
	switch len(codes) {
	case 0:
		return types.Identifier{}, fmt.Errorf("venue '%s' rejected", t.Domain)
	case 1:
	default:
		return types.Identifier{}, fmt.Errorf("venue '%s' rejected: EODHD lists it as %s", t.Domain, strings.Join(codes, " and "))
	}
	code, ok := market.WithClassSep(t.Value, '-')
	if !ok {
		return types.Identifier{}, fmt.Errorf("ticker '%s' rejected: more than one share class separator", t.Value)
	}
	return types.Identifier{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: code + "." + codes[0]}, nil
}

// Fetch asks EODHD for the whole history of each symbol in turn. An answer
// covers the requested period. EODHD documents one year of split history for
// its free plan. In practice the plan answers back to the 1970s. EODHD lists
// a split only once it takes effect. A symbol EODHD does not know is
// answered with an empty answer and nil coverage.
func (s splits) Fetch(ctx context.Context, reqs []market.Request[market.CorpActRequest]) ([]market.Response[market.CorpActResult], error) {
	out := make([]market.Response[market.CorpActResult], len(reqs))
	for i, r := range reqs {
		var ss []split
		err := s.c.get(ctx, "/api/splits/"+url.PathEscape(r.Sent.Value), url.Values{}, &ss)
		if unknown(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		acts, err := fromSplits(ss)
		if err != nil {
			return nil, err
		}
		p := r.Value.Period
		out[i].Value = market.CorpActResult{Acts: acts, Coverage: &p}
	}
	return out, nil
}

// unknown reports whether err is EODHD's answer that it does not know the
// symbol. EODHD bills it as a call, as it does any answer.
func unknown(err error) bool {
	var status market.StatusError
	return errors.As(err, &status) && status.Code == http.StatusNotFound && status.Body == unknownSymbol
}

// fromSplits converts EODHD's splits into corporate actions, each unhandled.
// EODHD omits the type, and its splits also carry stock dividends, spin-off
// price factors and reorganisations. A ratio alone cannot tell these from a
// split.
func fromSplits(ss []split) ([]market.CorpAct, error) {
	out := make([]market.CorpAct, len(ss))
	for i, s := range ss {
		ex, err := time.Parse(time.DateOnly, s.Date)
		if err != nil {
			return nil, fmt.Errorf("split date: %w", err)
		}
		to, from, ok := strings.Cut(s.Split, "/")
		if !ok || strings.Contains(from, "/") {
			return nil, fmt.Errorf("split '%s' is not one ratio", s.Split)
		}
		after, err := decimal.NewFromString(to)
		if err != nil {
			return nil, fmt.Errorf("split '%s': %w", s.Split, err)
		}
		before, err := decimal.NewFromString(from)
		if err != nil {
			return nil, fmt.Errorf("split '%s': %w", s.Split, err)
		}
		out[i] = market.CorpAct{ExDate: ex, Ratio: types.Ratio{From: before, To: after}}
	}
	return out, nil
}
