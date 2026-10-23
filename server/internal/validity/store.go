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

// Queries is this package's view of the validity queries.
type Queries interface {
	ListValueAssertions(ctx context.Context, arg gen.ListValueAssertionsParams) ([]gen.ListValueAssertionsRow, error)
	ListValueEvents(ctx context.Context, arg gen.ListValueEventsParams) ([]gen.ListValueEventsRow, error)
	ListValueCoverage(ctx context.Context, arg gen.ListValueCoverageParams) ([]gen.ListValueCoverageRow, error)
	ListDomainCoverage(ctx context.Context, domain string) ([]gen.ListDomainCoverageRow, error)
	FirstDomainTransaction(ctx context.Context, domain string) (time.Time, error)
}

var _ Queries = (*gen.Queries)(nil)

// ErrNotTicker is returned for an identifier other than a MIC_TICKER, the only
// type that has events and coverage.
var ErrNotTicker = errors.New("validity is kept for a MIC_TICKER only")

// Of reads the facts of v and derives its intervals.
func Of(ctx context.Context, q Queries, v types.Identifier) ([]Interval, error) {
	f, err := Read(ctx, q, v)
	if err != nil {
		return nil, err
	}
	return Derive(f).Intervals, nil
}

// Read reads the facts of v: its assertions, its events, and the coverage of
// the value and of its domain.
func Read(ctx context.Context, q Queries, v types.Identifier) (Facts, error) {
	if v.Type != types.IdentifierTypeMicTicker {
		return Facts{}, fmt.Errorf("%s: %w", v.Name(), ErrNotTicker)
	}
	var f Facts
	as, err := q.ListValueAssertions(ctx, gen.ListValueAssertionsParams{Type: v.Type, Domain: v.Domain, Value: v.Value})
	if err != nil {
		return Facts{}, fmt.Errorf("list assertions: %w", err)
	}
	for _, a := range as {
		src := Source{Attested: a.Datasource == nil}
		if a.Datasource != nil && a.Precedence != nil {
			src.Datasource, src.Precedence = *a.Datasource, *a.Precedence
		}
		if a.FetchKeyID != nil {
			src.FetchKey = *a.FetchKeyID
		}
		f.Assertions = append(f.Assertions, Assertion{At: a.AssertedOn, Instrument: a.InstrumentID, Source: src})
	}
	evs, err := q.ListValueEvents(ctx, gen.ListValueEventsParams{Domain: v.Domain, Value: v.Value})
	if err != nil {
		return Facts{}, fmt.Errorf("list events: %w", err)
	}
	for _, e := range evs {
		f.Events = append(f.Events, Event{On: e.EffectiveOn, FetchKey: e.FetchKeyID})
	}
	cover, err := q.ListValueCoverage(ctx, gen.ListValueCoverageParams{Domain: v.Domain, Value: &v.Value})
	if err != nil {
		return Facts{}, fmt.Errorf("list coverage: %w", err)
	}
	for _, c := range cover {
		f.Coverage = append(f.Coverage, types.Period{From: c.CoveredFrom, Before: c.CoveredBefore})
	}
	return f, nil
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
