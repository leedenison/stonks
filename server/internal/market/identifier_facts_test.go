package market

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// facts is an IdentifierFacts integration that answers every request with an
// empty result and counts its calls.
type facts struct {
	fake
	sent []IdentifierFactRequest
}

func (e *facts) Facts() Server[IdentifierFactRequest, IdentifierFactResult] { return e }

func (e *facts) Serves(r IdentifierFactRequest) (types.Identifier, error) { return r.Identifier, nil }

func (e *facts) Fetch(_ context.Context, reqs []Request[IdentifierFactRequest]) ([]Response[IdentifierFactResult], error) {
	e.calls++
	for _, r := range reqs {
		e.sent = append(e.sent, r.Value)
	}
	return make([]Response[IdentifierFactResult], len(reqs)), nil
}

// window is a Windowed whose window starts on one fixed day.
type window time.Time

func (w window) Window(time.Time) time.Time { return time.Time(w) }

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

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

// TestFetchIdentifierFactsPeriod checks that the period is part of the
// request as cached: a second period of one domain calls the provider again,
// and the same period is answered from the cache. No stated key asks for an
// identifier facts fetch. Each key carries its datasource fetch run.
func TestFetchIdentifierFactsPeriod(t *testing.T) {
	e := &facts{}
	h := newHarness(t, e, nil)
	nyse := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS"}
	fetch := func(from, before time.Time) FetchKey[IdentifierFactRequest, IdentifierFactResult] {
		t.Helper()
		got, err := h.fetcher.IdentifierFacts(context.Background(), h.parent, h.entry, []IdentifierFactRequest{{Identifier: nyse, Period: types.Period{From: from, Before: before}}})
		if err != nil {
			t.Fatalf("IdentifierFacts() error = %v", err)
		}
		return got[0]
	}
	fetch(day(2026, 1, 1), day(2026, 2, 1))
	fetch(day(2026, 2, 1), day(2026, 3, 1))
	again := fetch(day(2026, 2, 1), day(2026, 3, 1))
	if e.calls != 2 || !again.cached {
		t.Errorf("calls = %d, cached = %v, want two calls and the repeated period cached", e.calls, again.cached)
	}
	if again.Run.Kind != gen.RunKindDatasourceFetch || again.Run.ID == h.parent.ID {
		t.Errorf("fetch key run = %+v, want the datasource fetch run apart from %s", again.Run, h.parent.ID)
	}
	for _, k := range h.keys {
		if k.StatedKeyID != nil || k.SentValue != nil || k.SentDomain != "XNYS" {
			t.Errorf("fetch key = %+v, want the domain XNYS with no stated key", k)
		}
	}
}
