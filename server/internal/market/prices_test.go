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

// prices is a Prices integration that answers each identifier from answers,
// fails it with errs, and declines an identifier in neither.
type prices struct {
	fake
	answers map[string]PriceResult
	errs    map[string]error
}

func (p *prices) Prices() Server[PriceRequest, PriceResult] { return p }

func (p *prices) Serves(r PriceRequest) (types.Identifier, error) {
	if _, ok := p.answers[r.Identifier.Value]; ok {
		return r.Identifier, nil
	}
	if _, ok := p.errs[r.Identifier.Value]; ok {
		return r.Identifier, nil
	}
	return types.Identifier{}, errors.New("serves no such identifier")
}

func (p *prices) Fetch(_ context.Context, reqs []Request[PriceRequest]) ([]Response[PriceResult], error) {
	p.calls++
	out := make([]Response[PriceResult], len(reqs))
	for i, r := range reqs {
		if err, ok := p.errs[r.Sent.Value]; ok {
			out[i].Err = err
			continue
		}
		out[i].Value = p.answers[r.Sent.Value]
	}
	return out, nil
}

func priceRequest(value string, period types.Period) PriceRequest {
	return PriceRequest{Identifier: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: value}, Period: period}
}

var closes = PriceResult{
	Prices: []DayPrice{
		{Day: day(2024, 6, 7), Price: decimal.RequireFromString("1208.88"), AsAt: day(2024, 6, 7)},
		{Day: day(2024, 6, 10), Price: decimal.RequireFromString("121.79"), AsAt: day(2024, 6, 10)},
	},
	Currency: "USD",
	Coverage: &types.Period{From: day(2024, 6, 1), Before: day(2024, 7, 1)},
}

// TestFetchPrices checks the outcome of a served, a declined and a failed
// request. No stated key asks for a price fetch. A served key counts its
// prices. Each key records the period asked.
func TestFetchPrices(t *testing.T) {
	p := &prices{fake: fake{batch: 10}, answers: map[string]PriceResult{"NVDA": closes}, errs: map[string]error{"GONE": permanent("unknown ticker")}}
	h := newHarness(t, p, nil)
	period := types.Period{From: day(2024, 6, 1), Before: day(2024, 7, 1)}
	got, err := h.fetcher.Prices(context.Background(), h.parent, h.entry, []PriceRequest{
		priceRequest("NVDA", period), priceRequest("ODD", period), priceRequest("GONE", period),
	})
	if err != nil {
		t.Fatalf("Prices() error = %v", err)
	}
	want := []gen.FetchOutcome{gen.FetchOutcomeServed, gen.FetchOutcomeNotServed, gen.FetchOutcomeFailedPermanent}
	for i, k := range got {
		if k.Outcome != want[i] {
			t.Errorf("key %d outcome = %s, want %s", i, k.Outcome, want[i])
		}
	}
	if diff := cmp.Diff(closes, got[0].Response); diff != "" {
		t.Errorf("served response mismatch (-want +got):\n%s", diff)
	}
	if len(h.keys) != 3 || h.keys[0].Candidates != 2 {
		t.Errorf("fetch keys = %+v, want three with the served one counting two prices", h.keys)
	}
	for _, k := range h.keys {
		if k.StatedKeyID != nil {
			t.Errorf("fetch key = %+v, want no stated key", k)
		}
		if k.AskedFrom == nil || !k.AskedFrom.Equal(period.From) || k.AskedBefore == nil || !k.AskedBefore.Equal(period.Before) {
			t.Errorf("fetch key asked %v to %v, want the period of the request", k.AskedFrom, k.AskedBefore)
		}
	}
}

// TestFetchPricesCached checks that a cached answer survives the cache
// whole, with its exact prices, its currency, its coverage and the moment of
// the call that filled it, and that another period calls the provider again.
func TestFetchPricesCached(t *testing.T) {
	p := &prices{fake: fake{batch: 10}, answers: map[string]PriceResult{"NVDA": closes}}
	h := newHarness(t, p, nil)
	fetch := func(period types.Period) FetchKey[PriceRequest, PriceResult] {
		t.Helper()
		got, err := h.fetcher.Prices(context.Background(), h.parent, h.entry, []PriceRequest{priceRequest("NVDA", period)})
		if err != nil {
			t.Fatalf("Prices() error = %v", err)
		}
		return got[0]
	}
	period := types.Period{From: day(2024, 6, 1), Before: day(2024, 7, 1)}
	fetch(period)
	h.now = start.Add(time.Hour)
	hit := fetch(period)
	if !hit.cached || p.calls != 1 {
		t.Fatalf("cached = %v after %d calls, want the repeated period cached", hit.cached, p.calls)
	}
	if diff := cmp.Diff(closes, hit.Response); diff != "" {
		t.Errorf("cached response mismatch (-want +got):\n%s", diff)
	}
	if !hit.AnsweredAt.Equal(start) {
		t.Errorf("hit answered at %v, want the moment of the call that filled the cache %v", hit.AnsweredAt, start)
	}
	fetch(types.Period{From: day(2024, 6, 1), Before: day(2024, 8, 1)})
	if p.calls != 2 {
		t.Errorf("calls = %d, want another period to call again", p.calls)
	}
}

// TestFetchPricesNotServed checks that an integration without the capability
// does not serve prices.
func TestFetchPricesNotServed(t *testing.T) {
	h := newHarness(t, &facts{}, nil)
	got, err := h.fetcher.Prices(context.Background(), h.parent, h.entry, []PriceRequest{priceRequest("NVDA", types.Period{From: day(2024, 6, 1), Before: day(2024, 7, 1)})})
	if err != nil {
		t.Fatalf("Prices() error = %v", err)
	}
	if got[0].Outcome != gen.FetchOutcomeNotServed || got[0].Reason != "the datasource serves no prices" {
		t.Errorf("fetch key = %+v, want not served for want of the kind", got[0])
	}
}
