package tickervalidity

import (
	"context"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// FactQueries read the facts of one ticker.
type FactQueries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierPeriodAssertions(ctx context.Context, arg gen.ListIdentifierPeriodAssertionsParams) ([]gen.ListIdentifierPeriodAssertionsRow, error)
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
// each assertion and period assertion.
type Rows struct {
	Assertions       []gen.ListIdentifierAssertionsRow
	PeriodAssertions []gen.ListIdentifierPeriodAssertionsRow
}

// Read reads the facts of t: its assertions, and the period assertions of the
// ticker and of its market.
func Read(ctx context.Context, q FactQueries, t types.MarketTicker) (Facts, error) {
	rows, err := ReadRows(ctx, q, t)
	if err != nil {
		return Facts{}, err
	}
	return rows.Facts(), nil
}

// ReadRows reads the assertion and period assertion rows of t.
func ReadRows(ctx context.Context, q FactQueries, t types.MarketTicker) (Rows, error) {
	rows, err := ReadRowsOf(ctx, q, []types.MarketTicker{t})
	if err != nil {
		return Rows{}, err
	}
	return rows[t], nil
}

// ReadRowsOf reads the assertion and period assertion rows of each of ts,
// with two queries however many tickers there are.
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
		t := types.MarketTicker{Market: a.IdentifierAssertion.Market, Value: a.Value}
		r := out[t]
		r.Assertions = append(r.Assertions, a)
		out[t] = r
	}
	ps, err := q.ListIdentifierPeriodAssertions(ctx, gen.ListIdentifierPeriodAssertionsParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list period assertions: %w", err)
	}
	for _, p := range ps {
		for t, r := range out {
			if t.Market == p.Market && (p.Value == nil || *p.Value == t.Value) {
				r.PeriodAssertions = append(r.PeriodAssertions, p)
				out[t] = r
			}
		}
	}
	return out, nil
}

// Facts converts the rows to the facts Derive reads.
func (r Rows) Facts() Facts {
	var f Facts
	for _, row := range r.Assertions {
		ia := row.IdentifierAssertion
		src := Source{Attested: row.Datasource == nil}
		if row.Datasource != nil && row.Precedence != nil {
			src.Datasource, src.Precedence = *row.Datasource, *row.Precedence
		}
		if ia.FetchKeyID != nil {
			src.FetchKey = *ia.FetchKeyID
		}
		a := Assertion{At: ia.AssertedOn, Instr: ia.InstrID, Source: src}
		if ia.Edge != nil {
			a.Edge = *ia.Edge
		}
		if ia.Venue != nil {
			a.Stated.Venue = *ia.Venue
		}
		if ia.Currency != nil {
			a.Stated.Currency = *ia.Currency
		}
		f.Assertions = append(f.Assertions, a)
	}
	for _, p := range r.PeriodAssertions {
		f.PeriodAssertions = append(f.PeriodAssertions, PeriodAssertion{
			Period: types.Period{From: p.AssertedFrom, Before: p.AssertedBefore},
			Source: Source{Datasource: p.Datasource, Precedence: p.Precedence, FetchKey: p.FetchKeyID},
		})
	}
	return f
}
