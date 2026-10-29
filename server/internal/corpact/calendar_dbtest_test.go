//go:build dbtest

package corpact

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
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
		c := cals[id]
		for _, e := range c.Acts {
			out = append(out, c.Market+" "+e.ExDate.Format("2006-01-02")+" "+ratioText(e.Ratio))
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

// TestCalendarsMarket checks that a calendar reads only the answer of the
// source of the market of the instrument's primary venues. An instrument
// whose listings state no primary venue, or primary venues in two markets,
// has no calendar.
func TestCalendarsMarket(t *testing.T) {
	tests := []struct {
		name      string
		primaries map[string]*string
		want      []string
	}{
		{name: "a US primary with a listing in a second market", primaries: map[string]*string{"USD": ptr.To("XNAS"), "CAD": nil}, want: []string{"US"}},
		{name: "no primary venue", primaries: map[string]*string{"USD": nil}},
		{name: "primary venues in two markets", primaries: map[string]*string{"USD": ptr.To("XNAS"), "CAD": ptr.To("XTSE")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			dbtest.CorpActSource(t, s.tx, "XTSE", s.betaName)
			instr := s.listed(t, types.AssetClassStock, tc.primaries)
			ca := types.MarketTicker{Market: "XTSE", Value: "SHOP"}
			s.names(t, us("SHOP"), instr, day(2021, 1, 4))
			s.names(t, ca, instr, day(2021, 1, 4))
			s.alpha.Responses[ticker("XNAS", "SHOP")] = covering()
			s.beta.Responses[ticker("XTSE", "SHOP")] = covering(reported(day(2022, 6, 29), ratio(1, 10)))
			s.fetch(t, ticker("XNAS", "SHOP"), ticker("XTSE", "SHOP"))
			if a, ok := s.read(t, ca); !ok || len(a.Acts) != 1 {
				t.Fatalf("XTSE answer = %+v, %v, want one stored action", a, ok)
			}

			cals, err := Calendars(context.Background(), s.q, []uuid.UUID{instr})
			require.NoError(t, err)
			var got []string
			if c, ok := cals[instr]; ok {
				got = append(got, c.Market)
				if c.Datasource != s.alphaName || len(c.Acts) != 0 {
					t.Errorf("calendar = %+v, want the answer of %s with no acts", c, s.alphaName)
				}
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("markets of the calendar mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
