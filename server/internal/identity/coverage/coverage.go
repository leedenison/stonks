// Package coverage reads, for the admin area, where each ticker's validity
// is confirmed and where it is not.
//
// Gaps are measured against confirmed ticker validity. An identifier
// attestation closes a gap by asserting its days. A ticker is read at its
// market, and its transactions are those of the keys that state it at any
// venue of the market.
//
// The alerts are narrower than a ticker's page. A gap is an alert only
// while a key stating the ticker holds a position across some of its days.
// A ticker that names nothing, and a gap, each leave out the days an
// administrator attested as naming nothing.
package coverage

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	adminalertv1 "github.com/leedenison/stonks/proto/admin/alert/v1"
	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListIdentifierAssertions(ctx context.Context, arg gen.ListIdentifierAssertionsParams) ([]gen.ListIdentifierAssertionsRow, error)
	ListIdentifierAssertionCoverage(ctx context.Context, arg gen.ListIdentifierAssertionCoverageParams) ([]gen.ListIdentifierAssertionCoverageRow, error)
	ListTickerValidity(ctx context.Context, arg gen.ListTickerValidityParams) ([]gen.ListTickerValidityRow, error)
	ListInstrTickers(ctx context.Context, instrID uuid.UUID) ([]gen.ListInstrTickersRow, error)
	CountTickerTxns(ctx context.Context, probes [][]byte) (int32, error)
	ListTickerTxns(ctx context.Context, probes [][]byte) ([]gen.ListTickerTxnsRow, error)
	ListStatedTickerTxns(ctx context.Context) ([]gen.ListStatedTickerTxnsRow, error)
	ListAttestedNothing(ctx context.Context) ([]gen.ListAttestedNothingRow, error)
	ListInstrNames(ctx context.Context, ids []uuid.UUID) ([]gen.InstrName, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=coverage.go -destination=mock/coverage_mock.go -package=mock

// Reader reads the tickers of an instrument, and the facts and ticker validity
// of a ticker.
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
	Txns int
}

// ToProto converts the gap.
func (g Gap) ToProto() *admincoveragev1.Gap {
	return &admincoveragev1.Gap{Period: g.Period.ToProto(), Keys: int32(g.Keys), Txns: int32(g.Txns)}
}

// Page is one ticker as the admin area shows it: its facts, its ticker
// validity, and its gaps.
type Page struct {
	Ticker types.MarketTicker
	// Days are the distinct as-at dates of the transactions whose keys state
	// the ticker, in order.
	Days      []time.Time
	Rows      tickervalidity.Rows
	Intervals tickervalidity.Intervals
	Gaps      []Gap
	// Names holds the identifier naming each instrument the rows and the
	// intervals refer to.
	Names map[uuid.UUID]types.Identifier
}

// ToProto converts the page.
func (t *Page) ToProto() *admincoveragev1.Ticker {
	out := &admincoveragev1.Ticker{Ticker: t.Ticker.ToProto()}
	for _, d := range t.Days {
		out.TxnDays = append(out.TxnDays, ymd(d))
	}
	for _, p := range t.Rows.AssertionCoverage {
		out.AssertionCoverage = append(out.AssertionCoverage, to.ProtoAssertionCoverage(p))
	}
	for _, a := range t.Rows.Assertions {
		out.Assertions = append(out.Assertions, to.ProtoAssertionItem(a.IdentifierAssertion, a.Value, a.Datasource, a.AttestedBy))
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
		out.Instrs = append(out.Instrs, &admincoveragev1.InstrName{Id: id.String(), Name: t.Names[id].ToProto()})
	}
	return out
}

// InstrTicker is a ticker at its market that names an instrument, now
// or in an assertion, with the count of transactions whose keys state it at
// any venue of the market.
type InstrTicker struct {
	Ticker   types.MarketTicker
	Relation gen.TickerRelation
	Txns     int
	// Venues are the operating MICs at which the ticker names the
	// instrument.
	Venues []string
}

// ToProto converts the ticker.
func (t InstrTicker) ToProto() *admincoveragev1.InstrTicker {
	return &admincoveragev1.InstrTicker{Ticker: t.Ticker.ToProto(), Relation: types.ToProto[typev1.TickerRelation](t.Relation), Txns: int32(t.Txns), Venues: t.Venues}
}

// NamesNothing is a ticker that a key with transactions states, and that
// names nothing on any day.
type NamesNothing struct {
	Ticker types.MarketTicker
	// Keys counts the keys that state the ticker and have a transaction.
	Keys int
	// Txns counts those keys' transactions.
	Txns int
	// First is the earliest as-at date of those transactions.
	First time.Time
}

// ToProto converts the ticker.
func (n NamesNothing) ToProto() *adminalertv1.TickerNamesNothing {
	return &adminalertv1.TickerNamesNothing{Ticker: n.Ticker.ToProto(), Keys: int32(n.Keys), Txns: int32(n.Txns), FirstTxnDay: ymd(n.First)}
}

// TickerGap is one gap of a ticker, with the instrument of the interval
// nearest the gap.
type TickerGap struct {
	Ticker types.MarketTicker
	// Instr is the instrument of the interval nearest the gap; nil for an
	// isolated gap.
	Instr *uuid.UUID
	// Name is the identifier naming the instrument; the zero value where it
	// has none.
	Name types.Identifier
	Gap  Gap
}

// ToProto converts the gap.
func (g TickerGap) ToProto() *adminalertv1.TickerValidityGap {
	out := &adminalertv1.TickerValidityGap{Ticker: g.Ticker.ToProto(), Gap: g.Gap.ToProto()}
	if g.Instr != nil {
		out.Instr = &admincoveragev1.InstrName{Id: g.Instr.String(), Name: g.Name.ToProto()}
	}
	return out
}

// relationOrder ranks the relations as the tickers of an instrument are
// listed: current, then defunct, then former.
var relationOrder = map[gen.TickerRelation]int{gen.TickerRelationCurrent: 0, gen.TickerRelationDefunct: 1, gen.TickerRelationFormer: 2}

// InstrTickers lists the tickers that name the instrument, now or in an
// assertion, the most transacted first.
func (r *Reader) InstrTickers(ctx context.Context, instr uuid.UUID) ([]InstrTicker, error) {
	rows, err := r.q.ListInstrTickers(ctx, instr)
	if err != nil {
		return nil, fmt.Errorf("list tickers of %s: %w", instr, err)
	}
	var out []InstrTicker
	for _, row := range rows {
		v := types.MarketTicker{Market: row.Market, Value: row.Value}
		probes, err := r.mics.TickerProbes([]types.MarketTicker{v})
		if err != nil {
			return nil, err
		}
		n, err := r.q.CountTickerTxns(ctx, probes)
		if err != nil {
			return nil, fmt.Errorf("count transactions of %s: %w", v.Name(), err)
		}
		out = append(out, InstrTicker{Ticker: v, Relation: row.Relation, Txns: int(n), Venues: row.Venues})
	}
	slices.SortStableFunc(out, func(a, b InstrTicker) int {
		if a.Txns != b.Txns {
			return b.Txns - a.Txns
		}
		if a.Relation != b.Relation {
			return relationOrder[a.Relation] - relationOrder[b.Relation]
		}
		return strings.Compare(a.Ticker.Name(), b.Ticker.Name())
	})
	return out, nil
}

// Ticker reads the facts and ticker validity of v. It refuses an unknown market
// with tickers.ErrSpelling.
func (r *Reader) Ticker(ctx context.Context, v types.MarketTicker, today time.Time) (*Page, error) {
	if err := tickers.SpelledMarket(r.mics, v.Market); err != nil {
		return nil, err
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

func (r *Reader) read(ctx context.Context, v types.MarketTicker, today time.Time) (*Page, error) {
	rows, err := tickervalidity.ReadRows(ctx, r.q, v)
	if err != nil {
		return nil, err
	}
	probes, err := r.mics.TickerProbes([]types.MarketTicker{v})
	if err != nil {
		return nil, err
	}
	txns, err := r.q.ListTickerTxns(ctx, probes)
	if err != nil {
		return nil, fmt.Errorf("list transactions of %s: %w", v.Name(), err)
	}
	ivs, err := tickervalidity.Load(ctx, r.q, []types.MarketTicker{v})
	if err != nil {
		return nil, err
	}
	t := &Page{Ticker: v, Rows: rows, Intervals: ivs[v]}
	days := make([]txnDay, 0, len(txns))
	for _, txn := range txns {
		if n := len(t.Days); n == 0 || !t.Days[n-1].Equal(txn.AsAt) {
			t.Days = append(t.Days, txn.AsAt)
		}
		days = append(days, txnDay{Key: txn.StatedKeyID, AsAt: txn.AsAt, Txns: 1})
	}
	t.Gaps = gaps(t.Intervals, nil, days, today)
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
		if a.IdentifierAssertion.InstrID != nil {
			add(*a.IdentifierAssertion.InstrID)
		}
	}
	for _, iv := range t.Intervals {
		add(iv.Instr)
	}
	names, err := r.names(ctx, ids)
	if err != nil {
		return err
	}
	if len(names) > 0 {
		t.Names = names
	}
	return nil
}

// names reads the identifier naming each of the instruments ids.
func (r *Reader) names(ctx context.Context, ids []uuid.UUID) (map[uuid.UUID]types.Identifier, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	rows, err := r.q.ListInstrNames(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list instrument names: %w", err)
	}
	out := make(map[uuid.UUID]types.Identifier, len(rows))
	for _, n := range rows {
		out[n.InstrID] = to.InstrName(n)
	}
	return out, nil
}

// Unconfirmed reads the alerts of every ticker that a key with transactions
// states, in ticker order: the tickers that name nothing on any day, and
// the gaps of the tickers that name an instrument.
func (r *Reader) Unconfirmed(ctx context.Context, today time.Time) ([]NamesNothing, []TickerGap, error) {
	rows, err := r.q.ListStatedTickerTxns(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list the transactions of every stated ticker: %w", err)
	}
	var order []types.MarketTicker
	days := map[types.MarketTicker][]txnDay{}
	for _, row := range rows {
		v := types.MarketTicker{Market: row.Market, Value: row.Value}
		if _, seen := days[v]; !seen {
			order = append(order, v)
		}
		days[v] = append(days[v], txnDay{Key: row.StatedKeyID, AsAt: row.AsAt, Txns: int(row.Txns), Quantity: row.Quantity})
	}
	if len(order) == 0 {
		return nil, nil, nil
	}
	settled, err := r.attestedNothing(ctx)
	if err != nil {
		return nil, nil, err
	}
	ivs, err := tickervalidity.Load(ctx, r.q, order)
	if err != nil {
		return nil, nil, err
	}
	var nothing []NamesNothing
	var gapped []TickerGap
	for _, v := range order {
		ds := days[v]
		if !ivs[v].Any() {
			if n, ok := namesNothing(v, ds, settled[v]); ok {
				nothing = append(nothing, n)
			}
			continue
		}
		held := holdings(ds, today)
		for _, g := range gaps(ivs[v], settled[v], ds, today) {
			if !slices.ContainsFunc(held, func(p types.Period) bool { _, ok := p.Clip(g.Period); return ok }) {
				continue
			}
			gapped = append(gapped, TickerGap{Ticker: v, Instr: gapInstr(ivs[v], g.Period), Gap: g})
		}
	}
	if err := r.nameGaps(ctx, gapped); err != nil {
		return nil, nil, err
	}
	return nothing, gapped, nil
}

// attestedNothing reads, by ticker, the days an administrator attested as
// naming nothing.
func (r *Reader) attestedNothing(ctx context.Context) (map[types.MarketTicker][]types.Period, error) {
	rows, err := r.q.ListAttestedNothing(ctx)
	if err != nil {
		return nil, fmt.Errorf("list the attestations of nothing: %w", err)
	}
	out := map[types.MarketTicker][]types.Period{}
	for _, row := range rows {
		v := types.MarketTicker{Market: row.Market, Value: row.Value}
		out[v] = append(out[v], types.Period{From: row.AssertedFrom, Before: row.AssertedBefore})
	}
	return out, nil
}

// namesNothing counts the keys and transactions of a ticker without an
// interval, leaving out the days an administrator attested as naming
// nothing. It reports false when none remain.
func namesNothing(v types.MarketTicker, ds []txnDay, settled []types.Period) (NamesNothing, bool) {
	n := NamesNothing{Ticker: v}
	keys := map[uuid.UUID]bool{}
	for _, d := range ds {
		if slices.ContainsFunc(settled, func(p types.Period) bool { return p.Contains(d.AsAt) }) {
			continue
		}
		if n.Txns == 0 {
			n.First = d.AsAt
		}
		n.Txns += d.Txns
		keys[d.Key] = true
	}
	n.Keys = len(keys)
	return n, n.Txns > 0
}

// holdings returns the days over which each key of ds held a position: from
// its first transaction through today, or through its last transaction when
// the quantities it states sum to zero by then. The sum is of the stated
// quantities, so a position bought before a split and sold after it reads
// as held.
func holdings(ds []txnDay, today time.Time) []types.Period {
	var out []types.Period
	open := map[uuid.UUID]int{}
	sums := map[uuid.UUID]decimal.Decimal{}
	for _, d := range ds {
		if _, ok := open[d.Key]; !ok {
			open[d.Key] = len(out)
			out = append(out, types.Period{From: d.AsAt})
		}
		sums[d.Key] = sums[d.Key].Add(d.Quantity)
		out[open[d.Key]].Before = d.AsAt.AddDate(0, 0, 1)
	}
	for key, i := range open {
		if !sums[key].IsZero() {
			out[i].Before = types.Day(today).AddDate(0, 0, 1)
		}
	}
	return out
}

// gapInstr returns the instrument of the interval nearest the gap g, or nil
// for an isolated gap.
func gapInstr(ivs tickervalidity.Intervals, g types.Period) *uuid.UUID {
	if iv, ok := ivs.On(g.From); ok {
		return &iv.Instr
	}
	for _, iv := range ivs {
		if iv.Before != nil && iv.Before.Equal(g.From) {
			return &iv.Instr
		}
	}
	for _, iv := range ivs {
		if iv.From != nil && iv.From.Equal(g.Before) {
			return &iv.Instr
		}
	}
	return nil
}

// nameGaps sets the Name of each gap to its instrument's name.
func (r *Reader) nameGaps(ctx context.Context, gapped []TickerGap) error {
	var ids []uuid.UUID
	seen := map[uuid.UUID]bool{}
	for _, g := range gapped {
		if g.Instr != nil && !seen[*g.Instr] {
			seen[*g.Instr] = true
			ids = append(ids, *g.Instr)
		}
	}
	names, err := r.names(ctx, ids)
	if err != nil {
		return err
	}
	for i := range gapped {
		if gapped[i].Instr != nil {
			gapped[i].Name = names[*gapped[i].Instr]
		}
	}
	return nil
}

// txnDay is the transactions of one key on one as-at day.
type txnDay struct {
	Key      uuid.UUID
	AsAt     time.Time
	Txns     int
	Quantity decimal.Decimal
}

// gaps returns the parts of a ticker's transaction period that neither a
// confirmed interval of ivs nor a settled period holds, each with its keys
// and transactions. days are the ticker's transactions in order of day, and
// the period runs from the first up to and including today.
func gaps(ivs tickervalidity.Intervals, settled []types.Period, days []txnDay, today time.Time) []Gap {
	if len(days) == 0 {
		return nil
	}
	period := types.Period{From: days[0].AsAt, Before: types.Day(today).AddDate(0, 0, 1)}
	if !period.From.Before(period.Before) {
		return nil
	}
	covered := slices.Clone(settled)
	for _, iv := range ivs {
		if c, ok := iv.Clip(period); ok && iv.Validity == tickervalidity.Confirmed {
			covered = append(covered, c)
		}
	}
	var out []Gap
	for _, p := range types.Gaps(covered, period) {
		g := Gap{Period: p}
		keys := map[uuid.UUID]bool{}
		for _, d := range days {
			if p.Contains(d.AsAt) {
				g.Txns += d.Txns
				keys[d.Key] = true
			}
		}
		g.Keys = len(keys)
		out = append(out, g)
	}
	return out
}

func interval(iv tickervalidity.Interval) *admincoveragev1.Interval {
	return &admincoveragev1.Interval{
		Period:   types.ProtoPeriod(iv.From, iv.Before),
		InstrId:  iv.Instr.String(),
		Validity: types.ToProto[admincoveragev1.TickerValidity](iv.Validity),
	}
}

func ymd(d time.Time) string { return d.Format(time.DateOnly) }
