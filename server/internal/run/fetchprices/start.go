package fetchprices

import (
	"context"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run"
)

// ErrEmpty reports that Start found nothing to ask for.
var ErrEmpty = fmt.Errorf("no price is missing: %w", run.ErrEmpty)

// lane serialises fetch prices runs.
const lane = "fetch_prices"

// Start starts a fetch prices run over every gap in the prices of every user
// and in every rate, as a system run, and returns the pending row. It returns
// ErrEmpty where nothing is a gap. The run reads the gaps again when it
// starts.
func (s *Starter) Start(ctx context.Context) (gen.Run, error) {
	gaps, _, err := s.gaps(ctx, nil)
	if err != nil {
		return gen.Run{}, err
	}
	if len(gaps) == 0 {
		return gen.Run{}, ErrEmpty
	}
	spec := run.Spec{Kind: gen.RunKindFetchPrices, Trigger: gen.RunTriggerAdministrator, Lane: lane}
	row, err := s.runs.Start(ctx, spec, func(ctx context.Context, row gen.Run) error {
		return s.fill(ctx, row, nil)
	})
	if err != nil {
		return row, fmt.Errorf("start fetch prices run: %w", err)
	}
	return row, nil
}
