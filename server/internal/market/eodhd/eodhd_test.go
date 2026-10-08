package eodhd

import (
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
)

// mics is a MIC table holding the venues the tests name.
var mics = mic.Table{
	"XNYS": "XNYS", "ARCX": "XNYS", "XASE": "XNYS", "XNAS": "XNAS", "XNGS": "XNAS",
	"BATS": "BATS", "XCBO": "XCBO", "XLON": "XLON", "XETR": "XETR", "OTCM": "OTCM",
}

func ticker(venue, value string) types.Identifier {
	return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value}
}

func client(t *testing.T) *Client {
	t.Helper()
	c, err := New(market.Config{Name: "eodhd", Credential: "k"}, mics)
	require.NoError(t, err)
	return c
}

func TestNew(t *testing.T) {
	refused := []struct {
		name string
		cfg  market.Config
	}{
		{name: "no credential", cfg: market.Config{Name: "eodhd"}},
		{name: "an endpoint that is not a URL", cfg: market.Config{Name: "eodhd", Credential: "k", Endpoint: "eodhd.com"}},
	}
	for _, tc := range refused {
		if _, err := New(tc.cfg, mics); err == nil {
			t.Errorf("New() with %s succeeded, want an error", tc.name)
		}
	}
	c := client(t)
	if limit, burst := c.Limit(); limit != rate.Every(3*time.Minute/1000) || burst != 1 {
		t.Errorf("Limit() = %v, burst %d, want 1,000 a minute shared by three requests, burst 1", limit, burst)
	}
	if got := c.Endpoint(); got != endpoint {
		t.Errorf("Endpoint() = %q, want %q", got, endpoint)
	}
}

func TestClassify(t *testing.T) {
	status := func(code int, header http.Header) error {
		return market.StatusError{Provider: "eodhd", Code: code, Header: header}
	}
	tests := []struct {
		name string
		err  error
		want market.Failure
	}{
		{name: "a refused key", err: status(http.StatusUnauthorized, nil), want: market.Failure{Scope: gen.BlockScopeDatasource}},
		{name: "a symbol the plan does not cover", err: status(http.StatusForbidden, nil), want: market.Failure{Scope: gen.BlockScopeIdentifier}},
		{name: "a rate limit with a delay", err: status(http.StatusTooManyRequests, http.Header{"Retry-After": {"30"}}), want: market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier, RetryAfter: 30 * time.Second}},
		{name: "a rate limit without a delay", err: status(http.StatusTooManyRequests, nil), want: market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}},
		{name: "an outage", err: status(http.StatusBadGateway, nil), want: market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}},
		{name: "a bad request", err: status(http.StatusBadRequest, nil), want: market.Failure{Scope: gen.BlockScopeIdentifier}},
		{name: "a transport error", err: errors.New("eodhd: connection reset"), want: market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}},
	}
	c := client(t)
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := c.Classify(tc.err); got != tc.want {
				t.Errorf("Classify(%v) = %+v, want %+v", tc.err, got, tc.want)
			}
		})
	}
}

// TestClassifyQuota checks that a spent daily quota pauses the datasource
// until EODHD resets it.
func TestClassifyQuota(t *testing.T) {
	got := client(t).Classify(market.StatusError{Provider: "eodhd", Code: http.StatusPaymentRequired})
	if !got.Temporary || got.Scope != gen.BlockScopeDatasource || got.RetryAfter <= 0 || got.RetryAfter > 24*time.Hour {
		t.Errorf("Classify(402) = %+v, want temporary for the datasource, until midnight UTC", got)
	}
}

func TestUntilMidnight(t *testing.T) {
	day := time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC)
	tests := []struct {
		now  time.Time
		want time.Duration
	}{
		{now: day, want: 24 * time.Hour},
		{now: day.Add(12 * time.Hour), want: 12 * time.Hour},
		{now: day.Add(24*time.Hour - time.Second), want: time.Second},
		{now: day.Add(12 * time.Hour).In(time.FixedZone("UTC-5", -5*3600)), want: 12 * time.Hour},
	}
	for _, tc := range tests {
		if got := untilMidnight(tc.now); got != tc.want {
			t.Errorf("untilMidnight(%s) = %s, want %s", tc.now, got, tc.want)
		}
	}
}

// TestExchanges checks that every code listing an operating MIC is kept, as
// where EODHD splits an exchange by segment.
func TestExchanges(t *testing.T) {
	tbl := mic.Table{"XKRX": "XKRX", "XKOS": "XKRX", "XNAS": "XNAS", "BATS": "BATS", "XLON": "XLON"}
	got := exchanges(tbl)
	want := map[string][]string{"XKRX": {"KO", "KQ"}, "XNAS": {"US"}, "BATS": {"US"}, "XLON": {"LSE"}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("exchanges() mismatch (-want +got):\n%s", diff)
	}
}
