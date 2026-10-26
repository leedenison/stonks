package validity

import (
	"context"
	"errors"
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

// ErrNotTicker is returned for an identifier other than a MIC_TICKER, the only
// type that has events and coverage.
var ErrNotTicker = errors.New("validity is kept for a MIC_TICKER only")

// Of reads the facts of v and derives its intervals.
func Of(ctx context.Context, q FactQueries, v types.Identifier) ([]Interval, error) {
	f, err := Read(ctx, q, v)
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

// Read reads the facts of v: its assertions, its events, and the coverage of
// the ticker and of its domain.
func Read(ctx context.Context, q FactQueries, v types.Identifier) (Facts, error) {
	rows, err := ReadRows(ctx, q, v)
	if err != nil {
		return Facts{}, err
	}
	return rows.Facts(), nil
}

// ReadRows reads the assertion, event and coverage rows of v. It returns
// ErrNotTicker when v is not a MIC_TICKER.
func ReadRows(ctx context.Context, q FactQueries, v types.Identifier) (Rows, error) {
	rows, err := ReadRowsOf(ctx, q, []types.Identifier{v})
	if err != nil {
		return Rows{}, err
	}
	return rows[v], nil
}

// ReadRowsOf reads the assertion, event and coverage rows of each of vs,
// with three queries however many tickers there are. It returns ErrNotTicker
// when one of vs is not a MIC_TICKER.
func ReadRowsOf(ctx context.Context, q FactQueries, vs []types.Identifier) (map[types.Identifier]Rows, error) {
	out := make(map[types.Identifier]Rows, len(vs))
	var domains, values []string
	for _, v := range vs {
		if v.Type != types.IdentifierTypeMicTicker {
			return nil, fmt.Errorf("%s: %w", v.Name(), ErrNotTicker)
		}
		if _, seen := out[v]; seen {
			continue
		}
		out[v] = Rows{}
		domains, values = append(domains, v.Domain), append(values, v.Value)
	}
	if len(out) == 0 {
		return out, nil
	}
	ticker := func(domain, value string) types.Identifier {
		return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: domain, Value: value}
	}
	as, err := q.ListIdentifierAssertions(ctx, gen.ListIdentifierAssertionsParams{Type: types.IdentifierTypeMicTicker, Domains: domains, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list assertions: %w", err)
	}
	for _, a := range as {
		v := ticker(a.IdentifierAssertion.Domain, a.IdentifierAssertion.Value)
		if r, ok := out[v]; ok {
			r.Assertions = append(r.Assertions, a)
			out[v] = r
		}
	}
	es, err := q.ListIdentifierEvents(ctx, gen.ListIdentifierEventsParams{Domains: domains, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list events: %w", err)
	}
	for _, e := range es {
		named := []string{e.Value}
		if e.ToValue != nil && *e.ToValue != e.Value {
			named = append(named, *e.ToValue)
		}
		for _, value := range named {
			if r, ok := out[ticker(e.Domain, value)]; ok {
				r.Events = append(r.Events, e)
				out[ticker(e.Domain, value)] = r
			}
		}
	}
	cs, err := q.ListIdentifierCoverage(ctx, gen.ListIdentifierCoverageParams{Domains: domains, Values: values})
	if err != nil {
		return nil, fmt.Errorf("list coverage: %w", err)
	}
	for _, c := range cs {
		for v, r := range out {
			if v.Domain == c.Domain && (c.Value == nil || *c.Value == v.Value) {
				r.Coverage = append(r.Coverage, c)
				out[v] = r
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
