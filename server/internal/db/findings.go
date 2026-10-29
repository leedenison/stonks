package db

import (
	"context"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// StandingFindingCreator writes standing findings.
type StandingFindingCreator interface {
	CreateStandingFinding(ctx context.Context, arg gen.CreateStandingFindingParams) (int64, error)
}

// CreateStandingFinding writes arg and reports whether the row is new. A
// finding that already names the subject of arg keeps its row, cleared or
// not, and the write is skipped. The groups of finding kinds are in
// [006_findings.sql](migrations/006_findings.sql).
func CreateStandingFinding(ctx context.Context, q StandingFindingCreator, arg gen.CreateStandingFindingParams) (bool, error) {
	n, err := q.CreateStandingFinding(ctx, arg)
	return n > 0, err
}
