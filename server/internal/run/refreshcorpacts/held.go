package refreshcorpacts

import (
	"cmp"
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/corpact"
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

// heldInstrs returns each of instrs that is held, by its id. A transaction of
// a dated key on a day when its ticker names nothing holds nothing.
func (b *base) heldInstrs(ctx context.Context, instrs []uuid.UUID) (map[uuid.UUID]*held, error) {
	out := map[uuid.UUID]*held{}
	if len(instrs) == 0 {
		return out, nil
	}
	rows, err := b.queries.ListHeldInstrs(ctx, instrs)
	if err != nil {
		return nil, fmt.Errorf("list held instruments: %w", err)
	}
	for _, r := range rows {
		h := out[r.InstrID]
		if h == nil {
			h = &held{from: r.FirstAsAt}
			out[r.InstrID] = h
		}
		if r.FirstAsAt.Before(h.from) {
			h.from = r.FirstAsAt
		}
		if !slices.Contains(h.users, r.UserID) {
			h.users = append(h.users, r.UserID)
		}
	}
	return out, nil
}

// requests returns an ask for each ticker that ticker validity says named a
// held instrument in kept over its holding, at the instrument's market. The
// period and the users of an ask come from every held instrument at the
// ticker's market that the ticker named, kept or not, so that every run asks
// for the same period.
func (b *base) requests(ctx context.Context, kept []uuid.UUID) ([]ask, error) {
	if len(kept) == 0 {
		return nil, nil
	}
	read, err := tickervalidity.LoadNaming(ctx, b.queries, kept)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the held instruments' tickers: %w", err)
	}
	ts := slices.SortedFunc(maps.Keys(read), func(a, b types.MarketTicker) int {
		return cmp.Or(strings.Compare(a.Market, b.Market), strings.Compare(a.Value, b.Value))
	})
	instrs := map[uuid.UUID]bool{}
	for _, ivs := range read {
		for _, iv := range ivs {
			instrs[iv.Instr] = true
		}
	}
	hs, err := b.heldInstrs(ctx, slices.Collect(maps.Keys(instrs)))
	if err != nil {
		return nil, err
	}
	markets, err := corpact.Markets(ctx, b.queries, slices.Collect(maps.Keys(hs)))
	if err != nil {
		return nil, err
	}
	isKept := map[uuid.UUID]bool{}
	for _, id := range kept {
		isKept[id] = true
	}
	before := types.Day(b.clock()).AddDate(0, 0, 1)
	var out []ask
	for _, t := range ts {
		var a ask
		named, asked := false, false
		for _, iv := range read[t] {
			h := hs[iv.Instr]
			if h == nil || markets[iv.Instr] != t.Market || !overlaps(iv, h.from, before) {
				continue
			}
			asked = asked || isKept[iv.Instr]
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
