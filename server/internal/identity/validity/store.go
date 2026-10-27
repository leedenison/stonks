package validity

import (
	"context"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// FactQueries read the facts of one ticker.
type FactQueries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierEvents(ctx context.Context, arg gen.ListIdentifierEventsParams) ([]gen.ListIdentifierEventsRow, error)
	ListIdentifierCoverage(ctx context.Context, arg gen.ListIdentifierCoverageParams) ([]gen.ListIdentifierCoverageRow, error)
}

var _ FactQueries = (*gen.Queries)(nil)

// Of reads the facts of t and derives its intervals.
func Of(ctx context.Context, q FactQueries, t types.MarketTicker) ([]Interval, error) {
	f, err := Read(ctx, q, t)
	if err != nil {
		return nil, err
	}
	return Derive(f).Intervals, nil
}

// Rows is a ticker's facts as the queries return them, with the source of
// each assertion and coverage row.
type Rows struct {
	Assertions []gen.ListIdentifierAssertionsRow
	Events     []gen.ListIdentifierEventsRow
	Coverage   []gen.ListIdentifierCoverageRow
}

// Read reads the facts of t: its assertions, its events, and the coverage of
// the ticker and of its market.
func Read(ctx context.Context, q FactQueries, t types.MarketTicker) (Facts, error) {
	rows, err := ReadRows(ctx, q, t)
	if err != nil {
		return Facts{}, err
	}
	return rows.Facts(), nil
}

// ReadRows reads the assertion, event and coverage rows of t.
func ReadRows(ctx context.Context, q FactQueries, t types.MarketTicker) (Rows, error) {
	rows, err := ReadRowsOf(ctx, q, []types.MarketTicker{t})
	if err != nil {
		return Rows{}, err
	}
	return rows[t], nil
}

// ReadRowsOf reads the assertion, event and coverage rows of each of ts,
// with three queries however many tickers there are.
func ReadRowsOf(ctx context.Context, q FactQueries, ts []types.MarketTicker) (map[types.MarketTicker]Rows, error) {
	out := make(map[types.MarketTicker]Rows, len(ts))
	var markets, values []string
	for _, t := range ts {
		if _, seen := out[t]; seen {
			continue
		}
		out[t] = Rows{}
		markets, values = append(markets, t.Market), append(values, t.Value)
	}
	if len(out) == 0 {
		return out, nil
	}
	as, err := q.ListIdentifierAssertions(ctx, gen.ListIdentifierAssertionsParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list assertions: %w", err)
	}
	for _, a := range as {
		t := types.MarketTicker{Market: a.IdentifierAssertion.Domain, Value: a.IdentifierAssertion.Value}
		if r, ok := out[t]; ok {
			r.Assertions = append(r.Assertions, a)
			out[t] = r
		}
	}
	es, err := q.ListIdentifierEvents(ctx, gen.ListIdentifierEventsParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	for _, e := range es {
		named := []string{e.Value}
		if e.ToValue != nil && *e.ToValue != e.Value {
			named = append(named, *e.ToValue)
		}
		for _, value := range named {
			t := types.MarketTicker{Market: e.Market, Value: value}
			if r, ok := out[t]; ok {
				r.Events = append(r.Events, e)
				out[t] = r
			}
		}
	}
	cs, err := q.ListIdentifierCoverage(ctx, gen.ListIdentifierCoverageParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list coverage: %w", err)
	}
	for _, c := range cs {
		for t, r := range out {
			if t.Market == c.Market && (c.Value == nil || *c.Value == t.Value) {
				r.Coverage = append(r.Coverage, c)
				out[t] = r
			}
		}
	}
	return out, nil
}

// Facts converts the rows to the facts Derive reads.
func (r Rows) Facts() Facts {
	var f Facts
	for _, a := range r.Assertions {
		src := Source{Attested: a.Datasource == nil}
		if a.Datasource != nil && a.Precedence != nil {
			src.Datasource, src.Precedence = *a.Datasource, *a.Precedence
		}
		if a.IdentifierAssertion.FetchKeyID != nil {
			src.FetchKey = *a.IdentifierAssertion.FetchKeyID
		}
		f.Assertions = append(f.Assertions, Assertion{At: a.IdentifierAssertion.AssertedOn, Instrument: a.IdentifierAssertion.InstrumentID, Source: src})
	}
	for _, e := range r.Events {
		f.Events = append(f.Events, Event{On: e.EffectiveOn, FetchKey: e.FetchKeyID})
	}
	for _, c := range r.Coverage {
		f.Coverage = append(f.Coverage, Coverage{Period: types.Period{From: c.CoveredFrom, Before: c.CoveredBefore}, Datasource: c.Datasource, FetchKey: c.FetchKeyID})
	}
	return f
}
