package google

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

// provider stands in for Google's authorisation and token endpoints.
type provider struct {
	*httptest.Server
	// refresh answers a refresh: a status and the members of its body.
	refresh func() (int, map[string]any)
	// redirect rewrites the query of the redirect back to the tool.
	redirect func(url.Values)
	// challenge is the PKCE challenge of the last authorisation request.
	challenge atomic.Value
	// spare has the browser hold a second connection to the tool open
	// without a request on it, as a browser's preconnect does.
	spare bool
	// exchanges and refreshes count the calls of each grant.
	exchanges, refreshes atomic.Int32
}

func newProvider(t *testing.T) *provider {
	t.Helper()
	p := &provider{}
	mux := http.NewServeMux()
	mux.HandleFunc("/auth", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("code_challenge_method") != "S256" || q.Get("access_type") != "offline" {
			t.Errorf("authorisation request = %v, want PKCE with S256 and offline access", q)
		}
		for _, scope := range scopes {
			if !strings.Contains(q.Get("scope"), scope) {
				t.Errorf("authorisation request scope = %q, want it to hold %q", q.Get("scope"), scope)
			}
		}
		p.challenge.Store(q.Get("code_challenge"))
		back := url.Values{"code": {"code-1"}, "state": {q.Get("state")}}
		if p.redirect != nil {
			p.redirect(back)
		}
		http.Redirect(w, r, q.Get("redirect_uri")+"?"+back.Encode(), http.StatusFound) //nolint:gosec // the stub redirects as Google does, to the address the tool gave
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, r.ParseForm())
		w.Header().Set("Content-Type", "application/json")
		status, body := http.StatusOK, map[string]any{}
		switch r.PostForm.Get("grant_type") {
		case "authorization_code":
			p.exchanges.Add(1)
			sum := sha256.Sum256([]byte(r.PostForm.Get("code_verifier")))
			if got, want := base64.RawURLEncoding.EncodeToString(sum[:]), p.challenge.Load(); got != want {
				t.Errorf("code verifier hashes to %q, want the challenge %q", got, want)
			}
			if r.PostForm.Get("code") != "code-1" {
				t.Errorf("exchanged code = %q, want code-1", r.PostForm.Get("code"))
			}
			body = map[string]any{"access_token": "access-1", "token_type": "Bearer", "expires_in": 3600, "refresh_token": "refresh-1", "id_token": "id-from-sign-in"}
		case "refresh_token":
			p.refreshes.Add(1)
			status, body = p.refresh()
		default:
			t.Errorf("grant_type = %q", r.PostForm.Get("grant_type"))
		}
		w.WriteHeader(status)
		require.NoError(t, json.NewEncoder(w).Encode(body))
	})
	p.Server = httptest.NewServer(mux)
	t.Cleanup(p.Close)
	p.refresh = func() (int, map[string]any) {
		return http.StatusOK, map[string]any{"access_token": "access-2", "token_type": "Bearer", "expires_in": 3600, "id_token": "id-from-refresh"}
	}
	return p
}

// client writes the client file of a desktop client of p and loads it.
func (p *provider) client(t *testing.T) Client {
	t.Helper()
	path := filepath.Join(t.TempDir(), "client.json")
	b, err := json.Marshal(map[string]any{"installed": map[string]string{
		"client_id": "cli-client-id", "client_secret": "cli-secret", "auth_uri": p.URL + "/auth", "token_uri": p.URL + "/token",
	}})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(path, b, 0o600))
	c, err := LoadClient(path)
	require.NoError(t, err)
	return c
}

// identity runs Identity over dir, and opens the address it prints as the
// browser of an administrator would.
func (p *provider) identity(t *testing.T, dir string) (string, error) {
	t.Helper()
	pr, pw := io.Pipe()
	opened := make(chan struct{})
	go func() {
		defer close(opened)
		lines := bufio.NewScanner(pr)
		for lines.Scan() {
			if !strings.HasPrefix(lines.Text(), "http") {
				continue
			}
			if p.spare {
				p.preconnect(t, lines.Text())
			}
			res, err := http.Get(lines.Text()) //nolint:noctx // the address of the stub provider
			if err != nil {
				t.Errorf("open the sign-in address: %v", err)
				continue
			}
			if err := res.Body.Close(); err != nil {
				t.Error(err)
			}
		}
	}()
	id, err := Identity(context.Background(), p.client(t), dir, "127.0.0.1:0", pw)
	require.NoError(t, pw.Close())
	<-opened
	return id, err
}

// preconnect opens a connection to the redirect address that the sign-in
// address names, and holds it until the test ends.
func (p *provider) preconnect(t *testing.T, address string) {
	t.Helper()
	u, err := url.Parse(address)
	require.NoError(t, err)
	back, err := url.Parse(u.Query().Get("redirect_uri"))
	require.NoError(t, err)
	var d net.Dialer
	conn, err := d.DialContext(context.Background(), "tcp", back.Host)
	require.NoError(t, err)
	t.Cleanup(func() {
		// The tool may already have closed its end.
		if err := conn.Close(); err != nil && !errors.Is(err, net.ErrClosed) {
			t.Error(err)
		}
	})
}

func stored(t *testing.T, dir string) state {
	t.Helper()
	s, err := load(dir)
	require.NoError(t, err)
	return s
}

func TestIdentity(t *testing.T) {
	t.Run("a first sign-in stores the refresh token, readable by its owner alone", func(t *testing.T) {
		p, dir := newProvider(t), filepath.Join(t.TempDir(), "stonks")
		id, err := p.identity(t, dir)
		require.NoError(t, err)
		if id != "id-from-sign-in" {
			t.Errorf("Identity() = %q, want the token of the sign-in", id)
		}
		if got := stored(t, dir); got.RefreshToken != "refresh-1" {
			t.Errorf("stored refresh token = %q, want refresh-1", got.RefreshToken)
		}
		info, err := os.Stat(filepath.Join(dir, stateFile))
		require.NoError(t, err)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("state file mode = %v, want 0600", info.Mode().Perm())
		}
	})
	t.Run("a browser that holds a spare connection open does not fail the sign-in", func(t *testing.T) {
		p, dir := newProvider(t), t.TempDir()
		p.spare = true
		id, err := p.identity(t, dir)
		require.NoError(t, err)
		if id != "id-from-sign-in" || stored(t, dir).RefreshToken != "refresh-1" {
			t.Errorf("Identity() = %q with the stored token %q, want the sign-in to complete", id, stored(t, dir).RefreshToken)
		}
	})
	t.Run("a second run refreshes without a sign-in", func(t *testing.T) {
		p, dir := newProvider(t), t.TempDir()
		require.NoError(t, state{RefreshToken: "refresh-0", SpreadsheetID: "sheet-1"}.save(dir))
		id, err := p.identity(t, dir)
		require.NoError(t, err)
		if id != "id-from-refresh" || p.exchanges.Load() != 0 || p.refreshes.Load() != 1 {
			t.Errorf("Identity() = %q after %d exchanges and %d refreshes, want the refreshed token after one refresh", id, p.exchanges.Load(), p.refreshes.Load())
		}
		if got, want := stored(t, dir), (state{RefreshToken: "refresh-0", SpreadsheetID: "sheet-1"}); got != want {
			t.Errorf("state = %+v, want it unchanged as %+v", got, want)
		}
	})
	t.Run("a refresh without an ID token signs in again and keeps the spreadsheet", func(t *testing.T) {
		p, dir := newProvider(t), t.TempDir()
		p.refresh = func() (int, map[string]any) {
			return http.StatusOK, map[string]any{"access_token": "access-2", "token_type": "Bearer", "expires_in": 3600}
		}
		require.NoError(t, state{RefreshToken: "refresh-0", SpreadsheetID: "sheet-1"}.save(dir))
		id, err := p.identity(t, dir)
		require.NoError(t, err)
		if id != "id-from-sign-in" {
			t.Errorf("Identity() = %q, want the token of the sign-in", id)
		}
		if got, want := stored(t, dir), (state{RefreshToken: "refresh-1", SpreadsheetID: "sheet-1"}); got != want {
			t.Errorf("state = %+v, want %+v", got, want)
		}
	})
	t.Run("a withdrawn refresh token signs in again", func(t *testing.T) {
		p, dir := newProvider(t), t.TempDir()
		p.refresh = func() (int, map[string]any) {
			return http.StatusBadRequest, map[string]any{"error": "invalid_grant", "error_description": "Token has been expired or revoked."}
		}
		require.NoError(t, state{RefreshToken: "refresh-0"}.save(dir))
		id, err := p.identity(t, dir)
		require.NoError(t, err)
		if id != "id-from-sign-in" || stored(t, dir).RefreshToken != "refresh-1" {
			t.Errorf("Identity() = %q with the stored token %q, want a new sign-in", id, stored(t, dir).RefreshToken)
		}
	})
	t.Run("a refresh that fails for another reason is an error", func(t *testing.T) {
		p, dir := newProvider(t), t.TempDir()
		p.refresh = func() (int, map[string]any) {
			return http.StatusInternalServerError, map[string]any{"error": "server_error"}
		}
		require.NoError(t, state{RefreshToken: "refresh-0"}.save(dir))
		if _, err := p.identity(t, dir); err == nil {
			t.Error("Identity() error = nil, want the failed refresh")
		}
		if p.exchanges.Load() != 0 {
			t.Error("Identity() signed in again after a failed refresh")
		}
	})
	t.Run("a redirect with another state is refused", func(t *testing.T) {
		p := newProvider(t)
		p.redirect = func(q url.Values) { q.Set("state", "forged") }
		if _, err := p.identity(t, t.TempDir()); err == nil || !strings.Contains(err.Error(), "state") {
			t.Errorf("Identity() error = %v, want one that names the state", err)
		}
		if p.exchanges.Load() != 0 {
			t.Error("Identity() exchanged the code of a forged redirect")
		}
	})
	t.Run("a refused consent is an error", func(t *testing.T) {
		p := newProvider(t)
		p.redirect = func(q url.Values) { q.Del("code"); q.Set("error", "access_denied") }
		if _, err := p.identity(t, t.TempDir()); err == nil || !strings.Contains(err.Error(), "access_denied") {
			t.Errorf("Identity() error = %v, want one that names access_denied", err)
		}
	})
}

func TestSource(t *testing.T) {
	p := newProvider(t)
	ctx := context.Background()
	t.Run("without a refresh token it fails and opens no sign-in", func(t *testing.T) {
		if _, err := Source(ctx, p.client(t), t.TempDir()); !errors.Is(err, ErrSignedOut) {
			t.Errorf("Source() error = %v, want ErrSignedOut", err)
		}
	})
	t.Run("it refreshes from the state", func(t *testing.T) {
		dir := t.TempDir()
		require.NoError(t, state{RefreshToken: "refresh-0"}.save(dir))
		src, err := Source(ctx, p.client(t), dir)
		require.NoError(t, err)
		tok, err := src.Token()
		require.NoError(t, err)
		if tok.AccessToken != "access-2" {
			t.Errorf("Token() = %q, want the refreshed access token", tok.AccessToken)
		}
	})
}

func TestLoadClient(t *testing.T) {
	tests := []struct {
		name string
		file string
	}{
		{name: "not JSON", file: "client"},
		{name: "a web client", file: `{"web": {"client_id": "id", "auth_uri": "https://example.com/auth", "token_uri": "https://example.com/token"}}`},
		{name: "a client without endpoints", file: `{"installed": {"client_id": "id"}}`},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "client.json")
			require.NoError(t, os.WriteFile(path, []byte(tc.file), 0o600))
			if _, err := LoadClient(path); err == nil {
				t.Errorf("LoadClient(%s) error = nil, want a refusal", tc.file)
			}
		})
	}
	if _, err := LoadClient(filepath.Join(t.TempDir(), "missing.json")); err == nil {
		t.Error("LoadClient() of a missing file error = nil")
	}
}
