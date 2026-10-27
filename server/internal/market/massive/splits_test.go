package massive

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/types"
)

// asked is the period of every corporate actions request in the tests. It
// starts before since and ends after the day of the recording.
var asked = types.Period{From: day(2000, 1, 3), Before: day(2027, 1, 4)}

func splitsOf(c *Client, ticker types.Identifier) ([]market.Response[market.CorpActResult], error) {
	s := c.CorpActs()
	sent, err := s.Serves(market.CorpActRequest{Ticker: ticker, Period: asked})
	if err != nil {
		return nil, err
	}
	return s.Fetch(context.Background(), []market.Request[market.CorpActRequest]{{Value: market.CorpActRequest{Ticker: ticker, Period: asked}, Sent: sent}})
}

// described states each action as "ex-date from:to type", with "unhandled"
// appended where Massive's type does not restate a quantity.
func described(acts []market.CorpAct) []string {
	out := []string{}
	for _, e := range acts {
		d := e.ExDate.Format(time.DateOnly) + " " + e.Ratio.From.String() + ":" + e.Ratio.To.String() + " " + e.Type
		if !e.Handled {
			d += " unhandled"
		}
		out = append(out, d)
	}
	return out
}

// TestSplitsServes checks the ticker each request sends and the requests
// Serves refuses.
func TestSplitsServes(t *testing.T) {
	s := newClient(t).CorpActs()
	tests := []struct {
		name    string
		req     market.CorpActRequest
		want    types.Identifier
		refused bool
	}{
		{name: "a ticker", req: market.CorpActRequest{Ticker: ticker("XNAS", "NVDA"), Period: asked}, want: ticker("", "NVDA")},
		{name: "a class share", req: market.CorpActRequest{Ticker: ticker("XNYS", "BRK/B"), Period: asked}, want: ticker("", "BRK.B")},
		{name: "a period that ends on Massive's first day", req: market.CorpActRequest{Ticker: ticker("XNAS", "NVDA"), Period: types.Period{From: day(2001, 1, 2), Before: since}}, refused: true},
		{name: "a domain", req: market.CorpActRequest{Ticker: ticker("XNYS", ""), Period: asked}, refused: true},
		{name: "a venue outside Massive's table", req: market.CorpActRequest{Ticker: ticker("XLON", "VOD"), Period: asked}, refused: true},
		{name: "a preferred share", req: market.CorpActRequest{Ticker: ticker("XNYS", "BACpL"), Period: asked}, refused: true},
		{name: "two share class separators", req: market.CorpActRequest{Ticker: ticker("XNYS", "A.B.C"), Period: asked}, refused: true},
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

// TestSplits checks the corporate actions Massive returned for each ticker.
// Every answer covers asked from since.
func TestSplits(t *testing.T) {
	tests := []struct {
		cassette string
		ticker   types.Identifier
		want     []string
	}{
		{
			cassette: "massive_splits_forward",
			ticker:   ticker("XNAS", "NVDA"),
			want:     []string{"2024-06-10 1:10 forward_split", "2021-07-20 1:4 stock_dividend", "2007-09-11 2:3 forward_split", "2006-04-07 1:2 forward_split"},
		},
		{
			// GE's spin-offs of 2023 and 2024 come as forward splits whose
			// ratios state the fall in GE's price.
			cassette: "massive_splits_spinoff",
			ticker:   ticker("XNYS", "GE"),
			want:     []string{"2024-04-02 1000:1253 forward_split", "2023-01-04 1000:1281 forward_split", "2021-08-02 8:1 reverse_split"},
		},
		{
			cassette: "massive_splits_class_share",
			ticker:   ticker("XNYS", "BRK/B"),
			want:     []string{"2010-01-21 1:50 forward_split"},
		},
		{
			// On the day of the recording SOXX had announced a split with a
			// later ex-date.
			cassette: "massive_splits_announced",
			ticker:   ticker("XNAS", "SOXX"),
			want:     []string{"2026-11-05 1:3 forward_split", "2024-03-07 1:3 forward_split"},
		},
		{cassette: "massive_splits_empty", ticker: ticker("XNAS", "ZZZZQX"), want: []string{}},
		{
			// Massive's splits endpoint does not cover mutual funds. It
			// answers one as a ticker that has never split.
			cassette: "massive_splits_mutual_fund",
			ticker:   ticker("XNAS", "VFIAX"),
			want:     []string{},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette))
			got, err := splitsOf(c, tc.ticker)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.NoError(t, got[0].Err)
			if diff := cmp.Diff(tc.want, described(got[0].Value.Acts)); diff != "" {
				t.Errorf("events of %s mismatch (-want +got):\n%s", tc.ticker.Value, diff)
			}
			if want := (types.Period{From: since, Before: asked.Before}); got[0].Value.Coverage == nil || *got[0].Value.Coverage != want {
				t.Errorf("coverage of %s = %v, want %v", tc.ticker.Value, got[0].Value.Coverage, want)
			}
		})
	}
}

// TestSplitsUnauthorised checks that a refused API key blocks the
// datasource, although the splits endpoint refuses it with a 404. The
// recording is made with a key Massive does not hold.
func TestSplitsUnauthorised(t *testing.T) {
	const cassette = "massive_splits_unauthorised"
	key := vcr.Placeholder
	if vcr.Recording(cassette) {
		key = "not-a-key"
	}
	c := replay(t, cassette, key)
	_, err := splitsOf(c, ticker("XNAS", "AAPL"))
	if err == nil {
		t.Fatal("Fetch() with a refused key succeeded")
	}
	if got, want := c.Classify(err), (market.Failure{Scope: market.ScopeDatasource}); got != want {
		t.Errorf("Classify(%v) = %+v, want %+v", err, got, want)
	}
}

// TestSplitsBody checks answers Massive cannot be made to send on demand.
func TestSplitsBody(t *testing.T) {
	tests := []struct {
		name string
		body string
		want []string
		// paged is whether the answer runs past one page.
		paged bool
	}{
		{
			name: "a type Massive does not document",
			body: `{"status":"OK","results":[{"execution_date":"2025-03-03","split_from":1.0,"split_to":2.0,"ticker":"ZZZZQX","adjustment_type":"spin_off"}]}`,
			want: []string{"2025-03-03 1:2 spin_off unhandled"},
		},
		{
			name: "a fractional ratio",
			body: `{"status":"OK","results":[{"execution_date":"2026-11-03","split_from":1.0,"split_to":1.0106,"ticker":"ZZZZQX","adjustment_type":"stock_dividend"}]}`,
			want: []string{"2026-11-03 1:1.0106 stock_dividend"},
		},
		{
			name:  "an answer that runs past a page",
			body:  `{"status":"OK","results":[],"next_url":"https://api.massive.com/stocks/v1/splits?cursor=AQcPBA"}`,
			paged: true,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c, err := New(market.Config{Credential: "k", Endpoint: srv.URL, JSON: json.RawMessage(conf)}, mics, http.DefaultClient)
			require.NoError(t, err)
			got, err := splitsOf(c, ticker("XNAS", "ZZZZQX"))
			require.NoError(t, err)
			require.Len(t, got, 1)
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
			if diff := cmp.Diff(tc.want, described(got[0].Value.Acts)); diff != "" {
				t.Errorf("events mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
