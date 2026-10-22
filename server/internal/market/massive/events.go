package massive

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// since is the first day Massive's reference data covers. A point in time
// lookup of an earlier day answers that the ticker is unknown.
var since = time.Date(2003, time.September, 10, 0, 0, 0, 0, time.UTC)

// events is Massive's side of the identifier events kind.
type events struct{ c *Client }

// call is the Massive request that serves an identifier events request.
type call int

const (
	// chainCall is the ticker events of the entity a CUSIP or composite FIGI
	// names.
	chainCall call = iota
	// lookupCall is the record of a ticker on one day.
	lookupCall
	// retiredCall is the records of a ticker that Massive lists as delisted.
	retiredCall
)

// callOf is the Massive request that serves r.
func callOf(r market.IdentifierEventRequest) call {
	switch {
	case r.Subject.Type != types.IdentifierTypeMicTicker:
		return chainCall
	case r.Period.Before.Sub(r.Period.From) <= 24*time.Hour:
		return lookupCall
	}
	return retiredCall
}

// Serves returns the identifier to send for r, or an error when Massive
// cannot answer r. A ticker goes out without its venue, since Massive answers
// for the composite, and the composite's identifier facts hold at every venue.
func (e events) Serves(r market.IdentifierEventRequest) (types.Identifier, error) {
	s := r.Subject
	switch s.Type {
	case types.IdentifierTypeCusip, types.IdentifierTypeOpenfigiComposite:
		return s, nil
	case types.IdentifierTypeMicTicker:
	default:
		return types.Identifier{}, fmt.Errorf("identifier type '%s' rejected", s.Type)
	}
	switch {
	case s.IsDomain():
		return types.Identifier{}, fmt.Errorf("domain '%s' rejected: the datasource answers for one entity", s.Domain)
	case !e.c.prefer.Lists(s.Domain):
		return types.Identifier{}, fmt.Errorf("venue '%s' rejected", s.Domain)
	case preferred(s.Value):
		return types.Identifier{}, fmt.Errorf("preferred share '%s' rejected", s.Value)
	case callOf(r) == lookupCall && r.Period.From.Before(since):
		return types.Identifier{}, fmt.Errorf("day %s rejected: the data starts on %s", r.Period.From.Format(time.DateOnly), since.Format(time.DateOnly))
	}
	ticker, ok := market.WithClassSep(s.Value, '.')
	if !ok {
		return types.Identifier{}, fmt.Errorf("ticker '%s' rejected: more than one share class separator", s.Value)
	}
	return types.Identifier{Type: types.IdentifierTypeMicTicker, Value: ticker}, nil
}

// event is one ticker event as Massive states it.
type event struct {
	Type         string `json:"type"`
	Date         string `json:"date"`
	TickerChange struct {
		Ticker string `json:"ticker"`
	} `json:"ticker_change"`
}

// chain returns the ticker events of the entity id names, newest first. The
// second result reports whether Massive answered that it does not know id.
func (c *Client) chain(ctx context.Context, id types.Identifier) ([]event, bool, error) {
	q := url.Values{"apiKey": {c.key}, "types": {"ticker_change"}}
	var body struct {
		Results struct {
			Events []event `json:"events"`
		} `json:"results"`
	}
	err := c.get(ctx, "/vX/reference/tickers/"+url.PathEscape(id.Value)+"/events", q, &body)
	var status market.StatusError
	switch {
	case errors.As(err, &status) && unknown(status):
		return nil, true, nil
	case err != nil:
		return nil, false, err
	}
	return body.Results.Events, false, nil
}

// at returns the record of ticker on day. The second result reports whether
// Massive answered that ticker was not in use on day.
func (c *Client) at(ctx context.Context, ticker string, day time.Time) (*record, bool, error) {
	q := url.Values{"apiKey": {c.key}, "date": {day.Format(time.DateOnly)}}
	var body struct {
		Results *record `json:"results"`
	}
	err := c.get(ctx, "/v3/reference/tickers/"+url.PathEscape(ticker), q, &body)
	var status market.StatusError
	switch {
	case errors.As(err, &status) && unknown(status):
		return nil, true, nil
	case err != nil:
		return nil, false, err
	}
	return body.Results, false, nil
}

// delisting is a record Massive lists as inactive. Delisted is nil where
// Massive omits the date.
type delisting struct {
	record
	Delisted *time.Time `json:"delisted_utc"`
}

// errTruncated reports a list of delistings longer than one page.
var errTruncated = errors.New("massive: the delistings run past one page")

// delisted returns the records of ticker that Massive lists as inactive. It
// fails on a list longer than one page, since a partial list would miss a
// retirement.
func (c *Client) delisted(ctx context.Context, ticker string) ([]delisting, error) {
	q := url.Values{"apiKey": {c.key}, "ticker": {ticker}, "active": {"false"}, "market": {"stocks"}, "limit": {"1000"}}
	var page struct {
		Results []delisting `json:"results"`
		Next    string      `json:"next_url"`
	}
	if err := c.get(ctx, "/v3/reference/tickers", q, &page); err != nil {
		return nil, err
	}
	if page.Next != "" {
		return nil, errTruncated
	}
	return page.Results, nil
}
