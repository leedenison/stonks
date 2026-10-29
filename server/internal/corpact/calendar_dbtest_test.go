//go:build dbtest

package corpact

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/types"
)

// TestCalendarsHolders checks that an action goes to the instrument that its
// ticker named on the ex-date, read from stored ticker validity.
func TestCalendarsHolders(t *testing.T) {
	s := newStack(t)
	first := s.instr(t, types.AssetClassStock)
	later := s.instr(t, types.AssetClassStock)
	ex := day(2021, 1, 4)
	s.names(t, us("ABC"), first, ex)
	s.names(t, us("ABC"), later, day(2024, 1, 2))
	s.alpha.Responses[ticker("XNAS", "ABC")] = covering(reported(ex, ratio(1, 2)))
	s.fetch(t, ticker("XNAS", "ABC"))

	cals, err := Calendars(context.Background(), s.q, []uuid.UUID{first, later})
	require.NoError(t, err)
	acts := func(id uuid.UUID) []string {
		var out []string
		for _, m := range cals[id] {
			for _, e := range m.Acts {
				out = append(out, m.Market+" "+e.ExDate.Format("2006-01-02")+" "+ratioText(e.Ratio))
			}
		}
		return out
	}
	if diff := cmp.Diff([]string{"US 2021-01-04 2 for 1"}, acts(first)); diff != "" {
		t.Errorf("acts of the first holder mismatch (-want +got):\n%s", diff)
	}
	if got := acts(later); len(got) != 0 {
		t.Errorf("acts of the later holder = %v, want none", got)
	}
}
