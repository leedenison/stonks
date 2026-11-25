//go:build dbtest

package price

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/types"
)

// read returns the points of listing over p.
func (s *stack) read(t *testing.T, listing uuid.UUID, p types.Period) []point {
	t.Helper()
	got, err := Read(context.Background(), s.q, []uuid.UUID{listing}, p, today)
	require.NoError(t, err)
	return points(got[listing])
}

// TestReadPicksByPrecedence checks that a day two datasources priced reads
// from the one that ranks first, and that the pick changes when only the
// precedence is updated.
func TestReadPicksByPrecedence(t *testing.T) {
	s := newStack(t)
	_, listings := s.instr(t, types.AssetClassMutualFund, "USD")
	l := listings["USD"]
	week := span(day(2026, 3, 2), day(2026, 3, 7))
	s.record(t, s.answer(t, s.alpha, l, "USD", week, at(day(2026, 3, 2), "10"), at(day(2026, 3, 3), "11")))
	s.record(t, s.answer(t, s.beta, l, "USD", week, at(day(2026, 3, 3), "11.5"), at(day(2026, 3, 4), "12.5")))
	p := span(day(2026, 3, 2), day(2026, 3, 5))

	want := []point{
		{Day: "2026-03-02", Price: "10", Datasource: s.alpha}, {Day: "2026-03-03", Price: "11", Datasource: s.alpha},
		{Day: "2026-03-04", Price: "12.5", Datasource: s.beta},
	}
	if diff := cmp.Diff(want, s.read(t, l, p)); diff != "" {
		t.Errorf("Read() mismatch (-want +got):\n%s", diff)
	}
	_, err := s.tx.Exec(context.Background(), "UPDATE datasources SET precedence = 5 WHERE name = $1", s.beta)
	require.NoError(t, err)
	want[1] = point{Day: "2026-03-03", Price: "11.5", Datasource: s.beta}
	if diff := cmp.Diff(want, s.read(t, l, p)); diff != "" {
		t.Errorf("Read() after beta ranks first mismatch (-want +got):\n%s", diff)
	}
}

// TestReadCarriesAcrossDatasources checks the carry over stored rows. Alpha
// covers through Friday 6 March 2026 and beta covers the Saturday and Sunday,
// so the two periods meet and the weekend takes the Thursday close of beta. A
// day takes the latest earlier close whatever datasource served it. A period
// that starts on the Saturday still reaches back for that close.
func TestReadCarriesAcrossDatasources(t *testing.T) {
	s := newStack(t)
	_, listings := s.instr(t, types.AssetClassMutualFund, "USD")
	l := listings["USD"]
	s.record(t, s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 2), day(2026, 3, 7)), at(day(2026, 3, 4), "10")))
	s.record(t, s.answer(t, s.beta, l, "USD", span(day(2026, 3, 2), day(2026, 3, 6)), at(day(2026, 3, 5), "11")))
	s.record(t, s.answer(t, s.beta, l, "USD", span(day(2026, 3, 7), day(2026, 3, 9))))

	want := []point{
		{Day: "2026-03-04", Price: "10", Datasource: s.alpha}, {Day: "2026-03-05", Price: "11", Datasource: s.beta},
		{Day: "2026-03-06", Price: "11", Datasource: s.beta}, {Day: "2026-03-07", Price: "11", Datasource: s.beta},
		{Day: "2026-03-08", Price: "11", Datasource: s.beta},
	}
	if diff := cmp.Diff(want, s.read(t, l, span(day(2026, 3, 2), day(2026, 3, 10)))); diff != "" {
		t.Errorf("Read() mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff(want[3:], s.read(t, l, span(day(2026, 3, 7), day(2026, 3, 10)))); diff != "" {
		t.Errorf("Read() from the Saturday mismatch (-want +got):\n%s", diff)
	}
}

// TestReadStopsAtABreak checks that a day reads as unknown where the
// coverage back to the latest earlier close is broken.
func TestReadStopsAtABreak(t *testing.T) {
	s := newStack(t)
	_, listings := s.instr(t, types.AssetClassMutualFund, "USD")
	l := listings["USD"]
	s.record(t, s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 2), day(2026, 3, 5)), at(day(2026, 3, 3), "10")))
	s.record(t, s.answer(t, s.alpha, l, "USD", span(day(2026, 3, 6), day(2026, 3, 9))))
	want := []point{{Day: "2026-03-03", Price: "10", Datasource: s.alpha}, {Day: "2026-03-04", Price: "10", Datasource: s.alpha}}
	if diff := cmp.Diff(want, s.read(t, l, span(day(2026, 3, 2), day(2026, 3, 9)))); diff != "" {
		t.Errorf("Read() mismatch (-want +got):\n%s", diff)
	}
}
