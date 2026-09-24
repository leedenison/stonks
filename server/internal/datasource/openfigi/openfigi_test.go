package openfigi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/datasource"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/testutil/vcr"
)

// scrub declares that OpenFIGI's bodies carry only public security
// identifiers, and that its requests differ only in their bodies. The
// cassettes are recorded without a credential.
var scrub = vcr.Scrub{Body: func(body string) string { return body }, MatchBody: true}

func replay(t *testing.T, cassette, key string) *Client {
	t.Helper()
	return New(datasource.Config{Credential: key}, mics, WithHTTPClient(vcr.New(t, "testdata/"+cassette, scrub)))
}

var (
	appleShareClass = types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S5N8V8"}
	appleNasdaq     = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"}
	appleNYSE       = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "AAPL"}
)

// fetch asks c about sent for a key stating currency, as the framework would.
func fetch(c *Client, sent types.Identifier, currency string) ([]datasource.FetchResponse[datasource.IdentityResult], error) {
	req := datasource.FetchRequest[datasource.StatedKey]{Value: datasource.StatedKey{Currency: currency}, Sent: sent}
	return c.Fetch(context.Background(), []datasource.FetchRequest[datasource.StatedKey]{req})
}

// names reports whether some candidate returns id.
func names(r datasource.IdentityResult, id types.Identifier) bool {
	return slices.ContainsFunc(r.Candidates, func(c datasource.Candidate) bool {
		return slices.Contains(c.Identifiers, id)
	})
}

// TestFetchListings checks what OpenFIGI answered for identifiers it maps.
func TestFetchListings(t *testing.T) {
	tests := []struct {
		cassette string
		sent     types.Identifier
		currency string
		// every is named by every candidate; some by at least one.
		every []types.Identifier
		some  []types.Identifier
		n     int
		// class is every candidate's, where the call names one instrument.
		class gen.AssetClass
	}{
		{
			cassette: "isin",
			sent:     types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"},
			some:     []types.Identifier{appleShareClass, appleNasdaq, appleNYSE},
			n:        277,
			class:    gen.AssetClassStock,
		},
		{
			cassette: "cusip",
			sent:     types.Identifier{Type: types.IdentifierTypeCusip, Value: "037833100"},
			some:     []types.Identifier{appleShareClass, appleNasdaq, appleNYSE},
			n:        277,
			class:    gen.AssetClassStock,
		},
		{
			cassette: "bare_ticker",
			sent:     types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "AAPL"},
			some:     []types.Identifier{appleShareClass, appleNasdaq, appleNYSE},
			n:        74,
		},
		{
			cassette: "mic_ticker",
			sent:     types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"},
			every: []types.Identifier{
				{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG001S6PJ31"},
				{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000C6K5W3"},
				{Type: types.IdentifierTypeOpenfigiTicker, Domain: "LN", Value: "VOD"},
				{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"},
			},
			n:     1,
			class: gen.AssetClassStock,
		},
		{
			cassette: "currency",
			sent:     types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB00BH4HKS39"},
			currency: "GBX",
			some:     []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}},
			n:        31,
		},
		{
			cassette: "not_found",
			sent:     types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000002"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			got, err := fetch(replay(t, tc.cassette, ""), tc.sent, tc.currency)
			if err != nil {
				t.Fatalf("Fetch(%v) error = %v", tc.sent, err)
			}
			if len(got) != 1 {
				t.Fatalf("Fetch(%v) answered %d results, want 1", tc.sent, len(got))
			}
			if got[0].Err != nil {
				t.Fatalf("Fetch(%v) job error = %v", tc.sent, got[0].Err)
			}
			r := got[0].Value
			if diff := cmp.Diff([]types.Identifier{tc.sent}, r.Filtered); diff != "" {
				t.Errorf("Fetch(%v) filtered (-want +got):\n%s", tc.sent, diff)
			}
			if len(r.Candidates) != tc.n {
				t.Errorf("Fetch(%v) answered %d candidates, want %d", tc.sent, len(r.Candidates), tc.n)
			}
			for _, c := range r.Candidates {
				if c.Currency != tc.currency {
					t.Errorf("Fetch(%v) candidate %v currency = %q, want %q", tc.sent, c.Identifiers, c.Currency, tc.currency)
				}
				if tc.class != "" && c.Class != tc.class {
					t.Errorf("Fetch(%v) candidate %v class = %s, want %s", tc.sent, c.Identifiers, c.Class, tc.class)
				}
				for _, id := range tc.every {
					if !slices.Contains(c.Identifiers, id) {
						t.Errorf("Fetch(%v) candidate %v does not name %v", tc.sent, c.Identifiers, id)
					}
				}
			}
			for _, id := range tc.some {
				if !names(r, id) {
					t.Errorf("Fetch(%v) no candidate names %v", tc.sent, id)
				}
			}
		})
	}
}

// TestFetchRefused checks how the refusals OpenFIGI recorded are classified.
func TestFetchRefused(t *testing.T) {
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	tests := []struct {
		cassette string
		key      string
		sent     types.Identifier
		want     datasource.Failure
	}{
		{
			cassette: "invalid_value",
			sent:     types.Identifier{Type: types.IdentifierTypeCusip, Value: "!!!"},
			want:     datasource.Failure{Scope: gen.BlockScopeIdentifier},
		},
		{
			cassette: "unauthorised",
			key:      "not-a-key",
			sent:     isin,
			want:     datasource.Failure{Scope: gen.BlockScopeDatasource},
		},
	}
	for _, tc := range tests {
		t.Run(tc.cassette, func(t *testing.T) {
			c := replay(t, tc.cassette, tc.key)
			got, err := fetch(c, tc.sent, "")
			if err == nil {
				if len(got) != 1 || got[0].Err == nil {
					t.Fatalf("Fetch(%v) = %+v, want a refusal", tc.sent, got)
				}
				err = got[0].Err
			}
			if fail := c.Classify(err); fail != tc.want {
				t.Errorf("Classify(%v) = %+v, want %+v", err, fail, tc.want)
			}
		})
	}
}

// TestClassifyStatus checks the statuses OpenFIGI cannot be made to send on
// demand.
func TestClassifyStatus(t *testing.T) {
	tests := []struct {
		name   string
		status int
		reset  string
		want   datasource.Failure
	}{
		{
			name: "rate limited", status: http.StatusTooManyRequests, reset: "12",
			want: datasource.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier, RetryAfter: 12 * time.Second},
		},
		{
			name: "unavailable", status: http.StatusServiceUnavailable,
			want: datasource.Failure{Temporary: true, Scope: gen.BlockScopeIdentifier},
		},
		{
			name: "malformed", status: http.StatusBadRequest,
			want: datasource.Failure{Scope: gen.BlockScopeDatasource},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				if tc.reset != "" {
					w.Header().Set("Ratelimit-Reset", tc.reset)
				}
				w.WriteHeader(tc.status)
			}))
			t.Cleanup(srv.Close)
			c := New(datasource.Config{Endpoint: srv.URL}, mics)

			_, err := fetch(c, types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}, "")
			if err == nil {
				t.Fatalf("Fetch() against status %d succeeded, want an error", tc.status)
			}
			if got := c.Classify(err); got != tc.want {
				t.Errorf("Classify(%v) = %+v, want %+v", err, got, tc.want)
			}
		})
	}
}

// TestLimits checks that a credential buys OpenFIGI's larger allowance.
func TestLimits(t *testing.T) {
	for _, tc := range []struct {
		key   string
		every time.Duration
		batch int
	}{
		{key: "", every: time.Minute / 25, batch: 10},
		{key: "k", every: 6 * time.Second / 25, batch: 100},
	} {
		c := New(datasource.Config{Credential: tc.key}, mics)
		limit, burst := c.Limit()
		if limit != rate.Every(tc.every) || burst != 1 {
			t.Errorf("Limit() with credential %q = %v, burst %d, want one per %s, burst 1", tc.key, limit, burst, tc.every)
		}
		if got := c.Batch(); got != tc.batch {
			t.Errorf("Batch() with credential %q = %d, want %d", tc.key, got, tc.batch)
		}
	}
}
