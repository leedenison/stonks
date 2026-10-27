package holding

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/types"
)

// dated is what the dated keys' transactions amount to, by key id: the sum
// on each instrument the key's ticker names on their dates, and the sum of
// the transactions dated when the ticker names nothing.
type dated struct {
	onInstrument map[uuid.UUID]map[uuid.UUID]decimal.Decimal
	unresolved   map[uuid.UUID]decimal.Decimal
}

// dated gives each of the user's dated transactions the instrument its key's
// ticker names on its date. The facts of every dated key's ticker are read
// at once.
func (s *Server) dated(ctx context.Context, user uuid.UUID, keys []gen.ListHoldingKeysRow) (dated, error) {
	d := dated{onInstrument: map[uuid.UUID]map[uuid.UUID]decimal.Decimal{}, unresolved: map[uuid.UUID]decimal.Decimal{}}
	rows, err := s.reader.ListDatedTxns(ctx, user)
	if err != nil {
		return d, err
	}
	tickerOf := map[uuid.UUID]types.MarketTicker{}
	var tickers []types.MarketTicker
	for _, k := range keys {
		if v, ok := to.AssociatedTicker(k.StatedKey); ok {
			if t, ok := s.mics.MarketTicker(v); ok {
				tickerOf[k.StatedKey.ID] = t
				tickers = append(tickers, t)
			}
		}
	}
	read, err := tickervalidity.ReadTickers(ctx, s.reader, tickers)
	if err != nil {
		return d, fmt.Errorf("read the facts of the dated keys' tickers: %w", err)
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
		on := d.onInstrument[r.StatedKeyID]
		if on == nil {
			on = map[uuid.UUID]decimal.Decimal{}
			d.onInstrument[r.StatedKeyID] = on
		}
		on[iv.Instrument] = on[iv.Instrument].Add(r.Quantity)
	}
	return d, nil
}
