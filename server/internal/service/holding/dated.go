package holding

import (
	"context"
	"fmt"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// dated is what the dated keys' transactions amount to, by key id: the sum
// on each instrument the key's ticker names on their dates, and the sum of
// the transactions dated when the ticker names nothing.
type dated struct {
	onInstrument map[uuid.UUID]map[uuid.UUID]decimal.Decimal
	unresolved   map[uuid.UUID]decimal.Decimal
}

// dated gives each of the user's dated transactions the instrument its key's
// ticker names on its date. The facts of a ticker are read once however many
// keys are dated through it.
func (s *Server) dated(ctx context.Context, user uuid.UUID, keys []gen.ListHoldingKeysRow) (dated, error) {
	d := dated{onInstrument: map[uuid.UUID]map[uuid.UUID]decimal.Decimal{}, unresolved: map[uuid.UUID]decimal.Decimal{}}
	rows, err := s.reader.ListDatedTransactions(ctx, user)
	if err != nil {
		return d, err
	}
	tickerOf := map[uuid.UUID]types.Identifier{}
	for _, k := range keys {
		if v, ok := to.AssociatedTicker(k.StatedKey); ok {
			tickerOf[k.StatedKey.ID] = v
		}
	}
	read := map[types.Identifier]*validity.Ticker{}
	for _, r := range rows {
		v, ok := tickerOf[r.StatedKeyID]
		if !ok {
			return d, fmt.Errorf("transaction of key %s: the key is not dated", r.StatedKeyID)
		}
		t := read[v]
		if t == nil {
			if t, err = validity.ReadTicker(ctx, s.reader, v); err != nil {
				return d, fmt.Errorf("read the facts of %s: %w", v.Name(), err)
			}
			read[v] = t
		}
		iv, ok := t.On(r.OrderDate)
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
