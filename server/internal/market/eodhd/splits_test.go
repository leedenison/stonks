package eodhd

import (
	"context"
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

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// asked is the period of every corporate events request in the tests. It
// starts before EODHD's earliest split in the cassettes and ends after the
// day of the recording.
var asked = types.Period{From: day(1970, 1, 2), Before: day(2027, 1, 4)}

func splitsOf(c *Client, ticker types.Identifier) ([]market.Response[market.CorporateEventResult], error) {
	s := c.CorporateEvents()
	req := market.CorporateEventRequest{Ticker: ticker, Period: asked}
	sent, err := s.Serves(req)
	if err != nil {
		return nil, err
	}
	return s.Fetch(context.Background(), []market.Request[market.CorporateEventRequest]{{Value: req, Sent: sent}})
}

// described states each event as "ex-date from:to", with "unhandled"
// appended where the event is not handled.
func described(evs []market.CorporateEvent) []string {
	out := []string{}
	for _, e := range evs {
		d := e.ExDate.Format(time.DateOnly) + " " + e.Ratio.From.String() + ":" + e.Ratio.To.String()
		if !e.Handled {
			d += " unhandled"
		}
		out = append(out, d)
	}
	return out
}

func symbol(v string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: v}
}

// TestSplitsServes checks the symbol each request sends and the requests
// Serves refuses.
func TestSplitsServes(t *testing.T) {
	s := client(t).CorporateEvents()
	tests := []struct {
		name    string
		ticker  types.Identifier
		want    types.Identifier
		refused bool
	}{
		{name: "a US ticker", ticker: ticker("XNAS", "NVDA"), want: symbol("NVDA.US")},
		{name: "a US class share", ticker: ticker("XNYS", "BRK.B"), want: symbol("BRK-B.US")},
		{name: "an LSE ticker", ticker: ticker("XLON", "VOD"), want: symbol("VOD.LSE")},
		{name: "an LSE class share", ticker: ticker("XLON", "BT.A"), want: symbol("BT-A.LSE")},
		{name: "a domain", ticker: ticker("XNYS", ""), refused: true},
		{name: "a venue EODHD does not list", ticker: ticker("XXXX", "VOD"), refused: true},
		// KO and KQ both list XKRX, and the ticker does not say which.
		{name: "a venue several exchanges list", ticker: ticker("XKRX", "005930"), refused: true},
		{name: "two share class separators", ticker: ticker("XNYS", "A.B.C"), refused: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := market.CorporateEventRequest{Ticker: tc.ticker, Period: asked}
			got, err := s.Serves(req)
			if tc.refused {
				if err == nil {
					t.Errorf("Serves(%+v) = %v, want a refusal", req, got)
				}
				return
			}
			require.NoError(t, err)
			if got != tc.want {
				t.Errorf("Serves(%+v) = %v, want %v", req, got, tc.want)
			}
		})
	}
}

// TestSplits checks the corporate events EODHD returned for each ticker.
// Every answer covers asked, except the answer for a symbol EODHD does not
// know.
func TestSplits(t *testing.T) {
	tests := []struct {
		cassette string
		ticker   types.Identifier
		want     []string
		// unknown marks a symbol EODHD does not know.
		unknown bool
	}{
		{
			cassette: "eodhd_splits_forward",
			ticker:   ticker("XNAS", "NVDA"),
			want:     []string{"2000-06-27 1:2", "2001-09-12 1:2", "2006-04-07 1:2", "2007-09-11 2:3", "2021-07-20 1:4", "2024-06-10 1:10"},
		},
		{
			// Vodafone's ratio of 1994 is a price factor, and its 2006 and
			// 2014 events are share consolidations.
			cassette: "eodhd_splits_lse",
			ticker:   ticker("XLON", "VOD"),
			want:     []string{"1994-07-21 319897:959692", "1999-10-01 1:5", "2006-07-31 8:7", "2014-02-24 11:6"},
		},
		{
			cassette: "eodhd_splits_class_share",
			ticker:   ticker("XLON", "BT.A"),
			want:     []string{"2001-05-21 387:431", "2001-11-09 9622:12411"},
		},
		{
			// GE's spin-offs of 2019, 2023 and 2024 come as ratios that
			// state the fall in GE's price.
			cassette: "eodhd_splits_spinoff",
			ticker:   ticker("XNYS", "GE"),
			want: []string{
				"1971-06-08 1:2", "1983-06-02 1:2", "1987-05-26 1:2", "1994-05-16 1:2", "1997-05-12 1:2", "2000-05-08 1:3",
				"2019-02-26 100:104", "2021-08-02 8:1", "2023-01-04 1000:1281", "2024-04-02 1000:1253",
			},
		},
		{cassette: "eodhd_splits_empty", ticker: ticker("XNYS", "BRK.A"), want: []string{}},
		{cassette: "eodhd_splits_unknown", ticker: ticker("XNAS", "ZZZZQX"), want: []string{}, unknown: true},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "EODHD_API_KEY", "testdata/"+tc.cassette))
			got, err := splitsOf(c, tc.ticker)
			require.NoError(t, err)
			require.Len(t, got, 1)
			require.NoError(t, got[0].Err)
			if diff := cmp.Diff(tc.want, described(got[0].Value.Events)); diff != "" {
				t.Errorf("events of %s mismatch (-want +got):\n%s", tc.ticker.Value, diff)
			}
			cov := got[0].Value.Coverage
			switch {
			case tc.unknown && cov != nil:
				t.Errorf("coverage of %s = %v, want none", tc.ticker.Value, *cov)
			case !tc.unknown && (cov == nil || *cov != asked):
				t.Errorf("coverage of %s = %v, want %v", tc.ticker.Value, cov, asked)
			}
		})
	}
}

// TestSplitsUnauthorised checks that a refused API key blocks the
// datasource. The recording is made with a key EODHD did not issue.
func TestSplitsUnauthorised(t *testing.T) {
	const cassette = "eodhd_splits_unauthorised"
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

// TestSplitsRefused checks that a refusal other than an unknown symbol is an
// error that blocks the symbol. EODHD documents a 403 for a symbol its plan
// does not cover.
func TestSplitsRefused(t *testing.T) {
	tests := []struct {
		name   string
		status int
		body   string
	}{
		{name: "a 404 with another body", status: http.StatusNotFound, body: "Exchange Not Found."},
		{name: "a 403", status: http.StatusForbidden, body: "Forbidden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c, err := New(market.Config{Name: "eodhd", Credential: "k", Endpoint: srv.URL}, mics, srv.Client())
			require.NoError(t, err)
			got, err := splitsOf(c, ticker("XNAS", "AAPL"))
			if err == nil {
				t.Fatalf("Fetch() = %+v, want an error", got)
			}
			if f, want := c.Classify(err), (market.Failure{Scope: market.ScopeKey}); f != want {
				t.Errorf("Classify(%v) = %+v, want %+v", err, f, want)
			}
		})
	}
}

// TestFromSplits checks that a split which does not state one ratio is an
// error.
func TestFromSplits(t *testing.T) {
	for _, s := range []string{"2.000000", "2/1/1", "two/1", "2/one"} {
		if got, err := fromSplits([]split{{Date: "2024-06-10", Split: s}}); err == nil {
			t.Errorf("fromSplits(%q) = %v, want an error", s, got)
		}
	}
}
