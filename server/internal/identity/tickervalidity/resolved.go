package tickervalidity

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// resolvedName is one row of resolved_tickers: the ticker that names Instr at a
// venue, or at its market where Stated.Venue is empty. From and Before bound
// the days it has named Instr without a break. A nil bound is open.
type resolvedName struct {
	Ticker       types.MarketTicker
	Instr        uuid.UUID
	Stated       Stated
	From, Before *time.Time
}

// resolveNames returns the rows that name instr at one market, from ts, the
// tickers of that market whose facts name it. It applies the rule that
// resolved_tickers states in
// [010_ticker_validity.sql](../../db/migrations/010_ticker_validity.sql).
// The day of a ticker's latest winning assertion that names instr breaks a
// tie.
func resolveNames(instr uuid.UUID, ts []*tickerFacts) []resolvedName {
	var best *tickerFacts
	var bestEnd *time.Time
	var bestWon time.Time
	for _, t := range ts {
		ivs := t.Derived.Intervals
		i := lastNaming(ivs, instr)
		if i < 0 {
			continue
		}
		end, won := ivs[i].Before, lastWon(t, instr)
		if best == nil || later(end, bestEnd) > 0 ||
			later(end, bestEnd) == 0 && (won.After(bestWon) || won.Equal(bestWon) && t.Ticker.Value < best.Ticker.Value) {
			best, bestEnd, bestWon = t, end, won
		}
	}
	if best == nil {
		return nil
	}
	ivs := best.Derived.Intervals
	last := lastNaming(ivs, instr)
	first := last
	for first > 0 && ivs[first-1].Instr == instr && ivs[first-1].Before != nil && ivs[first].From != nil &&
		ivs[first-1].Before.Equal(*ivs[first].From) {
		first--
	}
	var out []resolvedName
	for _, s := range stated(best.Facts.Assertions, instr) {
		out = append(out, resolvedName{Ticker: best.Ticker, Instr: instr, Stated: s, From: ivs[first].From, Before: ivs[last].Before})
	}
	return out
}

// lastNaming returns the index of the last of ivs that names instr, or -1.
func lastNaming(ivs []Interval, instr uuid.UUID) int {
	for i := len(ivs) - 1; i >= 0; i-- {
		if ivs[i].Instr == instr {
			return i
		}
	}
	return -1
}

// later compares two ends of intervals, where nil is open and so latest.
func later(a, b *time.Time) int {
	switch {
	case a == nil && b == nil:
		return 0
	case a == nil:
		return 1
	case b == nil:
		return -1
	}
	return a.Compare(*b)
}

// lastWon returns the latest day that an assertion naming instr won for t.
func lastWon(t *tickerFacts, instr uuid.UUID) time.Time {
	ws, _ := winners(current(t.Facts.Assertions))
	for _, w := range slices.Backward(ws) {
		if w.instr != nil && *w.instr == instr {
			return prev(w.before)
		}
	}
	return time.Time{}
}

// stated returns what the positive assertions of instr among as state: one
// entry per venue, with the currency of the latest assertion at the venue
// that states one. Where every assertion omits the venue, it returns one entry
// with an empty venue and the currency of the latest assertion that states
// one. The latest assertion is of the latest answer, then of the latest day.
func stated(as []Assertion, instr uuid.UUID) []Stated {
	var named []Assertion
	for _, a := range as {
		if a.Instr != nil && *a.Instr == instr {
			named = append(named, a)
		}
	}
	slices.SortStableFunc(named, func(a, b Assertion) int {
		switch {
		case a.Source.newer(b.Source):
			return 1
		case b.Source.newer(a.Source):
			return -1
		}
		return a.Period.From.Compare(b.Period.From)
	})
	byVenue := map[string]string{}
	var venues []string
	for _, a := range named {
		if a.Stated.Venue == "" {
			continue
		}
		if _, ok := byVenue[a.Stated.Venue]; !ok {
			venues = append(venues, a.Stated.Venue)
			byVenue[a.Stated.Venue] = ""
		}
		if a.Stated.Currency != "" {
			byVenue[a.Stated.Venue] = a.Stated.Currency
		}
	}
	if len(venues) == 0 {
		var s Stated
		for _, a := range named {
			if a.Stated.Currency != "" {
				s.Currency = a.Stated.Currency
			}
		}
		return []Stated{s}
	}
	slices.Sort(venues)
	out := make([]Stated, len(venues))
	for i, v := range venues {
		out[i] = Stated{Venue: v, Currency: byVenue[v]}
	}
	return out
}

// RebuildQueries is what Rebuild queries.
type RebuildQueries interface {
	FactQueries
	db.AdvisoryLocker
	ListTickersByID(ctx context.Context, ids []uuid.UUID) ([]gen.Ticker, error)
	DeleteTickerValidity(ctx context.Context, tickerIds []uuid.UUID) error
	CreateTickerValidity(ctx context.Context, arg gen.CreateTickerValidityParams) error
	ListResolvePairs(ctx context.Context, tickerIds []uuid.UUID) ([]gen.ListResolvePairsRow, error)
	ListPairTickers(ctx context.Context, arg gen.ListPairTickersParams) ([]gen.ListPairTickersRow, error)
	DeleteResolvedTickers(ctx context.Context, arg gen.DeleteResolvedTickersParams) error
	CreateResolvedTickers(ctx context.Context, arg gen.CreateResolvedTickersParams) error
}

var _ RebuildQueries = (*gen.Queries)(nil)

// Rebuild rewrites the ticker validity of the tickers in ids. It then rewrites
// the rows of resolved_tickers of each instrument and market that the facts
// or the rows of those tickers touch. The caller holds the rows of ids.
// Rebuild locks each of those instruments in id order; see the package doc
// for the order of the locks.
func Rebuild(ctx context.Context, q RebuildQueries, ids []uuid.UUID) error {
	if len(ids) == 0 {
		return nil
	}
	rebuilt, err := q.ListTickersByID(ctx, ids)
	if err != nil {
		return fmt.Errorf("list tickers by id: %w", err)
	}
	pairs, err := q.ListResolvePairs(ctx, ids)
	if err != nil {
		return fmt.Errorf("list resolve pairs: %w", err)
	}
	var asked gen.ListPairTickersParams
	for _, p := range pairs {
		asked.InstrIds, asked.Markets = append(asked.InstrIds, p.InstrID), append(asked.Markets, p.Market)
	}
	if err := db.LockInstrs(ctx, q, asked.InstrIds); err != nil {
		return err
	}
	var rows []gen.ListPairTickersRow
	if len(pairs) > 0 {
		if rows, err = q.ListPairTickers(ctx, asked); err != nil {
			return fmt.Errorf("list pair tickers: %w", err)
		}
	}
	type pair struct {
		instr  uuid.UUID
		market string
	}
	of := map[pair][]types.MarketTicker{}
	idOf := map[types.MarketTicker]uuid.UUID{}
	var all []types.MarketTicker
	for _, r := range rebuilt {
		t := types.MarketTicker{Market: r.Market, Value: r.Value}
		idOf[t] = r.ID
		all = append(all, t)
	}
	for _, r := range rows {
		t := types.MarketTicker{Market: r.Ticker.Market, Value: r.Ticker.Value}
		k := pair{r.InstrID, r.Ticker.Market}
		of[k] = append(of[k], t)
		idOf[t] = r.Ticker.ID
		all = append(all, t)
	}
	read, err := readTickers(ctx, q, all)
	if err != nil {
		return err
	}
	if err := writeValidity(ctx, q, rebuilt, read); err != nil {
		return err
	}
	var out gen.CreateResolvedTickersParams
	for _, p := range pairs {
		var ts []*tickerFacts
		for _, t := range of[pair{p.InstrID, p.Market}] {
			ts = append(ts, read[t])
		}
		slices.SortFunc(ts, func(a, b *tickerFacts) int { return cmp.Compare(a.Ticker.Value, b.Ticker.Value) })
		for _, r := range resolveNames(p.InstrID, ts) {
			out.TickerIds = append(out.TickerIds, idOf[r.Ticker])
			out.Markets = append(out.Markets, r.Ticker.Market)
			out.InstrIds = append(out.InstrIds, r.Instr)
			out.Venues = append(out.Venues, r.Stated.Venue)
			out.Currencies = append(out.Currencies, r.Stated.Currency)
			out.ValidFroms = append(out.ValidFroms, isoDay(r.From))
			out.ValidBefores = append(out.ValidBefores, isoDay(r.Before))
		}
	}
	if len(pairs) == 0 {
		return nil
	}
	if err := q.DeleteResolvedTickers(ctx, gen.DeleteResolvedTickersParams(asked)); err != nil {
		return fmt.Errorf("delete resolved tickers: %w", err)
	}
	if len(out.TickerIds) == 0 {
		return nil
	}
	if err := q.CreateResolvedTickers(ctx, out); err != nil {
		return fmt.Errorf("create resolved tickers: %w", err)
	}
	return nil
}

// writeValidity replaces the stored ticker validity of rebuilt with what its
// facts imply.
func writeValidity(ctx context.Context, q RebuildQueries, rebuilt []gen.Ticker, read map[types.MarketTicker]*tickerFacts) error {
	ids := make([]uuid.UUID, len(rebuilt))
	var arg gen.CreateTickerValidityParams
	for i, r := range rebuilt {
		ids[i] = r.ID
		for _, iv := range read[types.MarketTicker{Market: r.Market, Value: r.Value}].Derived.Intervals {
			arg.TickerIds = append(arg.TickerIds, r.ID)
			arg.Markets = append(arg.Markets, r.Market)
			arg.InstrIds = append(arg.InstrIds, iv.Instr)
			arg.ValidFroms = append(arg.ValidFroms, isoDay(iv.From))
			arg.ValidBefores = append(arg.ValidBefores, isoDay(iv.Before))
			arg.Validities = append(arg.Validities, string(iv.Validity))
		}
	}
	if err := q.DeleteTickerValidity(ctx, ids); err != nil {
		return fmt.Errorf("delete ticker validity: %w", err)
	}
	if len(arg.TickerIds) == 0 {
		return nil
	}
	if err := q.CreateTickerValidity(ctx, arg); err != nil {
		return fmt.Errorf("create ticker validity: %w", err)
	}
	return nil
}

func isoDay(d *time.Time) string {
	if d == nil {
		return ""
	}
	return d.Format(time.DateOnly)
}
