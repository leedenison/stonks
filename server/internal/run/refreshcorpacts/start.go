package refreshcorpacts

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

// ErrEmpty reports that Start found nothing to ask about.
var ErrEmpty = errors.New("no instrument is held")

// lane serialises refresh corporate actions runs.
const lane = "refresh_corp_acts"

// Start starts a refresh corporate actions run over the tickers that named a
// held instrument, as a system run, and returns the pending row. The requests
// are chosen when the run starts. An instrument that becomes held later waits
// for the next run.
func (s *Starter) Start(ctx context.Context) (gen.Run, error) {
	ids, err := s.queries.ListHeldInstrIDs(ctx, nil)
	if err != nil {
		return gen.Run{}, fmt.Errorf("list held instruments: %w", err)
	}
	asks, err := s.requests(ctx, ids)
	if err != nil {
		return gen.Run{}, err
	}
	if len(asks) == 0 {
		return gen.Run{}, ErrEmpty
	}
	reqs := make([]market.CorpActRequest, len(asks))
	for i, a := range asks {
		reqs[i] = a.req
	}
	spec := run.Spec{Kind: gen.RunKindRefreshCorpActs, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	row, err := s.runs.Start(ctx, spec, func(ctx context.Context, row gen.Run) error {
		return s.recorder.Fetch(ctx, row, reqs)
	})
	if err != nil {
		return row, fmt.Errorf("start refresh corporate actions run: %w", err)
	}
	return row, nil
}
