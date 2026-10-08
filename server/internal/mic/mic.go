// Package mic normalises a venue to its ISO 10383 operating MIC.
//
// A venue is named by a MIC, which is either an operating MIC or a segment of one. The
// domain of a MIC_TICKER is always the operating MIC, so where one source names a
// segment and another names its operating MIC, they state the same listing.
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

// classSeps separate a ticker's root from its share class, as in BRK.B, BRK/B,
// BRK-B and "BRK B". A MIC_TICKER writes the separator as a dot.
const classSeps = ".-/ "

// WithClassSep writes ticker with its share class separator as sep. It
// reports false, returning ticker, when ticker has more than one separator.
func WithClassSep(ticker string, sep rune) (string, bool) {
	n := 0
	out := strings.Map(func(r rune) rune {
		if strings.ContainsRune(classSeps, r) {
			n++
			return sep
		}
		return r
	}, ticker)
	if n > 1 {
		return ticker, false
	}
	return out, true
}
