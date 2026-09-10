package vcr_test

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"

	"github.com/leedenison/stonks/server/internal/testutil/vcr"
)

// What the provider is given and what it hands back. Both are invented, and
// both have to be gone from the recording.
const (
	apiKey    = "live-key-4d5e6f"
	accountNo = "ACC-000123"
)

// scrub is the declaration a client of this provider makes: its key rides in
// the api_token parameter and in a vendor header, and its responses name an
// account.
var scrub = vcr.Scrub{
	Query:           []string{"api_token"},
	ResponseHeaders: []string{"X-Provider-Trace"},
	Body: func(body string) string {
		return strings.ReplaceAll(body, accountNo, vcr.Placeholder)
	},
}

func provider(t *testing.T) *httptest.Server {
	t.Helper()
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Provider-Trace", "trace-1")
		w.Header().Set("Set-Cookie", "session=abc")
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/v1/holdings" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		if _, err := io.WriteString(w, `{"account":"`+accountNo+`","units":"120"}`); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

func get(t *testing.T, c *http.Client, base, path, key string) *http.Response {
	t.Helper()
	u := base + path + "?" + url.Values{"api_token": {key}}.Encode()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	require.NoError(t, err)
	req.Header.Set("X-Provider-Key", key)
	resp, err := c.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	return resp
}

func TestRecordThenReplay(t *testing.T) {
	dir := t.TempDir()
	name := "holdings_found"
	path := filepath.Join(dir, name)
	server := provider(t)

	t.Run("record", func(t *testing.T) {
		t.Setenv("STONKS_RECORD", name)
		t.Setenv("PROVIDER_API_KEY", apiKey)
		if !vcr.Recording(path) {
			t.Fatalf("Recording(%q) = false, want true when STONKS_RECORD names it", path)
		}
		if got := vcr.Credential(t, "PROVIDER_API_KEY", path); got != apiKey {
			t.Errorf("Credential() while recording = %q, want the environment value %q", got, apiKey)
		}

		resp := get(t, vcr.New(t, path, scrub), server.URL, "/v1/holdings", apiKey)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("provider answered %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		if !strings.Contains(string(body), accountNo) {
			t.Errorf("live response = %q, want it to name the account", body)
		}
	})

	saved, err := os.ReadFile(path + ".yaml")
	require.NoError(t, err)

	t.Run("redacts", func(t *testing.T) {
		for _, secret := range []string{apiKey, accountNo, "session=abc", "trace-1"} {
			if strings.Contains(string(saved), secret) {
				t.Errorf("cassette holds %q, which must not reach disk", secret)
			}
		}
		if !strings.Contains(string(saved), vcr.Placeholder) {
			t.Errorf("cassette holds no %s, so nothing was redacted", vcr.Placeholder)
		}
		// The allowlist drops a vendor header this package has never met.
		if strings.Contains(string(saved), "X-Provider-Key") {
			t.Error("cassette holds the X-Provider-Key header, which is outside the allowlist")
		}
	})

	t.Run("replay", func(t *testing.T) {
		if got := vcr.Credential(t, "PROVIDER_API_KEY", path); got != vcr.Placeholder {
			t.Errorf("Credential() while replaying = %q, want %q", got, vcr.Placeholder)
		}
		// The provider is closed, so an answer can only have come from the
		// cassette.
		server.Close()
		resp := get(t, vcr.New(t, path, scrub), server.URL, "/v1/holdings", vcr.Placeholder)
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("replayed status = %d, want 200", resp.StatusCode)
		}
		body, err := io.ReadAll(resp.Body)
		require.NoError(t, err)
		want := `{"account":"` + vcr.Placeholder + `","units":"120"}`
		if string(body) != want {
			t.Errorf("replayed body = %q, want %q", body, want)
		}
		if got := resp.Header.Get("X-Provider-Trace"); got != "" {
			t.Errorf("replayed X-Provider-Trace = %q, want it dropped", got)
		}
		// Redaction shortened the body, so the recorded length was restated.
		if resp.ContentLength != int64(len(want)) {
			t.Errorf("replayed Content-Length = %d, want %d", resp.ContentLength, len(want))
		}
	})
}

func TestUnmatchedRequestFails(t *testing.T) {
	dir := t.TempDir()
	name := "holdings_unmatched"
	path := filepath.Join(dir, name)
	server := provider(t)

	t.Run("record", func(t *testing.T) {
		t.Setenv("STONKS_RECORD", name)
		get(t, vcr.New(t, path, scrub), server.URL, "/v1/holdings", apiKey)
	})

	c := vcr.New(t, path, scrub)
	u := server.URL + "/v1/prices?" + url.Values{"api_token": {vcr.Placeholder}}.Encode()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, u, nil)
	require.NoError(t, err)
	if _, err := c.Do(req); !errors.Is(err, cassette.ErrInteractionNotFound) {
		t.Errorf("a request the cassette does not cover: err = %v, want ErrInteractionNotFound", err)
	}
}

func TestScrubMustBeDeclared(t *testing.T) {
	// NoScrub is a declaration, so it is accepted where a nil Body is not.
	if vcr.NoScrub.Body == nil {
		t.Error("NoScrub.Body is nil, so it does not declare anything")
	}
	if got := vcr.NoScrub.Body(accountNo); got != accountNo {
		t.Errorf("NoScrub.Body(%q) = %q, want it unchanged", accountNo, got)
	}
}

func echo(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("read request: %v", err)
			return
		}
		if _, err := w.Write(body); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

func post(t *testing.T, c *http.Client, url, body string) string {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, url, strings.NewReader(body))
	require.NoError(t, err)
	resp, err := c.Do(req)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, resp.Body.Close()) })
	got, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	return string(got)
}

func TestMatchBody(t *testing.T) {
	dir := t.TempDir()
	name := "quotes_by_body"
	path := filepath.Join(dir, name)
	server := echo(t)
	scrub := vcr.Scrub{Body: vcr.NoScrub.Body, MatchBody: true}

	t.Run("record", func(t *testing.T) {
		t.Setenv("STONKS_RECORD", name)
		c := vcr.New(t, path, scrub)
		post(t, c, server.URL+"/v1/quotes", `{"ticker":"ZZALPHA"}`)
		post(t, c, server.URL+"/v1/quotes", `{"ticker":"ZZBETA"}`)
	})

	server.Close()
	c := vcr.New(t, path, scrub)
	// Asked in the other order, so a match on the URL alone would answer with
	// the wrong interaction.
	if got := post(t, c, server.URL+"/v1/quotes", `{"ticker":"ZZBETA"}`); got != `{"ticker":"ZZBETA"}` {
		t.Errorf("replayed body = %q, want the ZZBETA interaction", got)
	}
	if got := post(t, c, server.URL+"/v1/quotes", `{"ticker":"ZZALPHA"}`); got != `{"ticker":"ZZALPHA"}` {
		t.Errorf("replayed body = %q, want the ZZALPHA interaction", got)
	}
}
