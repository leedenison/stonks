package market

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// events is an IdentifierEvents integration that answers every request with an
// empty result and counts its calls.
type events struct {
	fake
	lag  time.Duration
	sent []IdentifierEventRequest
}

func (e *events) Events() Server[IdentifierEventRequest, IdentifierEventResult] { return e }

func (e *events) Serves(r IdentifierEventRequest) (types.Identifier, error) { return r.Identifier, nil }

func (e *events) Fetch(_ context.Context, reqs []Request[IdentifierEventRequest]) ([]Response[IdentifierEventResult], error) {
	e.calls++
	for _, r := range reqs {
		e.sent = append(e.sent, r.Value)
	}
	return make([]Response[IdentifierEventResult], len(reqs)), nil
}

// rangedEvents is an events integration that answers for whole markets.
type rangedEvents struct{ events }

func (*rangedEvents) Markets() []string          { return []string{"US"} }
func (*rangedEvents) Window(time.Time) time.Time { return time.Time{} }
func (r *rangedEvents) Lag() time.Duration       { return r.lag }

// window is a Windowed whose window starts on one fixed day.
type window time.Time

func (w window) Window(time.Time) time.Time { return time.Time(w) }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

func TestNextPeriod(t *testing.T) {
	now := time.Date(2026, time.March, 20, 15, 0, 0, 0, time.UTC)
	before := day(2026, time.March, 21)
	lag := 14 * 24 * time.Hour
	tests := []struct {
		name     string
		last     *time.Time
		earliest time.Time
		window   time.Time
		want     types.Period
	}{
		{name: "the first fetch starts at the earliest transaction", earliest: day(2024, 1, 2), window: day(2022, 7, 22), want: types.Period{From: day(2024, 1, 2), Before: before}},
		{name: "a later fetch repeats the lag", last: ptr.To(day(2026, 3, 10)), earliest: day(2024, 1, 2), want: types.Period{From: day(2026, 2, 24), Before: before}},
		{name: "the window bounds the start", earliest: day(2020, 1, 2), window: day(2022, 7, 22), want: types.Period{From: day(2022, 7, 22), Before: before}},
		{name: "a start after today is empty", earliest: day(2026, 6, 1), want: types.Period{From: before, Before: before}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got := NextPeriod(tc.last, tc.earliest, window(tc.window), lag, now)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("NextPeriod mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestLookupDay(t *testing.T) {
	now := time.Date(2026, time.March, 20, 15, 0, 0, 0, time.UTC)
	tests := []struct {
		name             string
		earliest, window time.Time
		want             time.Time
	}{
		{name: "the earliest transaction inside the window", earliest: day(2025, 1, 2), window: day(2024, 3, 20), want: day(2025, 1, 2)},
		{name: "the window's start after the earliest transaction", earliest: day(2020, 1, 2), window: day(2024, 3, 20), want: day(2024, 3, 20)},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := LookupDay(tc.earliest, window(tc.window), now); !got.Equal(tc.want) {
				t.Errorf("LookupDay = %s, want %s", got, tc.want)
			}
		})
	}
}

func TestHistory(t *testing.T) {
	now := time.Date(2026, time.March, 20, 15, 0, 0, 0, time.UTC)
	want := types.Period{From: day(2024, 3, 20), Before: day(2026, 3, 21)}
	if diff := cmp.Diff(want, History(window(day(2024, 3, 20)), now)); diff != "" {
		t.Errorf("History mismatch (-want +got):\n%s", diff)
	}
}

func TestCanonical(t *testing.T) {
	for ticker, want := range map[string]bool{
		"BRK.B": true, "BRK/B": false, "BRK-B": false, "BRK B": false, "ABRpD": true, "A.B.C": false, "AAPL": true,
	} {
		if got := Canonical(ticker); got != want {
			t.Errorf("Canonical(%q) = %v, want %v", ticker, got, want)
		}
	}
}

// TestLagOf checks that a datasource's own lag wins over its integration's
// default, which only a Ranged integration has.
func TestLagOf(t *testing.T) {
	ranged := &rangedEvents{events: events{lag: 14 * 24 * time.Hour}}
	tests := []struct {
		name string
		row  gen.Datasource
		i    Integration
		want time.Duration
	}{
		{name: "the integration's default", i: ranged, want: 14 * 24 * time.Hour},
		{name: "the row's setting", row: gen.Datasource{LagDays: ptr.To(int32(3))}, i: ranged, want: 3 * 24 * time.Hour},
		{name: "an integration that is not ranged", i: &events{}, want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := lagOf(tc.row, tc.i); got != tc.want {
				t.Errorf("lagOf = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestFetchIdentifierEventsPeriod checks that the period is part of the
// request as cached: a second period of one domain calls the provider again,
// and the same period is answered from the cache. No stated key asks for an
// identifier event fetch. Each key carries its fetch run.
func TestFetchIdentifierEventsPeriod(t *testing.T) {
	e := &events{}
	h := newHarness(t, e, nil)
	nyse := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS"}
	fetch := func(from, before time.Time) FetchKey[IdentifierEventRequest, IdentifierEventResult] {
		t.Helper()
		got, err := h.fetcher.IdentifierEvents(context.Background(), h.parent, h.entry, []IdentifierEventRequest{{Identifier: nyse, Period: types.Period{From: from, Before: before}}})
		if err != nil {
			t.Fatalf("IdentifierEvents() error = %v", err)
		}
		return got[0]
	}
	fetch(day(2026, 1, 1), day(2026, 2, 1))
	fetch(day(2026, 2, 1), day(2026, 3, 1))
	again := fetch(day(2026, 2, 1), day(2026, 3, 1))
	if e.calls != 2 || !again.cached {
		t.Errorf("calls = %d, cached = %v, want two calls and the repeated period cached", e.calls, again.cached)
	}
	if again.Run.Kind != gen.RunKindFetch || again.Run.ID == h.parent.ID {
		t.Errorf("fetch key run = %+v, want the fetch run apart from %s", again.Run, h.parent.ID)
	}
	for _, k := range h.keys {
		if k.StatedKeyID != nil || k.SentValue != nil || k.SentDomain != "XNYS" {
			t.Errorf("fetch key = %+v, want the domain XNYS with no stated key", k)
		}
	}
}
