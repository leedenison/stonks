package massive

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
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

// conf is the config the dev seed writes, with the plans Massive sells.
const conf = `{"plan": "basic", "plans": {"basic": {"perMinute": 5}, "starter": {}, "developer": {}, "advanced": {}}}`

// mics is a MIC table holding the venues the tests name.
var mics = mic.Table{
	"XNYS": "XNYS", "ARCX": "XNYS", "XASE": "XNYS", "XNAS": "XNAS", "XNGS": "XNAS",
	"BATS": "BATS", "XLON": "XLON",
}

// scrub removes the API key from every URL and the issuer's address,
// telephone number and description from every record. The massive.vcr entry
// of docker/vcrproxy/hosts.json redacts the same fields, and a field that
// one of them gains belongs in both.
var scrub = vcr.Scrub{Query: []string{"apiKey"}, Body: vcr.RedactFields("address", "phone_number", "description")}

func replay(t *testing.T, cassette, key string) *Client {
	t.Helper()
	c, err := New(market.Config{Credential: key, JSON: json.RawMessage(conf)}, mics, WithHTTPClient(vcr.New(t, "testdata/"+cassette, scrub)))
	require.NoError(t, err)
	return c
}

func fetch(c *Client, sent types.Identifier) ([]market.Response[market.IdentityResult], error) {
	return c.Fetch(context.Background(), []market.Request[gen.StatedKey]{{Sent: sent}})
}

var (
	cusip  = types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"}
	apple  = []types.Identifier{{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S5N8V8"}, {Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000B9XRY4"}, {Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"}}
	ticker = func(venue, value string) types.Identifier {
		return types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value}
	}
)

// TestFetch checks what Massive returned for the identifiers it is sent.
func TestFetch(t *testing.T) {
	tests := []struct {
		cassette string
		sent     types.Identifier
		// filtered is the identifier the call filters on. It defaults to
		// sent.
		filtered types.Identifier
		want     []market.Candidate
	}{
		{
			cassette: "massive_ticker",
			sent:     ticker("XNAS", "AAPL"),
			filtered: ticker("", "AAPL"),
			want:     []market.Candidate{{Class: gen.AssetClassStock, Currency: "USD", Identifiers: apple}},
		},
		{
			cassette: "massive_class_share",
			sent:     ticker("", "BRK/B"),
			want: []market.Candidate{{Class: gen.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{
				{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S90346"},
				{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000DWG505"},
				ticker("XNYS", "BRK.B"),
			}}},
		},
		{
			cassette: "massive_cusip",
			sent:     cusip,
			want:     []market.Candidate{{Class: gen.AssetClassStock, Currency: "USD", Identifiers: apple}},
		},
		{
			// Bank of America's Series L preferred, which Massive spells BACpL
			// and records without FIGIs.
			cassette: "massive_preferred",
			sent:     types.Identifier{Type: types.IdentifierTypeCusip, Value: "060505682"},
			want:     []market.Candidate{{Class: gen.AssetClassStock, Currency: "USD"}},
		},
		{
			cassette: "massive_unknown_ticker",
			sent:     ticker("", "ZZZZQQ"),
		},
		{
			// An invented CUSIP, which no security holds.
			cassette: "massive_unknown_cusip",
			sent:     types.Identifier{Type: types.IdentifierTypeCusip, Value: "99999Z999"},
		},
		{
			// Tencent's ADR trades over the counter.
			cassette: "massive_otc",
			sent:     ticker("", "TCEHY"),
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+tc.cassette))
			got, err := fetch(c, tc.sent)
			require.NoError(t, err)
			if tc.filtered == (types.Identifier{}) {
				tc.filtered = tc.sent
			}
			want := []market.Response[market.IdentityResult]{{Value: market.IdentityResult{
				Filtered: []types.Identifier{tc.filtered}, Candidates: tc.want, Limited: true,
			}}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Fetch(%v) mismatch (-want +got):\n%s", tc.sent, diff)
			}
		})
	}
}

// TestFetchUnauthorised checks that a refused API key blocks the
// datasource. The recording is made with a key Massive does not hold.
func TestFetchUnauthorised(t *testing.T) {
	const cassette = "massive_unauthorised"
	key := vcr.Placeholder
	if vcr.Recording(cassette) {
		key = "not-a-key"
	}
	c := replay(t, cassette, key)
	_, err := fetch(c, ticker("", "AAPL"))
	if err == nil {
		t.Fatal("Fetch() with a refused key succeeded")
	}
	if got, want := c.Classify(err), (market.Failure{Scope: gen.BlockScopeDatasource}); got != want {
		t.Errorf("Classify(%v) = %+v, want %+v", err, got, want)
	}
}

// unconverted holds the stock type codes whose records this integration
// drops, such as warrants, rights, units and bonds.
var unconverted = map[string]bool{
	"WARRANT": true, "RIGHT": true, "BOND": true, "SP": true, "ADRW": true,
	"UNIT": true, "LT": true, "OTHER": true, "AGEN": true, "EQLK": true,
}

// TestTypes checks that every stock type Massive lists is either converted
// or declared unconverted.
func TestTypes(t *testing.T) {
	const cassette = "massive_types"
	h := vcr.New(t, "testdata/"+cassette, scrub)
	q := url.Values{"asset_class": {"stocks"}, "locale": {"us"}, "apiKey": {vcr.Credential(t, "MASSIVE_API_KEY", "testdata/"+cassette)}}
	resp, err := h.Get(endpoint + "/v3/reference/tickers/types?" + q.Encode())
	require.NoError(t, err)
	defer func() { require.NoError(t, resp.Body.Close()) }()
	var page struct {
		Results []struct {
			Code string `json:"code"`
		} `json:"results"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&page))
	if len(page.Results) == 0 {
		t.Fatal("Massive listed no types")
	}
	for _, r := range page.Results {
		if _, ok := classes[r.Code]; !ok && !unconverted[r.Code] {
			t.Errorf("type %s is neither converted nor declared unconverted", r.Code)
		}
	}
}

// TestClassifyStatus checks the statuses Massive cannot be made to send on
// demand.
func TestClassifyStatus(t *testing.T) {
	tests := []struct {
		status int
		want   market.Failure
	}{
		{status: http.StatusForbidden, want: market.Failure{Scope: gen.BlockScopeDatasource}},
		{status: http.StatusTooManyRequests, want: market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}},
		{status: http.StatusBadGateway, want: market.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier}},
		{status: http.StatusBadRequest, want: market.Failure{Scope: gen.BlockScopeIdentifier}},
	}
	for _, tc := range tests {
		t.Run(http.StatusText(tc.status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)
			c, err := New(market.Config{Credential: "k", Endpoint: srv.URL, JSON: json.RawMessage(conf)}, mics)
			require.NoError(t, err)
			_, err = fetch(c, cusip)
			if err == nil {
				t.Fatalf("Fetch() against status %d succeeded, want an error", tc.status)
			}
			if got := c.Classify(err); got != tc.want {
				t.Errorf("Classify(%v) = %+v, want %+v", err, got, tc.want)
			}
		})
	}
}

// TestNotFound checks that a ticker Massive does not know gets an empty
// answer, and that a 404 from a path Massive does not serve is an error.
func TestNotFound(t *testing.T) {
	tests := []struct {
		name  string
		body  string
		fails bool
	}{
		{name: "an unknown ticker", body: `{"status":"NOT_FOUND","message":"Ticker not found."}`},
		{name: "a path Massive does not serve", body: "404 page not found", fails: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotFound)
				_, _ = w.Write([]byte(tc.body))
			}))
			t.Cleanup(srv.Close)
			c, err := New(market.Config{Credential: "k", Endpoint: srv.URL, JSON: json.RawMessage(conf)}, mics)
			require.NoError(t, err)
			got, err := fetch(c, ticker("", "ZZZZQQ"))
			if tc.fails {
				if err == nil {
					t.Errorf("Fetch() = %+v, want an error", got)
				}
				return
			}
			require.NoError(t, err)
			if len(got) != 1 || len(got[0].Value.Candidates) != 0 {
				t.Errorf("Fetch() = %+v, want one empty answer", got)
			}
		})
	}
}

// TestConfig checks the rate each plan gives and the configs New refuses.
func TestConfig(t *testing.T) {
	limit := func(t *testing.T, plan string) rate.Limit {
		t.Helper()
		var v map[string]any
		require.NoError(t, json.Unmarshal([]byte(conf), &v))
		v["plan"] = plan
		b, err := json.Marshal(v)
		require.NoError(t, err)
		c, err := New(market.Config{Credential: "k", JSON: b}, mics)
		require.NoError(t, err)
		l, burst := c.Limit()
		if burst != 1 {
			t.Errorf("burst = %d, want 1", burst)
		}
		return l
	}
	if got := limit(t, "basic"); got != rate.Every(12*time.Second) {
		t.Errorf("Limit() on basic = %v, want one per 12s", got)
	}
	if got := limit(t, "starter"); got != rate.Inf {
		t.Errorf("Limit() on starter = %v, want unlimited", got)
	}

	refused := []struct {
		name string
		cfg  market.Config
	}{
		{name: "no credential", cfg: market.Config{JSON: json.RawMessage(conf)}},
		{name: "a plan it does not describe", cfg: market.Config{Credential: "k", JSON: json.RawMessage(`{"plan": "gold", "plans": {"basic": {}}}`)}},
		{name: "an unknown member", cfg: market.Config{Credential: "k", JSON: json.RawMessage(`{"plan": "basic", "plans": {"basic": {"per_minute": 5}}}`)}},
		{name: "not an object", cfg: market.Config{Credential: "k", JSON: json.RawMessage(`[]`)}},
		{name: "a negative rate", cfg: market.Config{Credential: "k", JSON: json.RawMessage(`{"plan": "basic", "plans": {"basic": {"perMinute": -1}}}`)}},
		{name: "an endpoint that is not a URL", cfg: market.Config{Credential: "k", Endpoint: "api.massive.com", JSON: json.RawMessage(conf)}},
	}
	for _, tc := range refused {
		if _, err := New(tc.cfg, mics); err == nil {
			t.Errorf("New() with %s succeeded, want an error", tc.name)
		}
	}
}

func TestStatusErrorText(t *testing.T) {
	tests := []struct {
		err  statusError
		want string
	}{
		{err: statusError{code: 429, body: `{"status":"ERROR"}`}, want: `massive returned too many requests: {"status":"ERROR"}`},
		{err: statusError{code: 500}, want: "massive returned internal server error"},
		{err: statusError{code: 599, body: "x"}, want: "massive returned 599: x"},
	}
	for _, tc := range tests {
		if got := tc.err.Error(); got != tc.want {
			t.Errorf("statusError{%d, %q}.Error() = %q, want %q", tc.err.code, tc.err.body, got, tc.want)
		}
	}
}
