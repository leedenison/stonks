package price

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
)

// priceKey is the fetch key of one request for prices.
type priceKey = market.FetchKey[market.PriceRequest, market.PriceResult]

// Recorder fetches prices from a datasource and stores what it serves.
type Recorder struct {
	store    Store
	fetcher  Fetcher
	families currency.Families
	scales   currency.Scales
	log      *slog.Logger
}

// New returns a Recorder.
func New(store Store, fetcher Fetcher, families currency.Families, scales currency.Scales, log *slog.Logger) *Recorder {
	return &Recorder{store: store, fetcher: fetcher, families: families, scales: scales, log: log}
}

// Fetch asks e for each period of gaps and stores each served answer. It
// returns how many answers stored coverage. An answer that Record refuses is
// logged and its days stay gaps, as do the days of a request that e does not
// serve or fails. Fetch skips an entry that lacks the price capability.
func (r *Recorder) Fetch(ctx context.Context, parent gen.Run, e *market.Entry, gaps []Gap) (int, error) {
	if _, ok := e.Integration.(market.Prices); !ok {
		return 0, nil
	}
	var reqs []market.PriceRequest
	var listings []uuid.UUID
	for _, g := range gaps {
		for _, p := range g.Periods {
			reqs = append(reqs, market.PriceRequest{Identifier: g.Identifier, Period: p})
			listings = append(listings, g.Listing)
		}
	}
	if len(reqs) == 0 {
		return 0, nil
	}
	keys, err := r.fetcher.Prices(ctx, parent, e, reqs)
	if err != nil {
		return 0, fmt.Errorf("fetch prices from %s: %w", e.Name, err)
	}
	stored := 0
	for i, k := range keys {
		if k.Outcome != gen.FetchOutcomeServed {
			continue
		}
		a := Answer{Listing: listings[i], Datasource: e.Name, FetchKeyID: k.ID, AnsweredAt: k.AnsweredAt, PriceResult: k.Response}
		var wrote bool
		err := r.store.Tx(ctx, func(q RecordQueries) error {
			var err error
			wrote, err = Record(ctx, q, r.families, r.scales, a)
			return err
		})
		switch {
		case errors.Is(err, ErrCurrency), errors.Is(err, ErrInvalid):
			r.log.Warn("prices left out", "identifier", k.Request.Identifier.Name(), "datasource", e.Name, "listing", a.Listing, "reason", err)
		case err != nil:
			return stored, fmt.Errorf("record the prices of %s from %s: %w", k.Request.Identifier.Name(), e.Name, err)
		case wrote:
			stored++
		}
	}
	return stored, nil
}
