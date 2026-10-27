// Package coverage reads, for the admin area, where each ticker's validity
// is confirmed and where it is not.
//
// Gaps are measured against confirmed validity, not against coverage. An
// attestation closes a gap by bracketing it and writes no coverage row. A
// ticker is read at its market, and its transactions are those of the keys
// that state it at any venue of the market.
package coverage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/validity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierEvents(ctx context.Context, arg gen.ListIdentifierEventsParams) ([]gen.ListIdentifierEventsRow, error)
	ListIdentifierCoverage(ctx context.Context, arg gen.ListIdentifierCoverageParams) ([]gen.ListIdentifierCoverageRow, error)
	ListInstrumentTickers(ctx context.Context, instrumentID uuid.UUID) ([]gen.ListInstrumentTickersRow, error)
	CountTickerTransactions(ctx context.Context, probes [][]byte) (int32, error)
	ListTickerTransactions(ctx context.Context, probes [][]byte) ([]gen.ListTickerTransactionsRow, error)
	ListInstrumentNames(ctx context.Context, ids []uuid.UUID) ([]gen.InstrumentName, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=coverage.go -destination=mock/coverage_mock.go -package=mock

// Reader reads the tickers of an instrument, and the facts and validity of
// a ticker.
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
		UnconfirmedFrom: ymd(g.Period.From), UnconfirmedBefore: ymd(g.Period.Before),
		Keys: int32(g.Keys), Transactions: int32(g.Transactions),
	}
}

// Page is one ticker as the admin area shows it: its facts, the validity
// they derive, and its gaps.
type Page struct {
	Ticker types.MarketTicker
	// Days are the distinct as-at dates of the transactions whose keys state
	// the ticker, in order.
	Days      []time.Time
	Rows      validity.Rows
	Intervals []validity.Interval
	Gaps      []Gap
	// Names holds the identifier naming each instrument the rows and the
	// intervals refer to.
	Names map[uuid.UUID]types.Identifier
}

// ToProto converts the page.
func (t *Page) ToProto() *admincoveragev1.Ticker {
	out := &admincoveragev1.Ticker{Ticker: t.Ticker.ToProto()}
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

// InstrumentTicker is a ticker at its market that names an instrument, now
// or in an assertion, with the count of transactions whose keys state it at
// any venue of the market.
type InstrumentTicker struct {
	Ticker       types.MarketTicker
	Current      bool
	Transactions int
	// Venues are the operating MICs of the listings that carry the ticker.
	Venues []string
}

// ToProto converts the ticker.
func (t InstrumentTicker) ToProto() *admincoveragev1.InstrumentTicker {
	return &admincoveragev1.InstrumentTicker{Ticker: t.Ticker.ToProto(), Current: t.Current, Transactions: int32(t.Transactions), Venues: t.Venues}
}

// InstrumentTickers lists the tickers that name the instrument, now or in an
// assertion, the most transacted first.
func (r *Reader) InstrumentTickers(ctx context.Context, instrument uuid.UUID) ([]InstrumentTicker, error) {
	rows, err := r.q.ListInstrumentTickers(ctx, instrument)
	if err != nil {
		return nil, fmt.Errorf("list tickers of %s: %w", instrument, err)
	}
	var out []InstrumentTicker
	for _, row := range rows {
		v := types.MarketTicker{Market: row.Market, Value: row.Value}
		probes, err := r.mics.TickerProbes([]types.MarketTicker{v})
		if err != nil {
			return nil, err
		}
		n, err := r.q.CountTickerTransactions(ctx, probes)
		if err != nil {
			return nil, fmt.Errorf("count transactions of %s: %w", v.Name(), err)
		}
		out = append(out, InstrumentTicker{Ticker: v, Current: row.Current, Transactions: int(n), Venues: row.Venues})
	}
	slices.SortStableFunc(out, func(a, b InstrumentTicker) int {
		if a.Transactions != b.Transactions {
			return b.Transactions - a.Transactions
		}
		if a.Current != b.Current {
			if a.Current {
				return -1
			}
			return 1
		}
		return strings.Compare(a.Ticker.Name(), b.Ticker.Name())
	})
	return out, nil
}

// Ticker reads the facts and validity of v. It refuses an unknown market
// with validity.ErrSpelling.
func (r *Reader) Ticker(ctx context.Context, v types.MarketTicker, today time.Time) (*Page, error) {
	if !r.mics.IsMarket(v.Market) {
		return nil, fmt.Errorf("market %q: %w", v.Market, validity.ErrSpelling)
	}
	t, err := r.read(ctx, v, today)
	if err != nil {
		return nil, err
	}
	if err := r.name(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

// read derives v's validity and its gaps over its transaction period.
func (r *Reader) read(ctx context.Context, v types.MarketTicker, today time.Time) (*Page, error) {
	rows, err := validity.ReadRows(ctx, r.q, v)
	if err != nil {
		return nil, err
	}
	probes, err := r.mics.TickerProbes([]types.MarketTicker{v})
	if err != nil {
		return nil, err
	}
	txs, err := r.q.ListTickerTransactions(ctx, probes)
	if err != nil {
		return nil, fmt.Errorf("list transactions of %s: %w", v.Name(), err)
	}
	t := &Page{Ticker: v, Rows: rows, Intervals: validity.Derive(rows.Facts()).Intervals}
	for _, tx := range txs {
		if n := len(t.Days); n == 0 || !t.Days[n-1].Equal(tx.AsAt) {
			t.Days = append(t.Days, tx.AsAt)
		}
	}
	if len(txs) == 0 {
		return t, nil
	}
	period := types.Period{From: txs[0].AsAt, Before: types.Day(today).AddDate(0, 0, 1)}
	if !period.From.Before(period.Before) {
		return t, nil
	}
	for _, p := range validity.Gaps(t.Intervals, period) {
		g := Gap{Period: p}
		keys := map[uuid.UUID]bool{}
		for _, tx := range txs {
			if p.Contains(tx.AsAt) {
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
func (r *Reader) name(ctx context.Context, t *Page) error {
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
