package tickervalidity

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// FactQueries read the facts of one ticker.
type FactQueries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierAssertionCoverage(ctx context.Context, arg gen.ListIdentifierAssertionCoverageParams) ([]gen.ListIdentifierAssertionCoverageRow, error)
}

var _ FactQueries = (*gen.Queries)(nil)

// Rows is a ticker's facts as the queries return them, with the source of
// each assertion and assertion coverage.
type Rows struct {
	Assertions        []gen.ListIdentifierAssertionsRow
	AssertionCoverage []gen.ListIdentifierAssertionCoverageRow
}

// Diagnose reads the facts of t and returns the conflicts among them.
func Diagnose(ctx context.Context, q FactQueries, t types.MarketTicker) (Diagnosis, error) {
	rows, err := ReadRows(ctx, q, t)
	if err != nil {
		return Diagnosis{}, err
	}
	return derive(rows.facts()).Diagnosis, nil
}

// ValidityQueries read stored ticker validity.
type ValidityQueries interface {
	ListTickerValidity(ctx context.Context, arg gen.ListTickerValidityParams) ([]gen.ListTickerValidityRow, error)
}

var _ ValidityQueries = (*gen.Queries)(nil)

// Load reads the stored ticker validity of each of ts, with one query however
// many tickers there are. Each of ts is in the result. A ticker that names
// nothing on any day has empty Intervals.
func Load(ctx context.Context, q ValidityQueries, ts []types.MarketTicker) (map[types.MarketTicker]Intervals, error) {
	out := make(map[types.MarketTicker]Intervals, len(ts))
	var arg gen.ListTickerValidityParams
	for _, t := range ts {
		if _, seen := out[t]; seen {
			continue
		}
		out[t] = nil
		arg.Markets, arg.Values = append(arg.Markets, t.Market), append(arg.Values, t.Value)
	}
	if len(out) == 0 {
		return out, nil
	}
	rows, err := q.ListTickerValidity(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list ticker validity: %w", err)
	}
	for _, r := range rows {
		t := types.MarketTicker{Market: r.Market, Value: r.Value}
		out[t] = append(out[t], Interval{From: r.ValidFrom, Before: r.ValidBefore, Instr: r.InstrID, Validity: r.Validity})
	}
	return out, nil
}

// NamingQueries read the stored ticker validity of the tickers that name
// instruments.
type NamingQueries interface {
	ValidityQueries
	ListTickersNaming(ctx context.Context, instrs []uuid.UUID) ([]gen.ListTickersNamingRow, error)
}

var _ NamingQueries = (*gen.Queries)(nil)

// LoadNaming reads the stored ticker validity of each ticker that names one of
// instrs on some day, with two queries however many instruments there are.
// The Intervals of each ticker hold every instrument it names, including
// those outside instrs.
func LoadNaming(ctx context.Context, q NamingQueries, instrs []uuid.UUID) (map[types.MarketTicker]Intervals, error) {
	if len(instrs) == 0 {
		return map[types.MarketTicker]Intervals{}, nil
	}
	rows, err := q.ListTickersNaming(ctx, instrs)
	if err != nil {
		return nil, fmt.Errorf("list the tickers naming the instruments: %w", err)
	}
	ts := make([]types.MarketTicker, len(rows))
	for i, r := range rows {
		ts[i] = types.MarketTicker{Market: r.Market, Value: r.Value}
	}
	return Load(ctx, q, ts)
}

// ReadRows reads the assertion and assertion coverage rows of t.
func ReadRows(ctx context.Context, q FactQueries, t types.MarketTicker) (Rows, error) {
	rows, err := readRowsOf(ctx, q, []types.MarketTicker{t})
	if err != nil {
		return Rows{}, err
	}
	return rows[t], nil
}

// readRowsOf reads the assertion and assertion coverage rows of each of ts,
// with two queries however many tickers there are.
func readRowsOf(ctx context.Context, q FactQueries, ts []types.MarketTicker) (map[types.MarketTicker]Rows, error) {
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
	ps, err := q.ListIdentifierAssertionCoverage(ctx, gen.ListIdentifierAssertionCoverageParams{Markets: markets, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list assertion coverage: %w", err)
	}
	for _, p := range ps {
		t := types.MarketTicker{Market: p.Market, Value: p.Value}
		r := out[t]
		r.AssertionCoverage = append(r.AssertionCoverage, p)
		out[t] = r
	}
	return out, nil
}

// facts converts the rows to the facts derive reads.
func (r Rows) facts() facts {
	var f facts
	for _, row := range r.Assertions {
		ia := row.IdentifierAssertion
		src := Source{Answered: row.Answered}
		switch {
		case row.Datasource == nil:
			src.standing = attested
		case row.IsEventSource:
			src.standing = eventSource
		}
		if row.Datasource != nil && row.Precedence != nil {
			src.Datasource, src.Precedence = *row.Datasource, *row.Precedence
		}
		if row.AskedFrom != nil && row.AskedBefore != nil {
			src.Asked = types.Period{From: *row.AskedFrom, Before: *row.AskedBefore}
		}
		switch {
		case ia.FetchKeyID != nil:
			src.Answer = *ia.FetchKeyID
		case ia.RunID != nil:
			src.Answer = *ia.RunID
		}
		a := Assertion{
			Period: types.Period{From: ia.AssertedFrom, Before: ia.AssertedBefore}, Instr: ia.InstrID, Source: src,
		}
		if src.standing == eventSource {
			a.EdgeFrom, a.EdgeBefore = ia.EdgeFrom, ia.EdgeBefore
		}
		if ia.Venue != nil {
			a.Stated.Venue = *ia.Venue
		}
		if ia.Currency != nil {
			a.Stated.Currency = *ia.Currency
		}
		f.Assertions = append(f.Assertions, a)
	}
	for _, p := range r.AssertionCoverage {
		if !p.IsEventSource {
			continue
		}
		f.AssertionCoverage = append(f.AssertionCoverage, AssertionCoverage{
			Period: types.Period{From: p.CoveredFrom, Before: p.CoveredBefore},
			Source: Source{Datasource: p.Datasource, Precedence: p.Precedence, Answer: p.FetchKeyID, standing: eventSource},
		})
	}
	return f
}
