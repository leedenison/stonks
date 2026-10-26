package massive

import (
	"context"
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
)

// callOf is the Massive request that serves r.
func callOf(r market.IdentifierEventRequest) call {
	if r.Identifier.Type != types.IdentifierTypeMicTicker {
		return chainCall
	}
	return lookupCall
}

// Serves returns the identifier to send for r, or an error when Massive
// cannot answer r. A ticker goes out without its venue, since Massive answers
// for the composite, and the composite's identifier facts hold at every venue.
func (e events) Serves(r market.IdentifierEventRequest) (types.Identifier, error) {
	s := r.Identifier
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
	case !r.Period.Before.Equal(r.Period.From.AddDate(0, 0, 1)):
		return types.Identifier{}, fmt.Errorf("ticker over %d days rejected: a ticker is answered on one day", int(r.Period.Before.Sub(r.Period.From).Hours()/24))
	case r.Period.From.Before(since):
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

// chain returns the ticker events of the entity id names, newest first. It
// returns an empty list when Massive does not know id.
func (c *Client) chain(ctx context.Context, id types.Identifier) ([]event, error) {
	q := url.Values{"apiKey": {c.key}, "types": {"ticker_change"}}
	var body struct {
		Results struct {
			Events []event `json:"events"`
		} `json:"results"`
	}
	err := c.get(ctx, "/vX/reference/tickers/"+url.PathEscape(id.Value)+"/events", q, &body)
	if _, err := notFound(err); err != nil {
		return nil, err
	}
	return body.Results.Events, nil
}
