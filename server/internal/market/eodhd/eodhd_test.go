package eodhd

import (
	"context"
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
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
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
	if limit, burst := c.Limit(); limit != rate.Every(4*time.Minute/1000) || burst != 1 {
		t.Errorf("Limit() = %v, burst %d, want 1,000 a minute shared by four requests, burst 1", limit, burst)
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

// scrub removes the API key from every URL. EODHD's reference data carries
// nothing else that is private.
var scrub = vcr.Scrub{Query: []string{"api_token"}, Body: vcr.NoScrub.Body}

func replay(t *testing.T, cassette, key string) *Client {
	t.Helper()
	c, err := New(market.Config{Name: "eodhd", Credential: key}, mics, WithHTTPClient(vcr.New(t, "testdata/"+cassette, scrub)))
	require.NoError(t, err)
	return c
}

func fetch(c *Client, sent types.Identifier) ([]market.Response[market.IdentityResult], error) {
	return c.Fetch(context.Background(), []market.Request[gen.StatedKey]{{Sent: sent}})
}

// bySymbol returns the candidate whose DATASOURCE_TICKER is symbol.
func bySymbol(r market.IdentityResult, symbol string) (market.Candidate, bool) {
	for _, c := range r.Candidates {
		for _, id := range c.Identifiers {
			if id.Type == types.IdentifierTypeDatasourceTicker && id.Value == symbol {
				return c, true
			}
		}
	}
	return market.Candidate{}, false
}

var (
	appleISIN = types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	apple     = market.Candidate{Class: gen.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{
		{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: "AAPL.US"},
		appleISIN,
		{Type: types.IdentifierTypeCusip, Value: "037833100"},
		ticker("XNAS", "AAPL"),
	}}
	vodafone = market.Candidate{Class: gen.AssetClassStock, Currency: "GBX", Primary: "XLON", Identifiers: []types.Identifier{
		{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: "VOD.LSE"},
		{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"},
		ticker("XLON", "VOD"),
	}}
)

// TestFetch checks what EODHD returned for the identifiers it is sent.
func TestFetch(t *testing.T) {
	tests := []struct {
		cassette string
		sent     types.Identifier
		// filtered is the identifier that the call filters by. It defaults
		// to sent.
		filtered types.Identifier
		// want holds candidates the answer must carry, by symbol.
		want map[string]market.Candidate
		// count is the number of candidates. The test MIC table holds a few
		// venues, so a candidate elsewhere does not convert.
		count int
	}{
		{
			// Apple, listed on several of EODHD's exchanges.
			cassette: "eodhd_isin",
			sent:     appleISIN,
			want:     map[string]market.Candidate{"AAPL.US": apple},
			count:    3,
		},
		{
			cassette: "eodhd_cusip",
			sent:     types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"},
			want:     map[string]market.Candidate{"AAPL.US": apple},
			count:    3,
		},
		{
			// Vodafone, on the London Stock Exchange. Its US symbol trades
			// over the counter and does not convert.
			cassette: "eodhd_isin_foreign",
			sent:     types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"},
			want:     map[string]market.Candidate{"VOD.LSE": vodafone},
			count:    2,
		},
		{
			cassette: "eodhd_ticker_venue",
			sent:     ticker("XLON", "VOD"),
			filtered: ticker("", "VOD"),
			want:     map[string]market.Candidate{"VOD.LSE": vodafone},
			count:    1,
		},
		{
			// EODHD maps the symbol to two CUSIPs, and neither is kept.
			cassette: "eodhd_class_share",
			sent:     ticker("XNYS", "BRK.B"),
			filtered: ticker("", "BRK.B"),
			want: map[string]market.Candidate{"BRK-B.US": {Class: gen.AssetClassStock, Currency: "USD", Primary: "XNYS", Identifiers: []types.Identifier{
				{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: "BRK-B.US"},
				{Type: types.IdentifierTypeIsin, Value: "US0846707026"},
				ticker("XNYS", "BRK.B"),
			}}},
			count: 1,
		},
		{
			// The search matches Apple's name in other tickers, which are
			// dropped. The rows that remain carry several ISINs, so
			// id-mapping is not asked.
			cassette: "eodhd_ticker",
			sent:     ticker("", "AAPL"),
			want: map[string]market.Candidate{"AAPL.US": {Class: gen.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{
				{Type: types.IdentifierTypeDatasourceTicker, Domain: domain, Value: "AAPL.US"},
				appleISIN,
				ticker("XNAS", "AAPL"),
			}}},
			count: 1,
		},
		{
			// Tencent's ADR trades over the counter.
			cassette: "eodhd_otc",
			sent:     ticker("", "TCEHY"),
		},
		{
			// An invented ISIN that identifies no security.
			cassette: "eodhd_unknown_isin",
			sent:     types.Identifier{Type: types.IdentifierTypeIsin, Value: "US9999999999"},
		},
		{
			// An invented CUSIP that identifies no security.
			cassette: "eodhd_unknown_cusip",
			sent:     types.Identifier{Type: types.IdentifierTypeCusip, Value: "99999Z999"},
		},
		{
			cassette: "eodhd_unknown_ticker",
			sent:     ticker("", "ZZZZQQ"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "EODHD_API_KEY", "testdata/"+tc.cassette))
			got, err := fetch(c, tc.sent)
			require.NoError(t, err)
			require.Len(t, got, 1)
			res := got[0].Value
			if tc.filtered == (types.Identifier{}) {
				tc.filtered = tc.sent
			}
			if diff := cmp.Diff([]types.Identifier{tc.filtered}, res.Filtered); diff != "" || !res.Limited {
				t.Errorf("Fetch(%v) filtered mismatch (-want +got):\n%s, limited %v", tc.sent, diff, res.Limited)
			}
			if len(res.Candidates) != tc.count {
				t.Errorf("Fetch(%v) gave %d candidates, want %d: %+v", tc.sent, len(res.Candidates), tc.count, res.Candidates)
			}
			for symbol, want := range tc.want {
				got, ok := bySymbol(res, symbol)
				if !ok {
					t.Errorf("Fetch(%v) gave no candidate %s: %+v", tc.sent, symbol, res.Candidates)
					continue
				}
				if diff := cmp.Diff(want, got); diff != "" {
					t.Errorf("Fetch(%v) candidate %s mismatch (-want +got):\n%s", tc.sent, symbol, diff)
				}
			}
		})
	}
}

// TestFetchUnauthorised checks that a refused API key blocks the datasource.
// The recording is made with a key EODHD did not issue.
func TestFetchUnauthorised(t *testing.T) {
	const cassette = "eodhd_unauthorised"
	key := vcr.Placeholder
	if vcr.Recording(cassette) {
		key = "not-a-key"
	}
	c := replay(t, cassette, key)
	_, err := fetch(c, appleISIN)
	if err == nil {
		t.Fatal("Fetch() with a refused key succeeded")
	}
	if got, want := c.Classify(err), (market.Failure{Scope: gen.BlockScopeDatasource}); got != want {
		t.Errorf("Classify(%v) = %+v, want %+v", err, got, want)
	}
}
