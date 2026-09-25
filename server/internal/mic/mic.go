// Package mic normalises a venue to its ISO 10383 operating MIC.
//
// A venue is named by a MIC, which is either an operating MIC or a segment of one. The
// domain of a MIC_TICKER is always the operating MIC, so a source naming a segment and a
// source naming its operating MIC state the same listing.
package mic

import (
	"context"
	"fmt"
	"strings"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Queries is the database access Load needs.
type Queries interface {
	ListMICs(ctx context.Context) ([]gen.Mic, error)
}

// Table maps each MIC to its operating MIC.
type Table map[string]string

// Load reads the MIC reference table.
func Load(ctx context.Context, q Queries) (Table, error) {
	rows, err := q.ListMICs(ctx)
	if err != nil {
		return nil, fmt.Errorf("list mics: %w", err)
	}
	t := make(Table, len(rows))
	for _, r := range rows {
		t[r.Mic] = r.OperatingMic
	}
	return t, nil
}

// Operating returns the operating MIC of m, ignoring case and surrounding space. It
// reports false when m is not a MIC.
func (t Table) Operating(m string) (string, bool) {
	op, ok := t[strings.ToUpper(strings.TrimSpace(m))]
	return op, ok
}
