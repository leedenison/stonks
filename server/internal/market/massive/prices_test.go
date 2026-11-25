package massive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/types"
)

// recorded is the moment of every price test: the day the price cassettes
// were recorded. On it the window of a Basic plan starts on 26 November 2024.
var recorded = time.Date(2026, time.November, 25, 15, 0, 0, 0, time.UTC)

// priced is a period that starts before the window of every Basic plan and
// ends after the day of the recording.
var priced = types.Period{From: day(2024, 1, 1), Before: day(2026, 12, 1)}

// at pins the clock of c to the moment now.
func at(c *Client, now time.Time) *Client {
	c.now = func() time.Time { return now }
	return c
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

// barAt is a bar of Massive for day d that starts at hour o'clock UTC. A
// stock bar starts at midnight in New York, which is four or five o'clock,
// and a currency bar at midnight UTC.
func barAt(d time.Time, hour int, closing string) string {
	return fmt.Sprintf(`{"t":%d,"c":%s,"o":1,"h":1,"l":1,"v":1}`, d.Add(time.Duration(hour)*time.Hour).UnixMilli(), closing)
}

func barsBody(bars ...string) string {
	return `{"ticker":"T","resultsCount":` + fmt.Sprint(len(bars)) + `,"adjusted":false,"results":[` + strings.Join(bars, ",") + `],"status":"OK"}`
}

// TestPricesServes checks the ticker each request sends and the requests
// Serves refuses, on a Basic plan.
func TestPricesServes(t *testing.T) {
	s := at(newClient(t), recorded).Prices()
	tests := []struct {
		name    string
		req     market.PriceRequest
		want    types.Identifier
		refused bool
	}{
		{name: "a ticker", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: priced}, want: ticker("", "NVDA")},
		{name: "a class share", req: market.PriceRequest{Identifier: ticker("XNYS", "BRK/B"), Period: priced}, want: ticker("", "BRK.B")},
		{name: "a period that straddles the start of the window", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: types.Period{From: day(2020, 1, 2), Before: day(2024, 11, 27)}}, want: ticker("", "NVDA")},
		{name: "a period that ends at the start of the window", req: market.PriceRequest{Identifier: ticker("XNAS", "NVDA"), Period: types.Period{From: day(2020, 1, 2), Before: day(2024, 11, 26)}}, refused: true},
		{name: "a currency", req: market.PriceRequest{Identifier: currencyOf("EUR"), Period: priced}, want: currencyOf("EUR")},
		{name: "USD", req: market.PriceRequest{Identifier: currencyOf("USD"), Period: priced}, refused: true},
		{name: "a currency over a period that ends at the start of the window", req: market.PriceRequest{Identifier: currencyOf("EUR"), Period: types.Period{From: day(2020, 1, 2), Before: day(2024, 11, 26)}}, refused: true},
		{name: "a CUSIP", req: market.PriceRequest{Identifier: cusip, Period: priced}, refused: true},
		{name: "a domain", req: market.PriceRequest{Identifier: ticker("XNYS", ""), Period: priced}, refused: true},
		{name: "a venue outside Massive's table", req: market.PriceRequest{Identifier: ticker("XLON", "VOD"), Period: priced}, refused: true},
		{name: "a preferred share", req: market.PriceRequest{Identifier: ticker("XNYS", "BACpL"), Period: priced}, refused: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := s.Serves(tc.req)
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

// TestPricesFetch checks the request the client sends, and the prices and
// coverage it takes from each answer, on a Basic plan.
func TestPricesFetch(t *testing.T) {
	tests := []struct {
		name  string
		asked types.Period
		body  string
		// coverage is empty where the answer is uncovered.
		coverage string
		want     []string
		paged    bool
	}{
		{
			name:  "a period that starts before the window and ends after the last bar",
			asked: priced,
			body: barsBody(
				barAt(day(2024, 11, 25), 5, "232.87"), barAt(day(2024, 11, 26), 5, "235.06"),
				barAt(day(2026, 11, 23), 5, "349.08"), barAt(day(2026, 11, 24), 5, "354.07"),
			),
			coverage: "2024-11-26..2026-11-25",
			want:     []string{"2024-11-26 235.06", "2026-11-23 349.08", "2026-11-24 354.07"},
		},
		{
			name:  "bars before and after the asked period",
			asked: types.Period{From: day(2026, 11, 17), Before: day(2026, 11, 20)},
			body: barsBody(
				barAt(day(2026, 11, 16), 5, "1.5"), barAt(day(2026, 11, 17), 5, "2.5"), barAt(day(2026, 11, 18), 5, "3.5"),
				barAt(day(2026, 11, 19), 5, "4.5"), barAt(day(2026, 11, 20), 5, "5.5"), barAt(day(2026, 11, 23), 5, "6.5"),
			),
			coverage: "2026-11-17..2026-11-20",
			want:     []string{"2026-11-17 2.5", "2026-11-18 3.5", "2026-11-19 4.5"},
		},
		{
			name:     "a weekend between two bars",
			asked:    types.Period{From: day(2026, 11, 21), Before: day(2026, 11, 23)},
			body:     barsBody(barAt(day(2026, 11, 20), 5, "5.5"), barAt(day(2026, 11, 23), 5, "6.5")),
			coverage: "2026-11-21..2026-11-23",
			want:     []string{},
		},
		{
			name:  "a weekend after the last bar",
			asked: types.Period{From: day(2026, 11, 21), Before: day(2026, 11, 23)},
			body:  barsBody(barAt(day(2026, 11, 19), 5, "4.5"), barAt(day(2026, 11, 20), 5, "5.5")),
			want:  []string{},
		},
		{
			name:     "a bar in the summer, which starts at four o'clock UTC",
			asked:    types.Period{From: day(2026, 7, 1), Before: day(2026, 7, 2)},
			body:     barsBody(barAt(day(2026, 7, 1), 4, "7.25")),
			coverage: "2026-07-01..2026-07-02",
			want:     []string{"2026-07-01 7.25"},
		},
		{
			name:  "an unknown ticker",
			asked: priced,
			body:  `{"ticker":"ZZZZQQ","queryCount":0,"resultsCount":0,"adjusted":false,"status":"DELAYED"}`,
			want:  []string{},
		},
		{
			name:  "an answer that runs past one page",
			asked: priced,
			body:  `{"status":"OK","results":[` + barAt(day(2026, 11, 20), 5, "5.5") + `],"next_url":"https://api.massive.com/v2/aggs/ticker/T?cursor=AQcPBA"}`,
			paged: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var path string
			var query url.Values
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				path, query = r.URL.Path, r.URL.Query()
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c, err := New(market.Config{Credential: "k", Endpoint: srv.URL, JSON: json.RawMessage(conf)}, mics, http.DefaultClient)
			require.NoError(t, err)
			got, err := pricesOf(at(c, recorded), ticker("XNYS", "BRK/B"), tc.asked)
			require.NoError(t, err)
			require.Len(t, got, 1)
			if want := "/v2/aggs/ticker/BRK.B/range/1/day/2003-09-10/2099-12-31"; path != want {
				t.Errorf("path = %s, want %s", path, want)
			}
			if diff := cmp.Diff(url.Values{"apiKey": {"k"}, "adjusted": {"false"}, "sort": {"asc"}, "limit": {"50000"}}, query); diff != "" {
				t.Errorf("query mismatch (-want +got):\n%s", diff)
			}
			if tc.paged {
				if !errors.Is(got[0].Err, errPaged) {
					t.Fatalf("Fetch() error = %v, want %v", got[0].Err, errPaged)
				}
				if f, want := c.Classify(got[0].Err), (market.Failure{Scope: market.ScopeKey}); f != want {
					t.Errorf("Classify(%v) = %+v, want %+v", got[0].Err, f, want)
				}
				return
			}
			require.NoError(t, got[0].Err)
			res := got[0].Value
			if got := span(res.Coverage); got != tc.coverage {
				t.Errorf("coverage = %q, want %q", got, tc.coverage)
			}
			if want := map[bool]string{true: "USD", false: ""}[tc.coverage != ""]; res.Currency != want {
				t.Errorf("currency = %q, want %q", res.Currency, want)
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

// TestPricesAsTraded checks that Massive's bars are as traded. SOXX split 3
// for 1 on 5 November 2026, so its close falls to about a third on that day.
// It also checks the coverage: from the start of the window to the last bar.
func TestPricesAsTraded(t *testing.T) {
	const cassette = "massive_prices_split"
	c := at(replay(t, cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette)), recorded)
	got, err := pricesOf(c, ticker("XNAS", "SOXX"), priced)
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	res := got[0].Value
	require.NotNil(t, res.Coverage)
	require.NotEmpty(t, res.Prices)
	last := res.Prices[len(res.Prices)-1].Day
	if want := (types.Period{From: day(2024, 11, 26), Before: last.AddDate(0, 0, 1)}); *res.Coverage != want {
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

// TestPricesDividends checks that an as traded bar is free of any dividend
// adjustment. KO paid a dividend each quarter and did not split, so its
// closes are the same whether or not Massive adjusts them.
func TestPricesDividends(t *testing.T) {
	const cassette = "massive_prices_dividend"
	c := at(replay(t, cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette)), recorded)
	got, err := pricesOf(c, ticker("XNYS", "KO"), priced)
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	var page struct {
		Results []bar `json:"results"`
	}
	q := url.Values{"apiKey": {c.key}, "adjusted": {"true"}, "sort": {"asc"}, "limit": {barLimit}}
	require.NoError(t, c.get(context.Background(), "/v2/aggs/ticker/KO/range/1/day/2003-09-10/2099-12-31", q, &page))
	adjusted, err := fromBars(page.Results, priced, day(2024, 11, 26))
	require.NoError(t, err)
	if len(adjusted.Prices) < 400 {
		t.Fatalf("Massive served %d adjusted bars, want two years of them", len(adjusted.Prices))
	}
	if diff := cmp.Diff(stated(adjusted.Prices), stated(got[0].Value.Prices)); diff != "" {
		t.Errorf("as traded closes differ from adjusted closes (-adjusted +as traded):\n%s", diff)
	}
}

// TestPricesUnknown checks that a ticker Massive lacks is uncovered.
func TestPricesUnknown(t *testing.T) {
	const cassette = "massive_prices_unknown"
	c := at(replay(t, cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette)), recorded)
	got, err := pricesOf(c, ticker("XNAS", "ZZZZQQ"), priced)
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	if res := got[0].Value; res.Coverage != nil || len(res.Prices) != 0 || res.Currency != "" {
		t.Errorf("Fetch() = %+v, want an uncovered answer", res)
	}
}

// TestPricesTimeframe checks how Massive refuses days that lie wholly before
// the history of a Basic plan. The client never sends such a request, since
// each of its requests reaches the present day. The failure belongs to that
// request and not to the account.
func TestPricesTimeframe(t *testing.T) {
	const cassette = "massive_prices_timeframe"
	c := replay(t, cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette))
	q := url.Values{"apiKey": {c.key}, "adjusted": {"false"}, "sort": {"asc"}, "limit": {barLimit}}
	var page json.RawMessage
	err := c.get(context.Background(), "/v2/aggs/ticker/AAPL/range/1/day/2020-01-02/2020-01-10", q, &page)
	if err == nil {
		t.Fatalf("Massive answered %s, want a refusal", page)
	}
	if got, want := c.Classify(err), (market.Failure{Scope: market.ScopeRequest}); got != want {
		t.Errorf("Classify(%v) = %+v, want %+v", err, got, want)
	}
}

// TestRatesFetch checks the pair the client asks for a currency, and that a
// currency bar, which starts at midnight UTC, takes its own day.
func TestRatesFetch(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		_, _ = w.Write([]byte(barsBody(barAt(day(2026, 11, 15), 0, "1.10243"), barAt(day(2026, 11, 16), 0, "1.09984"))))
	}))
	t.Cleanup(srv.Close)
	c, err := New(market.Config{Credential: "k", Endpoint: srv.URL, JSON: json.RawMessage(conf)}, mics, http.DefaultClient)
	require.NoError(t, err)
	got, err := pricesOf(at(c, recorded), currencyOf("EUR"), types.Period{From: day(2026, 11, 14), Before: day(2026, 11, 20)})
	require.NoError(t, err)
	require.NoError(t, got[0].Err)
	if want := "/v2/aggs/ticker/C:EURUSD/range/1/day/2003-09-10/2099-12-31"; path != want {
		t.Errorf("path = %s, want %s", path, want)
	}
	res := got[0].Value
	if got, want := span(res.Coverage), "2026-11-14..2026-11-17"; got != want || res.Currency != "USD" {
		t.Errorf("coverage = %q in %q, want %q in USD", got, res.Currency, want)
	}
	if diff := cmp.Diff([]string{"2026-11-15 1.10243", "2026-11-16 1.09984"}, stated(res.Prices)); diff != "" {
		t.Errorf("rates mismatch (-want +got):\n%s", diff)
	}
}

// TestPlansCutApart checks that each product is cut to the window of its own
// plan: five years of stock bars on Starter and two years of currency bars
// on Basic.
func TestPlansCutApart(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(barsBody(barAt(day(2026, 11, 24), 0, "1.5"))))
	}))
	t.Cleanup(srv.Close)
	c, err := New(market.Config{Credential: "k", Endpoint: srv.URL, JSON: json.RawMessage(`{"stocks_plan": "starter", "currencies_plan": "basic"}`)}, mics, http.DefaultClient)
	require.NoError(t, err)
	at(c, recorded)
	asked := types.Period{From: day(2010, 1, 4), Before: day(2026, 12, 1)}
	for id, want := range map[types.Identifier]string{
		ticker("XNAS", "NVDA"): "2021-11-26..2026-11-25",
		currencyOf("EUR"):      "2024-11-26..2026-11-25",
	} {
		got, err := pricesOf(c, id, asked)
		require.NoError(t, err)
		require.NoError(t, got[0].Err)
		if got := span(got[0].Value.Coverage); got != want {
			t.Errorf("coverage of %s = %q, want %q", id.Value, got, want)
		}
	}
}

// TestRates checks the direction of a rate and the days a currency series
// holds. The close of a pair that is quoted in USD is the number of USD that
// one unit of the currency buys: more than one for EUR and far less for KRW,
// which shows that Massive lists a minor pair in that direction too. The
// series holds a bar for a Sunday and lacks one for a Saturday.
func TestRates(t *testing.T) {
	const cassette = "massive_prices_rate"
	c := at(replay(t, cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette)), recorded)
	tests := []struct {
		code      string
		low, high float64
	}{
		{code: "EUR", low: 0.9, high: 1.4},
		{code: "KRW", low: 0.0005, high: 0.001},
	}
	for _, tc := range tests {
		t.Run(tc.code, func(t *testing.T) {
			got, err := pricesOf(c, currencyOf(tc.code), priced)
			require.NoError(t, err)
			require.NoError(t, got[0].Err)
			res := got[0].Value
			require.NotNil(t, res.Coverage)
			if !res.Coverage.From.Equal(day(2024, 11, 26)) || res.Currency != "USD" {
				t.Errorf("coverage = %s in %q, want it from 2024-11-26 in USD", span(res.Coverage), res.Currency)
			}
			by := closes(res.Prices)
			for d, p := range by {
				if p.LessThan(decimal.NewFromFloat(tc.low)) || p.GreaterThan(decimal.NewFromFloat(tc.high)) {
					t.Errorf("rate on %s = %s, want between %v and %v", d, p, tc.low, tc.high)
				}
			}
			if _, ok := by["2026-11-14"]; ok {
				t.Errorf("the series holds a rate for Saturday 2026-11-14, want none")
			}
			if _, ok := by["2026-11-15"]; !ok {
				t.Errorf("the series lacks a rate for Sunday 2026-11-15")
			}
		})
	}
}
