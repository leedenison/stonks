package fetchprices

import (
	"context"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// AfterStmt fetches the prices that the user of stmt lacks, and every rate
// that is missing, beneath stmt. The fetch is one system child of stmt,
// because a price belongs to every user. AfterStmt starts a run only where
// some price or rate is a gap. A failure is logged, and the outcome of stmt
// stands.
func (s *Service) AfterStmt(ctx context.Context, stmt gen.Run) {
	gaps, _, err := s.gaps(ctx, stmt.UserID)
	if err != nil {
		s.log.Warn("price fetch failed", "parent", stmt.ID, "err", err)
		return
	}
	if len(gaps) == 0 {
		return
	}
	_, err = s.runs.ChildAs(ctx, stmt, nil, gen.RunKindFetchPrices, func(ctx context.Context, sys gen.Run) error {
		return s.fill(ctx, sys, stmt.UserID)
	})
	if err != nil {
		s.log.Warn("price fetch failed", "parent", stmt.ID, "err", err)
	}
}
