package corporate

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Answer is the stored answer of one datasource for one ticker.
type Answer struct {
	Datasource string
	Precedence int32
	Events     []Event
	// Coverage is the days over which the datasource reported every
	// corporate event of the ticker, less each day an unhandled event of the
	// ticker removes.
	Coverage []types.Period
}

// Event is one corporate event of an answer.
type Event struct {
	ID     uuid.UUID
	ExDate time.Time
	Ratio  types.Ratio
	// Type is the datasource's own word for the event, or empty when the
	// datasource gave no word.
	Type    string
	Handled bool
}

// Read returns the answers of each of ts, in precedence order, with what
// they hold within p: the events with an ex-date in p, and the coverage
// clipped to p. Read leaves out an answer that holds neither within p. It
// takes each day of an unhandled event of a ticker out of the coverage of
// every datasource of that ticker. It reads with two queries however many
// tickers there are.
func Read(ctx context.Context, q ReadQueries, ts []types.MarketTicker, p types.Period) (map[types.MarketTicker][]Answer, error) {
	stored, err := readStored(ctx, q, ts, p)
	if err != nil {
		return nil, err
	}
	out := make(map[types.MarketTicker][]Answer, len(stored))
	for t, as := range stored {
		var unhandled []time.Time
		for _, a := range as {
			for _, e := range a.Events {
				if !e.Handled {
					unhandled = append(unhandled, e.ExDate)
				}
			}
		}
		for _, a := range as {
			var cov []types.Period
			for _, c := range remove(a.Coverage, unhandled) {
				if c, ok := clip(c, p); ok {
					cov = append(cov, c)
				}
			}
			if len(cov) == 0 && len(a.Events) == 0 {
				continue
			}
			a.Coverage = cov
			out[t] = append(out[t], a)
		}
	}
	return out, nil
}

// always is a period that holds every day an answer may cover.
var always = types.Period{From: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Before: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}

// readStored reads the answers of each of ts, in precedence order, with
// their events whose ex-date p holds and their stored coverage, whole. A
// ticker without an answer is absent.
func readStored(ctx context.Context, q ReadQueries, ts []types.MarketTicker, p types.Period) (map[types.MarketTicker][]Answer, error) {
	want := map[types.MarketTicker]bool{}
	var markets, values []string
	for _, t := range ts {
		if !want[t] {
			want[t] = true
			markets, values = append(markets, t.Market), append(values, t.Value)
		}
	}
	out := map[types.MarketTicker][]Answer{}
	if len(want) == 0 {
		return out, nil
	}
	rows, err := q.ListCorporateAnswers(ctx, gen.ListCorporateAnswersParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list corporate event answers: %w", err)
	}
	type at struct {
		t types.MarketTicker
		i int
	}
	byID := map[uuid.UUID]at{}
	for _, row := range rows {
		a := row.CorporateEventAnswer
		t := types.MarketTicker{Market: a.Market, Value: a.Value}
		if !want[t] {
			continue
		}
		s := Answer{Datasource: a.Datasource, Precedence: row.Precedence}
		if a.CoveredFrom != nil && a.CoveredBefore != nil {
			s.Coverage = []types.Period{{From: *a.CoveredFrom, Before: *a.CoveredBefore}}
		}
		byID[a.ID] = at{t: t, i: len(out[t])}
		out[t] = append(out[t], s)
	}
	if len(byID) == 0 {
		return out, nil
	}
	events, err := q.ListCorporateEvents(ctx, gen.ListCorporateEventsParams{Markets: markets, Values: values, FromDay: p.From, BeforeDay: p.Before})
	if err != nil {
		return nil, fmt.Errorf("list corporate events: %w", err)
	}
	for _, e := range events {
		pos, ok := byID[e.AnswerID]
		if !ok {
			continue
		}
		a := &out[pos.t][pos.i]
		ev := Event{ID: e.ID, ExDate: e.ExDate, Ratio: types.Ratio{From: e.RatioFrom, To: e.RatioTo}, Handled: e.Handled}
		if e.Type != nil {
			ev.Type = *e.Type
		}
		a.Events = append(a.Events, ev)
	}
	return out, nil
}

// remove returns ps less each of days, splitting a period around a day it
// holds.
func remove(ps []types.Period, days []time.Time) []types.Period {
	days = slices.Clone(days)
	slices.SortFunc(days, func(a, b time.Time) int { return a.Compare(b) })
	var out []types.Period
	for _, p := range ps {
		for _, d := range days {
			if !p.Contains(d) {
				continue
			}
			if p.From.Before(d) {
				out = append(out, types.Period{From: p.From, Before: d})
			}
			p.From = d.AddDate(0, 0, 1)
		}
		if p.From.Before(p.Before) {
			out = append(out, p)
		}
	}
	return out
}

// clip returns the days p holds of c, and reports false when the overlap is
// empty.
func clip(c, p types.Period) (types.Period, bool) {
	if p.From.After(c.From) {
		c.From = p.From
	}
	if p.Before.Before(c.Before) {
		c.Before = p.Before
	}
	return c, c.From.Before(c.Before)
}
