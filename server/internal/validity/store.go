package validity

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// FactQueries read the facts of one ticker.
type FactQueries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierEvents(ctx context.Context, arg gen.ListIdentifierEventsParams) ([]gen.ListIdentifierEventsRow, error)
	ListIdentifierCoverage(ctx context.Context, arg gen.ListIdentifierCoverageParams) ([]gen.ListIdentifierCoverageRow, error)
}

// Queries is this package's view of the validity queries.
type Queries interface {
	FactQueries
	ListDomainCoverage(ctx context.Context, domain string) ([]gen.ListDomainCoverageRow, error)
	FirstDomainTransaction(ctx context.Context, domain string) (time.Time, error)
}

var _ Queries = (*gen.Queries)(nil)

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
	if v.Type != types.IdentifierTypeMicTicker {
		return Rows{}, fmt.Errorf("%s: %w", v.Name(), ErrNotTicker)
	}
	var r Rows
	var err error
	if r.Assertions, err = q.ListIdentifierAssertions(ctx, gen.ListIdentifierAssertionsParams{Type: v.Type, Domain: v.Domain, Value: v.Value}); err != nil {
		return Rows{}, fmt.Errorf("list assertions: %w", err)
	}
	if r.Events, err = q.ListIdentifierEvents(ctx, gen.ListIdentifierEventsParams{Domain: v.Domain, Value: v.Value}); err != nil {
		return Rows{}, fmt.Errorf("list events: %w", err)
	}
	if r.Coverage, err = q.ListIdentifierCoverage(ctx, gen.ListIdentifierCoverageParams{Domain: v.Domain, Value: &v.Value}); err != nil {
		return Rows{}, fmt.Errorf("list coverage: %w", err)
	}
	return r, nil
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
		f.Coverage = append(f.Coverage, types.Period{From: c.CoveredFrom, Before: c.CoveredBefore})
	}
	return f
}

// DomainGaps returns the parts of the days from the earliest transaction that
// states a MIC_TICKER of domain up to today that no coverage of the domain
// holds. It returns none where no transaction states one. The domain must be
// one that a datasource serving whole domains covers.
func DomainGaps(ctx context.Context, q Queries, domain string, today time.Time) ([]types.Period, error) {
	first, err := q.FirstDomainTransaction(ctx, domain)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("first transaction: %w", err)
	}
	rows, err := q.ListDomainCoverage(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("list coverage: %w", err)
	}
	cover := make([]types.Period, len(rows))
	for i, r := range rows {
		cover[i] = types.Period{From: r.CoveredFrom, Before: r.CoveredBefore}
	}
	return gaps(union(cover), types.Period{From: first, Before: next(types.Day(today))}), nil
}
