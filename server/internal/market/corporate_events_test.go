package market

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// corporate is a CorporateEvents integration that answers each ticker from
// answers, fails it with errs, and declines a ticker in neither.
type corporate struct {
	fake
	answers map[string]CorporateEventResult
	errs    map[string]error
}

func (c *corporate) CorporateEvents() Server[CorporateEventRequest, CorporateEventResult] { return c }

func (c *corporate) Serves(r CorporateEventRequest) (types.Identifier, error) {
	if _, ok := c.answers[r.Ticker.Value]; ok {
		return r.Ticker, nil
	}
	if _, ok := c.errs[r.Ticker.Value]; ok {
		return r.Ticker, nil
	}
	return types.Identifier{}, errors.New("serves no such ticker")
}

func (c *corporate) Fetch(_ context.Context, reqs []Request[CorporateEventRequest]) ([]Response[CorporateEventResult], error) {
	c.calls++
	out := make([]Response[CorporateEventResult], len(reqs))
	for i, r := range reqs {
		if err, ok := c.errs[r.Sent.Value]; ok {
			out[i].Err = err
			continue
		}
		out[i].Value = c.answers[r.Sent.Value]
	}
	return out, nil
}

func corporateRequest(value string, period types.Period) CorporateEventRequest {
	return CorporateEventRequest{Ticker: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: value}, Period: period}
}

var split = CorporateEventResult{
	Events: []CorporateEvent{{
		ExDate: day(2024, 6, 10), Ratio: types.Ratio{From: decimal.NewFromInt(1), To: decimal.RequireFromString("10.5")},
		Type: "forward_split", Handled: true,
	}},
	Coverage: &types.Period{From: day(2024, 1, 1), Before: day(2026, 12, 31)},
}

// TestFetchCorporateEvents checks the outcome of a served, a declined and a
// failed request. No stated key asks for a corporate event fetch, and a
// served key counts its events.
func TestFetchCorporateEvents(t *testing.T) {
	c := &corporate{fake: fake{batch: 10}, answers: map[string]CorporateEventResult{"NVDA": split}, errs: map[string]error{"GONE": permanent("unknown ticker")}}
	h := newHarness(t, c, nil)
	period := types.Period{From: day(2024, 1, 1), Before: day(2026, 12, 31)}
	got, err := h.fetcher.CorporateEvents(context.Background(), h.parent, h.entry, []CorporateEventRequest{
		corporateRequest("NVDA", period), corporateRequest("ODD", period), corporateRequest("GONE", period),
	})
	if err != nil {
		t.Fatalf("CorporateEvents() error = %v", err)
	}
	want := []gen.FetchOutcome{gen.FetchOutcomeServed, gen.FetchOutcomeNotServed, gen.FetchOutcomeFailedPermanent}
	for i, k := range got {
		if k.Outcome != want[i] {
			t.Errorf("key %d outcome = %s, want %s", i, k.Outcome, want[i])
		}
	}
	if diff := cmp.Diff(split, got[0].Response); diff != "" {
		t.Errorf("served response mismatch (-want +got):\n%s", diff)
	}
	if len(h.keys) != 3 || h.keys[0].Candidates != 1 {
		t.Errorf("fetch keys = %+v, want three with the served one counting one event", h.keys)
	}
	for _, k := range h.keys {
		if k.StatedKeyID != nil {
			t.Errorf("fetch key = %+v, want no stated key", k)
		}
	}
	if len(h.blocks) != 1 || h.blocks[0].SentValue == nil || *h.blocks[0].SentValue != "GONE" {
		t.Errorf("blocks = %+v, want one on the failed ticker", h.blocks)
	}
}

// TestFetchCorporateEventsCached checks that a cached answer survives the
// cache whole, with its exact ratio, its coverage and the moment of the call
// that filled it, and that another period calls the provider again.
func TestFetchCorporateEventsCached(t *testing.T) {
	c := &corporate{fake: fake{batch: 10}, answers: map[string]CorporateEventResult{"NVDA": split}}
	h := newHarness(t, c, nil)
	fetch := func(period types.Period) FetchKey[CorporateEventRequest, CorporateEventResult] {
		t.Helper()
		got, err := h.fetcher.CorporateEvents(context.Background(), h.parent, h.entry, []CorporateEventRequest{corporateRequest("NVDA", period)})
		if err != nil {
			t.Fatalf("CorporateEvents() error = %v", err)
		}
		return got[0]
	}
	period := types.Period{From: day(2024, 1, 1), Before: day(2026, 12, 31)}
	fetch(period)
	h.now = start.Add(time.Hour)
	hit := fetch(period)
	if !hit.cached || c.calls != 1 {
		t.Fatalf("cached = %v after %d calls, want the repeated period cached", hit.cached, c.calls)
	}
	if diff := cmp.Diff(split, hit.Response); diff != "" {
		t.Errorf("cached response mismatch (-want +got):\n%s", diff)
	}
	if !hit.AnsweredAt.Equal(start) {
		t.Errorf("hit answered at %v, want the moment of the call that filled the cache %v", hit.AnsweredAt, start)
	}
	fetch(types.Period{From: day(2025, 1, 1), Before: day(2026, 12, 31)})
	if c.calls != 2 {
		t.Errorf("calls = %d, want another period to call again", c.calls)
	}
}

// TestFetchCorporateEventsNotServed checks that an integration without the
// capability serves no corporate events.
func TestFetchCorporateEventsNotServed(t *testing.T) {
	h := newHarness(t, &events{}, nil)
	got, err := h.fetcher.CorporateEvents(context.Background(), h.parent, h.entry, []CorporateEventRequest{corporateRequest("NVDA", types.Period{From: day(2024, 1, 1), Before: day(2025, 1, 1)})})
	if err != nil {
		t.Fatalf("CorporateEvents() error = %v", err)
	}
	if got[0].Outcome != gen.FetchOutcomeNotServed || got[0].Reason != "the datasource serves no corporate_events" {
		t.Errorf("fetch key = %+v, want not served for want of the kind", got[0])
	}
}
