// Package coverage reads, for the admin area, where each ticker's validity
// is confirmed and where it is not.
//
// Gaps are measured against confirmed validity, not against coverage. An
// attestation closes a gap by bracketing it and writes no coverage row. A
// ticker's domain is its operating MIC, and a caller may name any venue of
// it.
package coverage

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierEvents(ctx context.Context, arg gen.ListIdentifierEventsParams) ([]gen.ListIdentifierEventsRow, error)
	ListIdentifierCoverage(ctx context.Context, arg gen.ListIdentifierCoverageParams) ([]gen.ListIdentifierCoverageRow, error)
	ListTickerDomains(ctx context.Context) ([]gen.ListTickerDomainsRow, error)
	ListDomainTickers(ctx context.Context, domain string) ([]string, error)
	ListTickerTransactions(ctx context.Context, arg gen.ListTickerTransactionsParams) ([]gen.ListTickerTransactionsRow, error)
	ListInstrumentNames(ctx context.Context, ids []uuid.UUID) ([]gen.InstrumentName, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=coverage.go -destination=mock/coverage_mock.go -package=mock

// Reader reads the domains, the gaps and the tickers the coverage page shows.
type Reader struct {
	q    Queries
	mics mic.MICMap
}

// New returns a Reader.
func New(q Queries, mics mic.MICMap) *Reader {
	return &Reader{q: q, mics: mics}
}

// Gap is a part of a ticker's transaction period that no confirmed interval
// holds. The period runs from the earliest transaction whose key states the
// ticker up to and including today.
type Gap struct {
	Ticker types.Identifier
	Period types.Period
	// Keys counts the keys that state the ticker and have a transaction
	// dated inside the gap, whether or not they resolved, since a new fact
	// for the ticker replays an unresolved key that states it.
	Keys int
	// Transactions counts those keys' transactions dated inside the gap.
	Transactions int
}

// ToProto converts the gap.
func (g Gap) ToProto() *admincoveragev1.Gap {
	return &admincoveragev1.Gap{
		Ticker: g.Ticker.ToProto(), UnconfirmedFrom: ymd(g.Period.From), UnconfirmedBefore: ymd(g.Period.Before),
		Keys: int32(g.Keys), Transactions: int32(g.Transactions),
	}
}

// Ticker is one ticker's facts and the validity they derive.
type Ticker struct {
	Identifier types.Identifier
	// Days are the distinct order dates of the transactions whose keys state
	// the ticker, in order.
	Days      []time.Time
	Rows      validity.Rows
	Intervals []validity.Interval
	Gaps      []Gap
	// Names holds the identifier naming each instrument the rows and the
	// intervals refer to.
	Names map[uuid.UUID]types.Identifier
}

// ToProto converts the ticker.
func (t *Ticker) ToProto() *admincoveragev1.Ticker {
	out := &admincoveragev1.Ticker{Ticker: t.Identifier.ToProto()}
	for _, d := range t.Days {
		out.TransactionDays = append(out.TransactionDays, ymd(d))
	}
	for _, c := range t.Rows.Coverage {
		out.Coverage = append(out.Coverage, to.ProtoCoverage(c))
	}
	for _, a := range t.Rows.Assertions {
		out.Assertions = append(out.Assertions, to.ProtoAssertionItem(a.IdentifierAssertion, a.Datasource, a.AttestedBy))
	}
	for _, e := range t.Rows.Events {
		out.Events = append(out.Events, to.ProtoIdentifierEvent(e))
	}
	for _, iv := range t.Intervals {
		out.Intervals = append(out.Intervals, interval(iv))
	}
	for _, g := range t.Gaps {
		out.Gaps = append(out.Gaps, g.ToProto())
	}
	ids := make([]uuid.UUID, 0, len(t.Names))
	for id := range t.Names {
		ids = append(ids, id)
	}
	slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
	for _, id := range ids {
		out.Instruments = append(out.Instruments, &admincoveragev1.InstrumentName{Id: id.String(), Name: t.Names[id].ToProto()})
	}
	return out
}

// Domains lists the operating MICs on which keys with transactions state
// tickers.
func (r *Reader) Domains(ctx context.Context) ([]gen.ListTickerDomainsRow, error) {
	rows, err := r.q.ListTickerDomains(ctx)
	if err != nil {
		return nil, fmt.Errorf("list domains: %w", err)
	}
	return rows, nil
}

// Gaps lists the gaps of every ticker stated on domain, by affected
// transactions, then affected keys, then ticker and first day.
func (r *Reader) Gaps(ctx context.Context, domain string, today time.Time) ([]Gap, error) {
	domain = r.operating(domain)
	values, err := r.q.ListDomainTickers(ctx, domain)
	if err != nil {
		return nil, fmt.Errorf("list tickers of %s: %w", domain, err)
	}
	var out []Gap
	for _, value := range values {
		t, err := r.read(ctx, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: domain, Value: value}, today)
		if err != nil {
			return nil, err
		}
		out = append(out, t.Gaps...)
	}
	slices.SortFunc(out, func(a, b Gap) int {
		return cmp.Or(
			cmp.Compare(b.Transactions, a.Transactions),
			cmp.Compare(b.Keys, a.Keys),
			strings.Compare(a.Ticker.Value, b.Ticker.Value),
			a.Period.From.Compare(b.Period.From),
		)
	})
	return out, nil
}

// Ticker reads the facts and validity of v. It refuses an identifier that is
// not a MIC_TICKER with validity.ErrNotTicker, and a domain with
// validity.ErrSpelling.
func (r *Reader) Ticker(ctx context.Context, v types.Identifier, today time.Time) (*Ticker, error) {
	if v.Type != types.IdentifierTypeMicTicker {
		return nil, fmt.Errorf("%s: %w", v.Name(), validity.ErrNotTicker)
	}
	if v.Value == "" {
		return nil, fmt.Errorf("%s names a domain: %w", v.Name(), validity.ErrSpelling)
	}
	v.Domain = r.operating(v.Domain)
	t, err := r.read(ctx, v, today)
	if err != nil {
		return nil, err
	}
	if err := r.name(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// operating returns the operating MIC of domain, or domain itself where the
// map does not list it.
func (r *Reader) operating(domain string) string {
	if op, ok := r.mics.Operating(domain); ok {
		return op
	}
	return domain
}

// read derives v's validity and its gaps over its transaction period.
func (r *Reader) read(ctx context.Context, v types.Identifier, today time.Time) (*Ticker, error) {
	rows, err := validity.ReadRows(ctx, r.q, v)
	if err != nil {
		return nil, err
	}
	txs, err := r.q.ListTickerTransactions(ctx, gen.ListTickerTransactionsParams{Domain: v.Domain, Value: v.Value})
	if err != nil {
		return nil, fmt.Errorf("list transactions of %s: %w", v.Name(), err)
	}
	t := &Ticker{Identifier: v, Rows: rows, Intervals: validity.Derive(rows.Facts()).Intervals}
	for _, tx := range txs {
		if n := len(t.Days); n == 0 || !t.Days[n-1].Equal(tx.OrderDate) {
			t.Days = append(t.Days, tx.OrderDate)
		}
	}
	if len(txs) == 0 {
		return t, nil
	}
	period := types.Period{From: txs[0].OrderDate, Before: types.Day(today).AddDate(0, 0, 1)}
	if !period.From.Before(period.Before) {
		return t, nil
	}
	for _, p := range validity.Gaps(t.Intervals, period) {
		g := Gap{Ticker: v, Period: p}
		keys := map[uuid.UUID]bool{}
		for _, tx := range txs {
			if p.Contains(tx.OrderDate) {
				g.Transactions++
				keys[tx.StatedKeyID] = true
			}
		}
		g.Keys = len(keys)
		t.Gaps = append(t.Gaps, g)
	}
	return t, nil
}

// name fills t.Names for every instrument its rows and intervals refer to.
func (r *Reader) name(ctx context.Context, t *Ticker) error {
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	add := func(id uuid.UUID) {
		if !seen[id] {
			seen[id] = true
			ids = append(ids, id)
		}
	}
	for _, a := range t.Rows.Assertions {
		if a.IdentifierAssertion.InstrumentID != nil {
			add(*a.IdentifierAssertion.InstrumentID)
		}
	}
	for _, iv := range t.Intervals {
		add(iv.Instrument)
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := r.q.ListInstrumentNames(ctx, ids)
	if err != nil {
		return fmt.Errorf("list instrument names: %w", err)
	}
	t.Names = make(map[uuid.UUID]types.Identifier, len(names))
	for _, n := range names {
		t.Names[n.InstrumentID] = to.InstrumentName(n)
	}
	return nil
}

func interval(iv validity.Interval) *admincoveragev1.Interval {
	out := &admincoveragev1.Interval{InstrumentId: iv.Instrument.String(), Validity: types.ToProto[admincoveragev1.Validity](iv.Validity)}
	if iv.From != nil {
		from := ymd(*iv.From)
		out.ValidFrom = &from
	}
	if iv.Before != nil {
		before := ymd(*iv.Before)
		out.ValidBefore = &before
	}
	return out
}

func ymd(d time.Time) string { return d.Format(time.DateOnly) }
