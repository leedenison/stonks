package corpact

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// Answer is the stored answer of a market's corporate actions source for one
// ticker.
type Answer struct {
	Datasource string
	Acts       []Act
	// Coverage is the days over which the datasource reported every
	// corporate action of the ticker, less each day an unknown action of the
	// ticker removes.
	Coverage []types.Period
}

// Act is one corporate action of an answer.
type Act struct {
	ID     uuid.UUID
	ExDate time.Time
	Ratio  types.Ratio
	// Type is the datasource's own word for the action, or empty when the
	// datasource gave no word.
	Type string
	// Handled is whether the system judged the action to restate the
	// quantity held when it stored it.
	Handled bool
	// Decision is the type an administrator decided the action is, or empty
	// while undecided. Restates and Unknown read it.
	Decision gen.CorpActDecision
}

// Read returns the answer of each of ts from its market's corporate actions
// source, with what it holds within p: the actions with an ex-date in p, and
// the coverage clipped to p. Read leaves out a ticker whose answer holds
// neither within p. It reads with two queries however many tickers there
// are.
func Read(ctx context.Context, q ReadQueries, ts []types.MarketTicker, p types.Period) (map[types.MarketTicker]Answer, error) {
	want := map[types.MarketTicker]bool{}
	var markets, values []string
	for _, t := range ts {
		if !want[t] {
			want[t] = true
			markets, values = append(markets, t.Market), append(values, t.Value)
		}
	}
	out := map[types.MarketTicker]Answer{}
	if len(want) == 0 {
		return out, nil
	}
	rows, err := q.ListCorpActAnswers(ctx, gen.ListCorpActAnswersParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list corporate action answers: %w", err)
	}
	if len(rows) == 0 {
		return out, nil
	}
	byID := map[uuid.UUID]types.MarketTicker{}
	stored := map[types.MarketTicker]*Answer{}
	for _, row := range rows {
		a := row.CorpActAnswer
		t := types.MarketTicker{Market: row.Market, Value: row.Value}
		s := &Answer{Datasource: a.Datasource}
		if a.CoveredFrom != nil && a.CoveredBefore != nil {
			s.Coverage = []types.Period{{From: *a.CoveredFrom, Before: *a.CoveredBefore}}
		}
		byID[a.ID] = t
		stored[t] = s
	}
	acts, err := q.ListCorpActs(ctx, gen.ListCorpActsParams{Markets: markets, Values: values, FromDay: p.From, BeforeDay: p.Before})
	if err != nil {
		return nil, fmt.Errorf("list corporate actions: %w", err)
	}
	for _, e := range acts {
		t, ok := byID[e.AnswerID]
		if !ok {
			continue
		}
		act := Act{ID: e.ID, ExDate: e.ExDate, Ratio: types.Ratio{From: e.RatioFrom, To: e.RatioTo}, Handled: e.Handled}
		if e.Type != nil {
			act.Type = *e.Type
		}
		if e.Decision != nil {
			act.Decision = *e.Decision
		}
		stored[t].Acts = append(stored[t].Acts, act)
	}
	for t, a := range stored {
		var unknown []time.Time
		for _, e := range a.Acts {
			if e.Unknown() {
				unknown = append(unknown, e.ExDate)
			}
		}
		var cov []types.Period
		for _, c := range remove(a.Coverage, unknown) {
			if c, ok := c.Clip(p); ok {
				cov = append(cov, c)
			}
		}
		if len(cov) == 0 && len(a.Acts) == 0 {
			continue
		}
		a.Coverage = cov
		out[t] = *a
	}
	return out, nil
}

// always is a period that holds every day an answer may cover.
var always = types.Period{From: time.Date(1, 1, 1, 0, 0, 0, 0, time.UTC), Before: time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)}

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
