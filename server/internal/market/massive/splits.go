package massive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

var _ market.CorpActs = (*Client)(nil)

// pageLimit is the most rows Massive returns on one page of splits.
const pageLimit = "5000"

// errPaged is an answer that runs past one page. A ticker has fewer splits
// than a page holds. Coverage of part of an answer would hide the rest.
var errPaged = errors.New("the splits of one ticker ran past one page")

// handledTypes are the adjustment types Massive documents. Each type restates
// a quantity. Massive also gives these types to price factors, such as the
// fall in a parent's price at a spin-off.
var handledTypes = map[string]bool{"forward_split": true, "reverse_split": true, "stock_dividend": true}

// splits is Massive's side of the corporate actions kind.
type splits struct{ c *Client }

// split is one corporate action as Massive states it.
type split struct {
	ExecutionDate  string      `json:"execution_date"`
	SplitFrom      json.Number `json:"split_from"`
	SplitTo        json.Number `json:"split_to"`
	AdjustmentType string      `json:"adjustment_type"`
}

// CorpActs is Massive's server of corporate actions.
func (c *Client) CorpActs() market.Server[market.CorpActRequest, market.CorpActResult] {
	return splits{c: c}
}

// Serves returns the ticker to send for r, or an error when Massive cannot
// answer r.
func (s splits) Serves(r market.CorpActRequest) (types.Identifier, error) {
	if !r.Period.Before.After(since) {
		return types.Identifier{}, fmt.Errorf("period before %s rejected: the data starts on %s", r.Period.Before.Format(time.DateOnly), since.Format(time.DateOnly))
	}
	return s.c.sent(r.Ticker)
}

// Fetch asks Massive for the whole history of each ticker in turn. The
// history is that of the symbol, so a symbol that passed between companies
// carries the actions of each. Massive answers a ticker it does not cover as
// one that has never split. Coverage is the requested period, starting no
// earlier than since.
func (s splits) Fetch(ctx context.Context, reqs []market.Request[market.CorpActRequest]) ([]market.Response[market.CorpActResult], error) {
	out := make([]market.Response[market.CorpActResult], len(reqs))
	for i, r := range reqs {
		q := url.Values{"apiKey": {s.c.key}, "ticker": {r.Sent.Value}, "limit": {pageLimit}}
		var page struct {
			Results []split `json:"results"`
			NextURL string  `json:"next_url"`
		}
		if err := s.c.get(ctx, "/stocks/v1/splits", q, &page); err != nil {
			return nil, err
		}
		if page.NextURL != "" {
			out[i].Err = errPaged
			continue
		}
		acts, err := fromSplits(page.Results)
		if err != nil {
			return nil, err
		}
		p := r.Value.Period
		if p.From.Before(since) {
			p.From = since
		}
		out[i].Value = market.CorpActResult{Acts: acts, Coverage: &p}
	}
	return out, nil
}

// fromSplits converts Massive's splits into corporate actions.
func fromSplits(ss []split) ([]market.CorpAct, error) {
	out := make([]market.CorpAct, len(ss))
	for i, s := range ss {
		ex, err := time.Parse(time.DateOnly, s.ExecutionDate)
		if err != nil {
			return nil, fmt.Errorf("split execution date: %w", err)
		}
		from, err := decimal.NewFromString(s.SplitFrom.String())
		if err != nil {
			return nil, fmt.Errorf("split from: %w", err)
		}
		to, err := decimal.NewFromString(s.SplitTo.String())
		if err != nil {
			return nil, fmt.Errorf("split to: %w", err)
		}
		out[i] = market.CorpAct{
			ExDate: ex, Ratio: types.Ratio{From: from, To: to},
			Type: s.AdjustmentType, Handled: handledTypes[s.AdjustmentType],
		}
	}
	return out, nil
}
