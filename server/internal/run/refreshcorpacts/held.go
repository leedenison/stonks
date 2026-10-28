package refreshcorpacts

import (
	"bytes"
	"context"
	"fmt"
	"maps"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// held is one held instrument.
type held struct {
	// from is the earliest as-at date of the transactions of every user that
	// name the instrument.
	from  time.Time
	users []uuid.UUID
}

// ask is the request for one ticker, with the users of the held instruments
// it named.
type ask struct {
	req   market.CorpActRequest
	users []uuid.UUID
}

// heldInstrs returns every held instrument by its id. A transaction of
// a dated key on a day when its ticker names nothing holds nothing.
func (b *base) heldInstrs(ctx context.Context) (map[uuid.UUID]*held, error) {
	out := map[uuid.UUID]*held{}
	add := func(instr, user uuid.UUID, d time.Time) {
		h := out[instr]
		if h == nil {
			h = &held{from: d}
			out[instr] = h
		}
		if d.Before(h.from) {
			h.from = d
		}
		if !slices.Contains(h.users, user) {
			h.users = append(h.users, user)
		}
	}
	fixed, err := b.queries.ListHeldInstrs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list held instruments: %w", err)
	}
	for _, r := range fixed {
		add(r.InstrID, r.UserID, r.FirstAsAt)
	}
	dated, err := b.queries.ListHeldTickerDates(ctx)
	if err != nil {
		return nil, fmt.Errorf("list held ticker dates: %w", err)
	}
	tickerOf := make([]types.MarketTicker, len(dated))
	var ts []types.MarketTicker
	for i, r := range dated {
		t := types.MarketTicker{Market: r.Market, Value: r.Value}
		tickerOf[i] = t
		if !slices.Contains(ts, t) {
			ts = append(ts, t)
		}
	}
	read, err := tickervalidity.Load(ctx, b.queries, ts)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the held tickers: %w", err)
	}
	for i, r := range dated {
		if iv, ok := read[tickerOf[i]].On(r.AsAt); ok {
			add(iv.Instr, r.UserID, r.AsAt)
		}
	}
	return out, nil
}

// requests returns an ask for each ticker that ticker validity says named a
// kept instrument of hs over its holding. The period and the users of an ask
// come from every instrument of hs that the ticker named, kept or not, so that
// every run asks for the same period.
func (b *base) requests(ctx context.Context, hs map[uuid.UUID]*held, keep func(uuid.UUID, *held) bool) ([]ask, error) {
	var kept []uuid.UUID
	for _, id := range slices.SortedFunc(maps.Keys(hs), func(a, b uuid.UUID) int { return bytes.Compare(a[:], b[:]) }) {
		if keep(id, hs[id]) {
			kept = append(kept, id)
		}
	}
	if len(kept) == 0 {
		return nil, nil
	}
	rows, err := b.queries.ListTickersNaming(ctx, kept)
	if err != nil {
		return nil, fmt.Errorf("list the tickers of the held instruments: %w", err)
	}
	var ts []types.MarketTicker
	for _, r := range rows {
		t := types.MarketTicker{Market: r.Market, Value: r.Value}
		if !slices.Contains(ts, t) {
			ts = append(ts, t)
		}
	}
	read, err := tickervalidity.Load(ctx, b.queries, ts)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the held instruments' tickers: %w", err)
	}
	before := types.Day(b.clock()).AddDate(0, 0, 1)
	var out []ask
	for _, t := range ts {
		var a ask
		named, asked := false, false
		for _, iv := range read[t] {
			h := hs[iv.Instr]
			if h == nil || !overlaps(iv, h.from, before) {
				continue
			}
			asked = asked || slices.Contains(kept, iv.Instr)
			if !named || h.from.Before(a.req.Period.From) {
				named = true
				a.req.Period.From = h.from
			}
			for _, u := range h.users {
				if !slices.Contains(a.users, u) {
					a.users = append(a.users, u)
				}
			}
		}
		if !asked {
			continue
		}
		venue, ok := b.venue(t.Market)
		if !ok {
			b.log.Warn("ticker left out of the corporate actions fetch: its market has no venue", "ticker", t.Name())
			continue
		}
		a.req.Ticker = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: t.Value}
		a.req.Period.Before = before
		out = append(out, a)
	}
	return out, nil
}

// overlaps reports whether iv covers any day on or after from and before
// before.
func overlaps(iv tickervalidity.Interval, from, before time.Time) bool {
	return (iv.Before == nil || iv.Before.After(from)) && (iv.From == nil || iv.From.Before(before))
}

// venue returns the first operating MIC of market. A request names a venue,
// while a ticker's facts name its market. A datasource answers a ticker alike
// at every operating MIC of its market.
func (b *base) venue(market string) (string, bool) {
	for _, v := range b.mics.Venues(market) {
		if op, _ := b.mics.Operating(v); op == v {
			return v, true
		}
	}
	return "", false
}
