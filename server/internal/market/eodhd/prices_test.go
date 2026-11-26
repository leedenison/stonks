package eodhd

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// pins holds the moment each price cassette was recorded. On 25 November
// 2026 the window of the free plan starts on 26 November 2025.
var pins = map[string]time.Time{
	"eodhd_prices_unknown": time.Date(2026, time.November, 25, 16, 0, 0, 0, time.UTC),
	"eodhd_prices_split":   time.Date(2026, time.November, 25, 16, 0, 0, 0, time.UTC),
	"eodhd_prices_lse":     time.Date(2026, time.November, 25, 16, 0, 0, 0, time.UTC),
	"eodhd_prices_rate":    time.Date(2026, time.November, 25, 16, 0, 0, 0, time.UTC),
}

// pinned is the moment of every price test that replays no cassette.
var pinned = time.Date(2026, time.November, 25, 15, 0, 0, 0, time.UTC)

// priced is a period that starts before the window of the free plan and ends
// after the day of every recording.
var priced = types.Period{From: day(2025, 1, 1), Before: day(2026, 12, 1)}

// at pins the clock of c to the moment now.
func at(c *Client, now time.Time) *Client {
	c.now = func() time.Time { return now }
	return c
}

// on returns a client on plan that calls endpoint, with its clock at pinned.
func on(t *testing.T, plan, endpoint string) *Client {
	t.Helper()
	c, err := New(market.Config{Name: "eodhd", Credential: "k", Endpoint: endpoint, JSON: json.RawMessage(`{"plan": "` + plan + `"}`)}, mics, http.DefaultClient)
	require.NoError(t, err)
	return at(c, pinned)
}

// replayed returns a client on the free plan that replays cassette, with its
// clock at the moment of the recording.
func replayed(t *testing.T, cassette string) *Client {
	t.Helper()
	return at(replay(t, cassette, vcr.Credential(t, "EODHD_API_KEY", "testdata/"+cassette)), pins[cassette])
}

func pricesOf(c *Client, id types.Identifier, p types.Period) ([]market.Response[market.PriceResult], error) {
	s := c.Prices()
	req := market.PriceRequest{Identifier: id, Period: p}
	sent, err := s.Serves(req)
	if err != nil {
		return nil, err
	}
	return s.Fetch(context.Background(), []market.Request[market.PriceRequest]{{Value: req, Sent: sent}})
}

// span states a period as "from..before".
func span(p *types.Period) string {
	if p == nil {
		return ""
	}
	return p.From.Format(time.DateOnly) + ".." + p.Before.Format(time.DateOnly)
}

// stated states each price as "day close", and appends the as at day where
// it differs from the day.
func stated(ps []market.DayPrice) []string {
	out := []string{}
	for _, p := range ps {
		s := p.Day.Format(time.DateOnly) + " " + p.Price.String()
		if !p.AsAt.Equal(p.Day) {
			s += " as at " + p.AsAt.Format(time.DateOnly)
		}
		out = append(out, s)
	}
	return out
}

func currencyOf(code string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeCurrency, Value: code}
}

// barOn is a bar of EODHD for day d.
func barOn(d time.Time, closing string) string {
	return `{"date":"` + d.Format(time.DateOnly) + `","open":1,"high":1,"low":1,"close":` + closing + `,"adjusted_close":0.5,"volume":1}`
}

func barsBody(bars ...string) string { return "[" + strings.Join(bars, ",") + "]" }

// fresh checks that the last price of res lies within a week before the
// moment of the recording, so a cassette recorded again without a new pin
// fails.
func fresh(t *testing.T, cassette string, res market.PriceResult) {
	t.Helper()
	require.NotEmpty(t, res.Prices)
	last, pin := res.Prices[len(res.Prices)-1].Day, pins[cassette]
	if last.After(pin) || last.Before(pin.AddDate(0, 0, -7)) {
		t.Errorf("last price of %s is on %s, want within a week before its pin %s", cassette, last.Format(time.DateOnly), pin.Format(time.DateOnly))
	}
}

// TestPricesServes checks the symbol each request sends and the requests
// Serves refuses.
func TestPricesServes(t *testing.T) {
	old := types.Period{From: day(2020, 1, 2), Before: day(2025, 11, 26)}
	tests := []struct {
		name    string
		plan    string
		req     market.PriceRequest
		want    types.Identifier
		refused bool
	}{
		{name: "a US ticker", plan: "free", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: priced}, want: dsTicker("NVDA.US")},
		{name: "a US class share", plan: "free", req: market.PriceRequest{Identifier: ticker("XNYS", "BRK.B"), Period: priced}, want: dsTicker("BRK-B.US")},
		{name: "an LSE ticker", plan: "free", req: market.PriceRequest{Identifier: ticker("XLON", "VOD"), Period: priced}, want: dsTicker("VOD.LSE")},
		{name: "a period that straddles the start of the window", plan: "free", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: types.Period{From: day(2020, 1, 2), Before: day(2025, 11, 27)}}, want: dsTicker("NVDA.US")},
		{name: "a period that ends at the start of the window", plan: "free", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: old}, refused: true},
		{name: "an old period on a plan with the whole history", plan: "all_in_one", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: old}, want: dsTicker("NVDA.US")},
		{name: "a plan without daily bars", plan: "fundamentals", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: priced}, refused: true},
		{name: "a currency", plan: "free", req: market.PriceRequest{Identifier: currencyOf("EUR"), Period: priced}, want: currencyOf("EUR")},
		{name: "USD", plan: "free", req: market.PriceRequest{Identifier: currencyOf("USD"), Period: priced}, refused: true},
		{name: "a currency over a period that ends at the start of the window", plan: "free", req: market.PriceRequest{Identifier: currencyOf("EUR"), Period: old}, refused: true},
		{name: "a currency on a plan without daily bars", plan: "fundamentals", req: market.PriceRequest{Identifier: currencyOf("EUR"), Period: priced}, refused: true},
		{name: "an ISIN", plan: "free", req: market.PriceRequest{Identifier: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}, Period: priced}, refused: true},
		{name: "a domain", plan: "free", req: market.PriceRequest{Identifier: ticker("XNYS", ""), Period: priced}, refused: true},
		// KO and KQ both list XKRX, and the ticker does not say which.
		{name: "a venue several exchanges list", plan: "free", req: market.PriceRequest{Identifier: ticker("XKRX", "005930"), Period: priced}, refused: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := on(t, tc.plan, "").Prices().Serves(tc.req)
			if tc.refused {
				if err == nil {
					t.Errorf("Serves(%+v) = %v, want a refusal", tc.req, got)
				}
				return
			}
			require.NoError(t, err)
			if got != tc.want {
				t.Errorf("Serves(%+v) = %v, want %v", tc.req, got, tc.want)
			}
		})
	}
}

// TestPricesFetch checks the requests the client sends, and the prices,
// currency and coverage it takes from each answer.
func TestPricesFetch(t *testing.T) {
	const vod = `[{"Code":"VOD","Exchange":"LSE","Type":"Common Stock","Currency":"GBX","ISIN":"GB00BH4HKS39","isPrimary":true}]`
	tests := []struct {
		name   string
		plan   string
		ticker types.Identifier
		asked  types.Period
		// search is the body of the search answer, for a ticker outside the US.
		search string
		// status is the status of the bars answer, where it is not 200.
		status int
		bars   string
		paths  []string
		// coverage and currency are empty where the answer is uncovered.
		coverage string
		currency string
		want     []string
		fails    bool
	}{
		{
			name: "a period that starts before the window and ends after the last bar", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced,
			bars:  barsBody(barOn(day(2025, 11, 25), "232.87"), barOn(day(2025, 11, 26), "235.06"), barOn(day(2026, 11, 23), "349.08"), barOn(day(2026, 11, 24), "354.07")),
			paths: []string{"/api/eod/BRK-B.US"}, coverage: "2025-11-26..2026-11-25", currency: "USD",
			want: []string{"2025-11-26 235.06", "2026-11-23 349.08", "2026-11-24 354.07"},
		},
		{
			name: "a first bar later than the window", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced,
			bars:  barsBody(barOn(day(2026, 3, 2), "10.5"), barOn(day(2026, 3, 3), "11.5")),
			paths: []string{"/api/eod/BRK-B.US"}, coverage: "2026-03-02..2026-03-04", currency: "USD",
			want: []string{"2026-03-02 10.5", "2026-03-03 11.5"},
		},
		{
			name: "a plan with the whole history", plan: "all_in_one", ticker: ticker("XNYS", "BRK.B"), asked: types.Period{From: day(2000, 1, 3), Before: day(2010, 1, 6)},
			bars:  barsBody(barOn(day(2010, 1, 4), "1.5"), barOn(day(2010, 1, 5), "2.5"), barOn(day(2010, 1, 6), "3.5")),
			paths: []string{"/api/eod/BRK-B.US"}, coverage: "2010-01-04..2010-01-06", currency: "USD",
			want: []string{"2010-01-04 1.5", "2010-01-05 2.5"},
		},
		{
			name: "bars before and after the asked period", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: types.Period{From: day(2026, 11, 17), Before: day(2026, 11, 20)},
			bars: barsBody(
				barOn(day(2026, 11, 16), "1.5"), barOn(day(2026, 11, 17), "2.5"), barOn(day(2026, 11, 18), "3.5"),
				barOn(day(2026, 11, 19), "4.5"), barOn(day(2026, 11, 20), "5.5"), barOn(day(2026, 11, 23), "6.5"),
			),
			paths: []string{"/api/eod/BRK-B.US"}, coverage: "2026-11-17..2026-11-20", currency: "USD",
			want: []string{"2026-11-17 2.5", "2026-11-18 3.5", "2026-11-19 4.5"},
		},
		{
			name: "a weekend between two bars", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: types.Period{From: day(2026, 11, 21), Before: day(2026, 11, 23)},
			bars:  barsBody(barOn(day(2026, 11, 20), "5.5"), barOn(day(2026, 11, 23), "6.5")),
			paths: []string{"/api/eod/BRK-B.US"}, coverage: "2026-11-21..2026-11-23", currency: "USD",
			want: []string{},
		},
		{
			name: "a weekend after the last bar", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: types.Period{From: day(2026, 11, 21), Before: day(2026, 11, 23)},
			bars:  barsBody(barOn(day(2026, 11, 19), "4.5"), barOn(day(2026, 11, 20), "5.5")),
			paths: []string{"/api/eod/BRK-B.US"}, want: []string{},
		},
		{
			name: "a close of many digits", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced,
			bars:  barsBody(barOn(day(2026, 11, 24), "0.0007431795238517")),
			paths: []string{"/api/eod/BRK-B.US"}, coverage: "2026-11-24..2026-11-25", currency: "USD",
			want: []string{"2026-11-24 0.0007431795238517"},
		},
		{
			name: "a ticker outside the US", plan: "free", ticker: ticker("XLON", "VOD"), asked: priced, search: vod,
			bars:  barsBody(barOn(day(2026, 11, 23), "102.85"), barOn(day(2026, 11, 24), "102.9")),
			paths: []string{"/api/search/VOD", "/api/eod/VOD.LSE"}, coverage: "2026-11-23..2026-11-25", currency: "GBX",
			want: []string{"2026-11-23 102.85", "2026-11-24 102.9"},
		},
		{
			name: "a ticker the search does not list", plan: "free", ticker: ticker("XLON", "VOD"), asked: priced,
			search: `[{"Code":"VODX","Exchange":"LSE","Type":"ETF","Currency":"USD"}]`,
			paths:  []string{"/api/search/VOD"}, want: []string{},
		},
		{
			name: "an answer without a bar", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced, bars: "[]",
			paths: []string{"/api/eod/BRK-B.US"}, want: []string{},
		},
		{
			name: "a symbol the bars endpoint does not know", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced, status: http.StatusNotFound, bars: "Ticker Not Found.",
			paths: []string{"/api/eod/BRK-B.US"}, want: []string{},
		},
		{
			name: "a symbol not found", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced, status: http.StatusNotFound, bars: "Symbol not found",
			paths: []string{"/api/eod/BRK-B.US"}, want: []string{},
		},
		{
			name: "a 404 for another reason", plan: "free", ticker: ticker("XNYS", "BRK.B"), asked: priced, status: http.StatusNotFound, bars: "Not Found",
			paths: []string{"/api/eod/BRK-B.US"}, fails: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var paths []string
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				paths = append(paths, r.URL.Path)
				if strings.HasPrefix(r.URL.Path, "/api/search/") {
					if got := r.URL.Query().Get("exchange"); got != "LSE" {
						t.Errorf("search exchange = %q, want LSE", got)
					}
					_, _ = w.Write([]byte(tc.search))
					return
				}
				if r.URL.Query().Has("from") || r.URL.Query().Has("to") {
					t.Errorf("bars query = %s, want no dates", r.URL.RawQuery)
				}
				if tc.status != 0 {
					w.WriteHeader(tc.status)
				}
				_, _ = w.Write([]byte(tc.bars))
			}))
			t.Cleanup(srv.Close)
			got, err := pricesOf(on(t, tc.plan, srv.URL), tc.ticker, tc.asked)
			if diff := cmp.Diff(tc.paths, paths); diff != "" {
				t.Errorf("paths mismatch (-want +got):\n%s", diff)
			}
			if tc.fails {
				if err == nil {
					t.Fatalf("Fetch() = %+v, want an error", got)
				}
				return
			}
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.NoError(t, got[0].Err)
			res := got[0].Value
			if got := span(res.Coverage); got != tc.coverage {
				t.Errorf("coverage = %q, want %q", got, tc.coverage)
			}
			if res.Currency != tc.currency {
				t.Errorf("currency = %q, want %q", res.Currency, tc.currency)
			}
			if diff := cmp.Diff(tc.want, stated(res.Prices)); diff != "" {
				t.Errorf("prices mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// closes maps each day of ps to its close.
func closes(ps []market.DayPrice) map[string]decimal.Decimal {
	out := map[string]decimal.Decimal{}
	for _, p := range ps {
		out[p.Day.Format(time.DateOnly)] = p.Price
	}
	return out
}

// TestPricesAsTraded checks that EODHD's closes are as traded. SOXX split 3
// for 1 on 5 November 2026, so its close falls to about a third on that day.
// It also checks the coverage: from the start of the window to the last bar.
func TestPricesAsTraded(t *testing.T) {
	const cassette = "eodhd_prices_split"
	got, err := pricesOf(replayed(t, cassette), ticker("XNAS", "SOXX"), priced)
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	res := got[0].Value
	require.NotNil(t, res.Coverage)
	fresh(t, cassette, res)
	last := res.Prices[len(res.Prices)-1].Day
	if want := (types.Period{From: day(2025, 11, 26), Before: last.AddDate(0, 0, 1)}); *res.Coverage != want {
		t.Errorf("coverage = %s, want %s", span(res.Coverage), span(&want))
	}
	if res.Currency != "USD" {
		t.Errorf("currency = %q, want USD", res.Currency)
	}
	for _, p := range res.Prices {
		if !p.AsAt.Equal(p.Day) || !res.Coverage.Contains(p.Day) {
			t.Errorf("price on %s is as at %s, want its own day inside %s", p.Day.Format(time.DateOnly), p.AsAt.Format(time.DateOnly), span(res.Coverage))
		}
	}
	by := closes(res.Prices)
	before, after := by["2026-11-04"], by["2026-11-05"]
	if before.IsZero() || after.IsZero() {
		t.Fatalf("closes either side of the split = %s and %s, want both", before, after)
	}
	if ratio := before.Div(after); ratio.LessThan(decimal.NewFromFloat(2.5)) || ratio.GreaterThan(decimal.NewFromFloat(3.5)) {
		t.Errorf("close fell from %s to %s at the split, want about a third", before, after)
	}
}

// TestPricesPence checks what EODHD states for a London symbol. The search
// states its currency as GBX. The close differs from the adjusted close
// before the ex date of a dividend, and the client takes the close, so a
// price is free of the dividend adjustment.
func TestPricesPence(t *testing.T) {
	const cassette = "eodhd_prices_lse"
	ctx := context.Background()
	c := replayed(t, cassette)
	vod := ticker("XLON", "VOD")
	rows, err := c.searchTicker(ctx, vod)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	if rows[0].Currency != "GBX" {
		t.Errorf("currency of VOD.LSE = %q, want GBX", rows[0].Currency)
	}
	var raw []struct {
		Date          string      `json:"date"`
		Close         json.Number `json:"close"`
		AdjustedClose json.Number `json:"adjusted_close"`
	}
	require.NoError(t, c.get(ctx, "/api/eod/VOD.LSE", map[string][]string{}, &raw))
	require.NotEmpty(t, raw)
	// exDate is the ex date of the one dividend VOD paid in the year.
	const exDate = "2026-06-04"
	bs := make([]bar, len(raw))
	want := []string{}
	for i, r := range raw {
		bs[i] = bar{Date: r.Date, Close: r.Close}
		want = append(want, r.Date+" "+r.Close.String())
		if r.Date < exDate && r.Close == r.AdjustedClose {
			t.Errorf("close on %s = adjusted close %s, want them to differ before the ex date", r.Date, r.Close)
		}
	}
	if raw[0].Date >= exDate {
		t.Fatalf("first bar is on %s, want one before the ex date %s", raw[0].Date, exDate)
	}
	res, err := fromBars(bs, types.Period{From: day(2000, 1, 3), Before: day(2099, 1, 1)}, time.Time{}, rows[0].Currency)
	require.NoError(t, err)
	fresh(t, cassette, res)
	if diff := cmp.Diff(want, stated(res.Prices)); diff != "" {
		t.Errorf("prices differ from the closes (-close +price):\n%s", diff)
	}
}

// TestPricesUnknown checks that a symbol EODHD lacks is uncovered.
func TestPricesUnknown(t *testing.T) {
	got, err := pricesOf(replayed(t, "eodhd_prices_unknown"), ticker("XNAS", "ZZZZQX"), priced)
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	if res := got[0].Value; res.Coverage != nil || len(res.Prices) != 0 || res.Currency != "" {
		t.Errorf("Fetch() = %+v, want an uncovered answer", res)
	}
}

// TestRatesFetch checks the pair the client asks for a currency, without a
// search, and that the rate is stated in USD.
func TestRatesFetch(t *testing.T) {
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.Path)
		_, _ = w.Write([]byte(barsBody(barOn(day(2026, 11, 15), "1.10243"), barOn(day(2026, 11, 16), "1.09984"))))
	}))
	t.Cleanup(srv.Close)
	got, err := pricesOf(on(t, "free", srv.URL), currencyOf("EUR"), types.Period{From: day(2026, 11, 14), Before: day(2026, 11, 20)})
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	if diff := cmp.Diff([]string{"/api/eod/EURUSD.FOREX"}, paths); diff != "" {
		t.Errorf("paths mismatch (-want +got):\n%s", diff)
	}
	res := got[0].Value
	if got, want := span(res.Coverage), "2026-11-15..2026-11-17"; got != want || res.Currency != "USD" {
		t.Errorf("coverage = %q in %q, want %q in USD", got, res.Currency, want)
	}
	if diff := cmp.Diff([]string{"2026-11-15 1.10243", "2026-11-16 1.09984"}, stated(res.Prices)); diff != "" {
		t.Errorf("rates mismatch (-want +got):\n%s", diff)
	}
}

// TestRates checks the direction of a rate. The close of a pair that is
// quoted in USD is the number of USD that one unit of the currency buys: more
// than one for EUR and far less for KRW, which shows that EODHD lists a minor
// pair in that direction too. A code EODHD lacks is uncovered.
func TestRates(t *testing.T) {
	const cassette = "eodhd_prices_rate"
	c := replayed(t, cassette)
	tests := []struct {
		code      string
		low, high float64
	}{
		{code: "EUR", low: 0.9, high: 1.4},
		{code: "KRW", low: 0.0005, high: 0.001},
	}
	for _, tc := range tests {
		got, err := pricesOf(c, currencyOf(tc.code), priced)
		require.NoError(t, err)
		require.NoError(t, got[0].Err)
		res := got[0].Value
		require.NotNil(t, res.Coverage)
		fresh(t, cassette, res)
		if !res.Coverage.From.Equal(day(2025, 11, 26)) || res.Currency != "USD" {
			t.Errorf("coverage of %s = %s in %q, want it from 2025-11-26 in USD", tc.code, span(res.Coverage), res.Currency)
		}
		for d, p := range closes(res.Prices) {
			if p.LessThan(decimal.NewFromFloat(tc.low)) || p.GreaterThan(decimal.NewFromFloat(tc.high)) {
				t.Errorf("rate of %s on %s = %s, want between %v and %v", tc.code, d, p, tc.low, tc.high)
			}
		}
	}
	got, err := pricesOf(c, currencyOf("ZZZ"), priced)
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	if res := got[0].Value; res.Coverage != nil || len(res.Prices) != 0 || res.Currency != "" {
		t.Errorf("Fetch() of ZZZ = %+v, want an uncovered answer", res)
	}
}
