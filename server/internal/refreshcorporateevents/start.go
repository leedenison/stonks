package refreshcorporateevents

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

// ErrEmpty reports that Start found nothing to ask about.
var ErrEmpty = errors.New("no instrument is held")

// lane serialises refresh corporate events runs.
const lane = "refresh_corporate_events"

// Start starts a refresh corporate events run over the tickers of every held
// instrument, as a system run, and returns the pending row. The requests are
// chosen when the run starts. An instrument that becomes held later waits
// for the next run.
func (s *Starter) Start(ctx context.Context) (gen.Run, error) {
	hs, err := s.heldInstruments(ctx)
	if err != nil {
		return gen.Run{}, err
	}
	asks, err := s.requests(ctx, hs, func(uuid.UUID, *held) bool { return true })
	if err != nil {
		return gen.Run{}, err
	}
	if len(asks) == 0 {
		return gen.Run{}, ErrEmpty
	}
	reqs := make([]market.CorporateEventRequest, len(asks))
	for i, a := range asks {
		reqs[i] = a.req
	}
	spec := run.Spec{Kind: gen.RunKindRefreshCorporateEvents, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	row, err := s.runs.Start(ctx, spec, func(ctx context.Context, row gen.Run) error {
		return s.recorder.Fetch(ctx, row, reqs)
	})
	if err != nil {
		return row, fmt.Errorf("start refresh corporate events run: %w", err)
	}
	return row, nil
}
