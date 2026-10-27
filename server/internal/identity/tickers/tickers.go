// Package tickers writes and reads the rows of tickers: a MIC_TICKER at its
// market, in the system's spelling; see
// [010_ticker_validity.sql](../../db/migrations/010_ticker_validity.sql). Get
// alone writes the table. Spelled decides the spelling, and every other check
// of a spelling calls it.
package tickers

//go:generate go tool mockgen -source=tickers.go -destination=tickers_mock_test.go -package=tickers -self_package=github.com/leedenison/stonks/server/internal/identity/tickers

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	CreateTickers(ctx context.Context, arg gen.CreateTickersParams) error
	ListTickers(ctx context.Context, arg gen.ListTickersParams) ([]gen.Ticker, error)
	ListTickersByID(ctx context.Context, ids []uuid.UUID) ([]gen.Ticker, error)
}

var _ Queries = (*gen.Queries)(nil)

// ErrSpelling reports a ticker or a market that is not in the system's
// spelling. Nothing matches it, so it is refused.
var ErrSpelling = errors.New("not in the system's spelling")

// SpelledMarket returns ErrSpelling unless m is a market.
func SpelledMarket(mics mic.MICMap, m string) error {
	if !mics.IsMarket(m) {
		return fmt.Errorf("market %q: %w", m, ErrSpelling)
	}
	return nil
}

// Spelled returns ErrSpelling unless t is at a market with its value in the
// system's spelling.
func Spelled(mics mic.MICMap, t types.MarketTicker) error {
	if err := SpelledMarket(mics, t.Market); err != nil {
		return err
	}
	if !market.Canonical(t.Value) {
		return fmt.Errorf("value %q: %w", t.Value, ErrSpelling)
	}
	return nil
}

// Get returns the row of each of ts, creating the rows that do not exist. It
// commits the creation on its own, before the caller's write, so the
// caller's write holds no lock on a ticker row. A ticker row holds only a
// spelling, so the early commit is harmless. Get refuses the whole call with
// ErrSpelling, before it writes, when one of ts is not in the system's
// spelling.
func Get(ctx context.Context, q Queries, mics mic.MICMap, ts []types.MarketTicker) (map[types.MarketTicker]gen.Ticker, error) {
	ts = sorted(ts)
	for _, t := range ts {
		if err := Spelled(mics, t); err != nil {
			return nil, err
		}
	}
	if len(ts) == 0 {
		return map[types.MarketTicker]gen.Ticker{}, nil
	}
	arg := gen.CreateTickersParams{Ids: make([]uuid.UUID, len(ts))}
	for i, t := range ts {
		arg.Ids[i] = db.NewID()
		arg.Markets, arg.Values = append(arg.Markets, t.Market), append(arg.Values, t.Value)
	}
	if err := q.CreateTickers(ctx, arg); err != nil {
		return nil, fmt.Errorf("create tickers: %w", err)
	}
	out, err := Find(ctx, q, ts)
	if err != nil {
		return nil, err
	}
	for _, t := range ts {
		if _, ok := out[t]; !ok {
			return nil, fmt.Errorf("ticker %s has no row after its creation", t.Name())
		}
	}
	return out, nil
}

// Find returns the row of each of ts that has one.
func Find(ctx context.Context, q Queries, ts []types.MarketTicker) (map[types.MarketTicker]gen.Ticker, error) {
	out := map[types.MarketTicker]gen.Ticker{}
	if len(ts) == 0 {
		return out, nil
	}
	var arg gen.ListTickersParams
	for _, t := range ts {
		arg.Markets, arg.Values = append(arg.Markets, t.Market), append(arg.Values, t.Value)
	}
	rows, err := q.ListTickers(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("list tickers: %w", err)
	}
	for _, r := range rows {
		out[Of(r)] = r
	}
	return out, nil
}

// ByID returns the row of each of ids that has one.
func ByID(ctx context.Context, q Queries, ids []uuid.UUID) (map[uuid.UUID]gen.Ticker, error) {
	out := map[uuid.UUID]gen.Ticker{}
	if len(ids) == 0 {
		return out, nil
	}
	rows, err := q.ListTickersByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list tickers by id: %w", err)
	}
	for _, r := range rows {
		out[r.ID] = r
	}
	return out, nil
}

// Of returns the market ticker that r spells.
func Of(r gen.Ticker) types.MarketTicker {
	return types.MarketTicker{Market: r.Market, Value: r.Value}
}

// sorted returns ts once each, by market and then value.
func sorted(ts []types.MarketTicker) []types.MarketTicker {
	out := slices.Clone(ts)
	slices.SortFunc(out, func(a, b types.MarketTicker) int {
		return cmp.Or(cmp.Compare(a.Market, b.Market), cmp.Compare(a.Value, b.Value))
	})
	return slices.Compact(out)
}
