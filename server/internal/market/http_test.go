package market

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestStatusErrorText(t *testing.T) {
	tests := []struct {
		name string
		err  StatusError
		want string
	}{
		{name: "known status with a body", err: StatusError{Provider: "openfigi", Code: 429, Body: `{"error":"slow down"}`}, want: `openfigi returned too many requests: {"error":"slow down"}`},
		{name: "known status alone", err: StatusError{Provider: "massive", Code: 500}, want: "massive returned internal server error"},
		{name: "unknown status", err: StatusError{Provider: "massive", Code: 599, Body: "x"}, want: "massive returned 599: x"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.err.Error(); got != tc.want {
				t.Errorf("%+v.Error() = %q, want %q", tc.err, got, tc.want)
			}
		})
	}
}

func get(t *testing.T, url string, v any) error {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
	require.NoError(t, err)
	return Do(http.DefaultClient, req, "acme", v)
}

func TestDo(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/ok" {
			_, _ = w.Write([]byte(`{"name": "AAPL"}`))
			return
		}
		w.Header().Set("Retry-After", "30")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(" " + strings.Repeat("x", 600)))
	}))
	t.Cleanup(srv.Close)

	var got struct {
		Name string `json:"name"`
	}
	require.NoError(t, get(t, srv.URL+"/ok", &got))
	if got.Name != "AAPL" {
		t.Errorf("Do() decoded name %q, want AAPL", got.Name)
	}

	err := get(t, srv.URL+"/refused", &got)
	var status StatusError
	if !errors.As(err, &status) {
		t.Fatalf("Do() against a 429 = %v, want a StatusError", err)
	}
	if status.Provider != "acme" || status.Code != http.StatusTooManyRequests || status.Header.Get("Retry-After") != "30" {
		t.Errorf("StatusError = %+v, want acme, 429 and the Retry-After header", status)
	}
	if status.Body != strings.Repeat("x", 511) {
		t.Errorf("StatusError body has %d bytes, want the first %d trimmed", len(status.Body), bodyLimit)
	}
}

// TestDoTransportError checks that a failed call does not quote a URL, whose
// query may carry a credential.
func TestDoTransportError(t *testing.T) {
	srv := httptest.NewServer(http.NotFoundHandler())
	srv.Close()
	err := get(t, srv.URL+"/x?apiKey=secret", nil)
	if err == nil {
		t.Fatal("Do() against a closed server succeeded")
	}
	if strings.Contains(err.Error(), "secret") || !strings.HasPrefix(err.Error(), "acme: ") {
		t.Errorf("Do() error = %q, want the provider's name and no URL", err)
	}
}

func TestEndpoint(t *testing.T) {
	tests := []struct {
		name     string
		endpoint string
		want     string
		wantErr  bool
	}{
		{name: "unset", want: "https://default.test"},
		{name: "set", endpoint: "http://proxy:8080", want: "http://proxy:8080"},
		{name: "relative", endpoint: "api.example.com", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := Endpoint(Config{Name: "acme", Endpoint: tc.endpoint}, "https://default.test")
			if (err != nil) != tc.wantErr {
				t.Fatalf("Endpoint(%q) error = %v, wantErr %v", tc.endpoint, err, tc.wantErr)
			}
			if got != tc.want {
				t.Errorf("Endpoint(%q) = %q, want %q", tc.endpoint, got, tc.want)
			}
		})
	}
}
