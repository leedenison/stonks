package massive

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
	"github.com/leedenison/stonks/server/internal/types"
)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

var (
	// meta is Meta's composite FIGI. Its chain holds META, then FB.
	meta = types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000MM2P62"}
	// twitter is Twitter's composite FIGI. Twitter was delisted in 2022.
	twitter = types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000H6HNW3"}
	// oneDay and history are the periods of a lookup and of the other calls.
	oneDay  = types.Period{From: day(2021, 6, 1), Before: day(2021, 6, 2)}
	history = types.Period{From: since, Before: day(2026, 10, 23)}
)

func newClient(t *testing.T) *Client {
	t.Helper()
	c, err := New(market.Config{Credential: "k", JSON: json.RawMessage(conf)}, mics, http.DefaultClient)
	require.NoError(t, err)
	return c
}

// TestEventsServes checks the call that serves each request and the requests
// Serves refuses.
func TestEventsServes(t *testing.T) {
	e := events{c: newClient(t)}
	tests := []struct {
		name    string
		req     market.IdentifierEventRequest
		want    types.Identifier
		call    call
		refused bool
	}{
		{name: "a composite FIGI", req: market.IdentifierEventRequest{Identifier: meta, Period: history}, want: meta, call: chainCall},
		{name: "a CUSIP", req: market.IdentifierEventRequest{Identifier: cusip, Period: history}, want: cusip, call: chainCall},
		{name: "a ticker on one day", req: market.IdentifierEventRequest{Identifier: ticker("XNAS", "FB"), Period: oneDay}, want: ticker("", "FB"), call: lookupCall},
		{name: "a class share", req: market.IdentifierEventRequest{Identifier: ticker("XNYS", "BRK.B"), Period: oneDay}, want: ticker("", "BRK.B"), call: lookupCall},
		{name: "a ticker over a period", req: market.IdentifierEventRequest{Identifier: ticker("XNYS", "TWTR"), Period: history}, refused: true},
		{name: "a domain", req: market.IdentifierEventRequest{Identifier: ticker("XNYS", ""), Period: history}, refused: true},
		{name: "a venue outside Massive's table", req: market.IdentifierEventRequest{Identifier: ticker("XLON", "VOD"), Period: history}, refused: true},
		{name: "a preferred share", req: market.IdentifierEventRequest{Identifier: ticker("XNYS", "BACpL"), Period: history}, refused: true},
		{name: "an ISIN", req: market.IdentifierEventRequest{Identifier: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}, Period: history}, refused: true},
		{name: "a lookup before Massive's data starts", req: market.IdentifierEventRequest{Identifier: ticker("XNAS", "AAPL"), Period: types.Period{From: day(2002, 6, 3), Before: day(2002, 6, 4)}}, refused: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := e.Serves(tc.req)
			if tc.refused {
				if err == nil {
					t.Errorf("Serves() = %v, want a refusal", got)
				}
				return
			}
			require.NoError(t, err)
			if got != tc.want {
				t.Errorf("Serves() = %v, want %v", got, tc.want)
			}
			if c := callOf(tc.req); c != tc.call {
				t.Errorf("callOf() = %d, want %d", c, tc.call)
			}
		})
	}
}

// TestChain checks the body of the ticker events call.
func TestChain(t *testing.T) {
	change := func(ticker, date string) event {
		e := event{Type: "ticker_change", Date: date}
		e.TickerChange.Ticker = ticker
		return e
	}
	tests := []struct {
		cassette string
		id       types.Identifier
		want     []event
	}{
		{cassette: "massive_events_ticker_change", id: meta, want: []event{change("META", "2022-06-09"), change("FB", "2012-05-18")}},
		{cassette: "massive_events_unchanged", id: apple[1], want: []event{change("AAPL", "2003-09-10")}},
		{cassette: "massive_events_unknown", id: twitter},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette))
			got, err := c.chain(context.Background(), tc.id)
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("chain(%v) mismatch (-want +got):\n%s", tc.id, diff)
			}
		})
	}
}

// TestTickerOnDay checks the body of the point in time lookup. On 2021-06-01 FB was
// Facebook's ticker and META was not yet in use.
func TestTickerOnDay(t *testing.T) {
	tests := []struct {
		cassette string
		ticker   string
		want     *record
		unknown  bool
	}{
		{cassette: "massive_point_in_time", ticker: "FB", want: &record{
			Ticker: "FB", Market: "stocks", PrimaryExchange: "XNAS", Type: "CS", CurrencyName: "usd",
			CompositeFIGI: "BBG000MM2P62", ShareClassFIGI: "BBG001SQCQC5",
		}},
		{cassette: "massive_point_in_time_unknown", ticker: "META", unknown: true},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette))
			got, unknown, err := c.ticker(context.Background(), tc.ticker, oneDay.From)
			require.NoError(t, err)
			if diff := cmp.Diff(tc.want, got); diff != "" || unknown != tc.unknown {
				t.Errorf("ticker(%s) unknown %v, want %v (-want +got):\n%s", tc.ticker, unknown, tc.unknown, diff)
			}
		})
	}
}

// TestClassifyBody checks the 403 and 404 bodies Massive cannot be made to
// send on demand.
func TestClassifyBody(t *testing.T) {
	c := newClient(t)
	tests := []struct {
		name string
		// code is the status of the answer, 403 when zero.
		code int
		body string
		want market.Failure
	}{
		{
			name: "dates outside the plan's history",
			body: `{"status":"NOT_AUTHORIZED","message":"Your plan doesn't include this data timeframe. Please upgrade your plan at https://massive.com/pricing"}`,
			want: market.Failure{Scope: market.ScopeRequest},
		},
		{
			name: "an entitlement the plan lacks",
			body: `{"status":"NOT_AUTHORIZED","message":"You are not entitled to this data. Please upgrade your plan at https://massive.com/pricing"}`,
			want: market.Failure{Scope: market.ScopeDatasource},
		},
		{
			name: "a refusal worded in a way the integration does not recognise",
			body: `{"status":"NOT_AUTHORIZED","message":"Access to this resource is restricted."}`,
			want: market.Failure{Scope: market.ScopeKey},
		},
		{name: "a body that is not JSON", body: "forbidden", want: market.Failure{Scope: market.ScopeKey}},
		{name: "an unknown ticker", code: http.StatusNotFound, body: `{"status":"NOT_FOUND","message":"Ticker not found."}`, want: market.Failure{Scope: market.ScopeKey}},
		{name: "a path Massive does not serve", code: http.StatusNotFound, body: "404 page not found", want: market.Failure{Scope: market.ScopeKey}},
		{name: "another error", code: http.StatusNotFound, body: `{"status":"ERROR","error":"Something else"}`, want: market.Failure{Scope: market.ScopeKey}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code := tc.code
			if code == 0 {
				code = http.StatusForbidden
			}
			err := market.StatusError{Provider: "massive", Code: code, Body: tc.body}
			if got := c.Classify(err); got != tc.want {
				t.Errorf("Classify() = %+v, want %+v", got, tc.want)
			}
		})
	}
}
