package holding

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/types"
)

// dated is what the dated keys' transactions amount to, by key id: the sum
// on each instrument the key's ticker names on their dates, and the sum of
// the transactions dated when the ticker names nothing.
type dated struct {
	onInstr    map[uuid.UUID]map[uuid.UUID]decimal.Decimal
	unresolved map[uuid.UUID]decimal.Decimal
	tickers    map[uuid.UUID]gen.Ticker
}

// dated gives each of the user's dated transactions the instrument its key's
// ticker names on its date. The ticker validity of every dated key's ticker
// is read at once.
func (s *Server) dated(ctx context.Context, user uuid.UUID, keys []gen.ListHoldingKeysRow) (dated, error) {
	d := dated{onInstr: map[uuid.UUID]map[uuid.UUID]decimal.Decimal{}, unresolved: map[uuid.UUID]decimal.Decimal{}}
	rows, err := s.reader.ListDatedTxns(ctx, user)
	if err != nil {
		return d, err
	}
	stated := make([]gen.StatedKey, len(keys))
	for i, k := range keys {
		stated[i] = k.StatedKey
	}
	if d.tickers, err = tickers.OfKeys(ctx, s.reader, stated); err != nil {
		return d, err
	}
	tickerOf := map[uuid.UUID]types.MarketTicker{}
	var ts []types.MarketTicker
	for _, k := range stated {
		if t, _, ok := tickers.Dated(k, d.tickers); ok {
			tickerOf[k.ID] = t
			ts = append(ts, t)
		}
	}
	read, err := tickervalidity.Load(ctx, s.reader, ts)
	if err != nil {
		return d, fmt.Errorf("read the ticker validity of the dated keys' tickers: %w", err)
	}
	for _, r := range rows {
		v, ok := tickerOf[r.StatedKeyID]
		if !ok {
			return d, fmt.Errorf("transaction of key %s: the key is not dated", r.StatedKeyID)
		}
		iv, ok := read[v].On(r.AsAt)
		if !ok {
			d.unresolved[r.StatedKeyID] = d.unresolved[r.StatedKeyID].Add(r.Quantity)
			continue
		}
		on := d.onInstr[r.StatedKeyID]
		if on == nil {
			on = map[uuid.UUID]decimal.Decimal{}
			d.onInstr[r.StatedKeyID] = on
		}
		on[iv.Instr] = on[iv.Instr].Add(r.Quantity)
	}
	return d, nil
}
