package main

import (
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

const (
	mapping    = "/v3/mapping"
	apple      = `[{"idType":"ID_ISIN","idValue":"US0378331005"}]`
	header     = "X-OPENFIGI-APIKEY"
	figiHost   = "openfigi.vcr"
	secret     = "real-key"
	secretEnv  = "VCRPROXY_TEST_KEY"
	stackValue = "placeholder"
)

// upstream is a provider that counts its calls, echoes the request body and
// answers 401 unless c carries want. An empty want means c must be absent.
func upstream(t *testing.T, calls *atomic.Int32, c credential, want string) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		got := r.Header.Get(c.Name)
		if c.In == "query" {
			got = r.URL.Query().Get(c.Name)
		}
		if got != want {
			http.Error(w, "credential "+got, http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ticker":"ANSWER","address":"1 Main Street","sent":"` + strings.ReplaceAll(string(body), `"`, `'`) + `"}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// figi is the hosts file entry of an OpenFIGI-like provider at up, whose
// recorder adds no credential of its own.
func figi(up, dir string) host {
	return host{
		Upstream:   up,
		Recorded:   filepath.Join(dir, "recorded.yaml"),
		Credential: &credential{In: "header", Name: header},
	}
}

func serve(t *testing.T, mode string, hosts map[string]host) *httptest.Server {
	t.Helper()
	p, err := newProxy(mode, hosts, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	srv := httptest.NewServer(p)
	t.Cleanup(func() {
		srv.Close()
		require.NoError(t, p.Close())
	})
	return srv
}

// record runs a proxy in record mode for fn, then writes the recordings.
func record(t *testing.T, hosts map[string]host, fn func(*httptest.Server)) {
	t.Helper()
	p, err := newProxy("record", hosts, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	srv := httptest.NewServer(p)
	fn(srv)
	srv.Close()
	require.NoError(t, p.Close())
}

// post sends body as the stack would, to hostname, and returns the status and
// body.
func post(t *testing.T, srv *httptest.Server, hostname, path, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+path, strings.NewReader(body))
	require.NoError(t, err)
	req.Host = hostname + ":8080"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(header, stackValue)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	text, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(text)
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	require.NoError(t, err)
	return string(b)
}

// TestRecordThenReplay checks that record mode calls the upstream once per
// distinct request, writes the recording on Close, and that replay mode
// answers from it without reaching the upstream.
func TestRecordThenReplay(t *testing.T) {
	var calls atomic.Int32
	up := upstream(t, &calls, credential{In: "header", Name: header}, "")
	hosts := map[string]host{figiHost: figi(up.URL, t.TempDir())}

	record(t, hosts, func(srv *httptest.Server) {
		for range 2 {
			code, body := post(t, srv, figiHost, mapping, apple)
			if code != http.StatusOK || !strings.Contains(body, "ANSWER") {
				t.Fatalf("recording: status %d body %q", code, body)
			}
		}
	})
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1: the second request replays the first", calls.Load())
	}

	replay := serve(t, "replay", hosts)
	for range 2 {
		code, body := post(t, replay, figiHost, mapping, apple)
		if code != http.StatusOK || !strings.Contains(body, "ANSWER") {
			t.Errorf("replaying: status %d body %q", code, body)
		}
	}
	code, body := post(t, replay, figiHost, mapping, `[{"idType":"ID_ISIN","idValue":"GB00BH4HKS39"}]`)
	if code != http.StatusBadGateway || !strings.Contains(body, "GB00BH4HKS39") || !strings.Contains(body, mapping) {
		t.Errorf("unrecorded: status %d body %q, want 502 naming the path and body", code, body)
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1: replay reaches nothing", calls.Load())
	}
}

// TestRoutesByHost checks that each hostname reaches its own provider and
// records into its own cassette, and that a hostname the file does not name
// fails.
func TestRoutesByHost(t *testing.T) {
	var a, b atomic.Int32
	c := credential{In: "header", Name: header}
	upA, upB := upstream(t, &a, c, ""), upstream(t, &b, c, "")
	dirA, dirB := t.TempDir(), t.TempDir()
	hosts := map[string]host{"a.vcr": figi(upA.URL, dirA), "b.vcr": figi(upB.URL, dirB)}

	record(t, hosts, func(srv *httptest.Server) {
		post(t, srv, "a.vcr", "/a", apple)
		post(t, srv, "b.vcr", "/b", apple)
		post(t, srv, "b.vcr", "/b", apple)
		code, body := post(t, srv, "c.vcr", "/c", apple)
		if code != http.StatusBadGateway || !strings.Contains(body, "c.vcr") {
			t.Errorf("unknown host: status %d body %q, want 502 naming the host", code, body)
		}
	})
	if a.Load() != 1 || b.Load() != 1 {
		t.Errorf("upstream calls = %d and %d, want 1 each", a.Load(), b.Load())
	}
	if ra, rb := read(t, filepath.Join(dirA, "recorded.yaml")), read(t, filepath.Join(dirB, "recorded.yaml")); !strings.Contains(ra, "/a") || strings.Contains(ra, "/b") || !strings.Contains(rb, "/b") || strings.Contains(rb, "/a") {
		t.Errorf("recordings hold another provider's requests:\n%s\n%s", ra, rb)
	}
}

// TestRecordingCredential checks that recording replaces the stack's
// placeholder with the credential from the environment, that the recording
// holds neither, and that replay matches the request the stack sends.
func TestRecordingCredential(t *testing.T) {
	t.Setenv(secretEnv, secret)
	for _, c := range []credential{
		{In: "header", Name: header, Env: secretEnv},
		{In: "query", Name: "apiKey", Env: secretEnv},
	} {
		t.Run(c.In, func(t *testing.T) {
			var calls atomic.Int32
			up := upstream(t, &calls, c, secret)
			dir := t.TempDir()
			hosts := map[string]host{"p.vcr": {Upstream: up.URL, Recorded: filepath.Join(dir, "recorded.yaml"), Credential: &c}}
			path := "/v3/reference/tickers/AAPL"
			if c.In == "query" {
				path += "?apiKey=" + stackValue
			}

			record(t, hosts, func(srv *httptest.Server) {
				if code, body := post(t, srv, "p.vcr", path, ""); code != http.StatusOK {
					t.Fatalf("recording: status %d body %q", code, body)
				}
			})
			saved := read(t, filepath.Join(dir, "recorded.yaml"))
			if strings.Contains(saved, secret) || strings.Contains(saved, stackValue) {
				t.Errorf("recording holds a credential:\n%s", saved)
			}

			replay := serve(t, "replay", hosts)
			if code, body := post(t, replay, "p.vcr", path, ""); code != http.StatusOK {
				t.Errorf("replaying: status %d body %q", code, body)
			}
			if calls.Load() != 1 {
				t.Errorf("upstream calls = %d, want 1", calls.Load())
			}
		})
	}
}

// TestRecordNeedsCredential checks that record mode refuses to start for a
// provider whose credential the environment lacks.
func TestRecordNeedsCredential(t *testing.T) {
	t.Setenv(secretEnv, "")
	dir := t.TempDir()
	hosts := map[string]host{"p.vcr": {Upstream: "https://example.invalid", Recorded: filepath.Join(dir, "recorded.yaml"), Credential: &credential{In: "query", Name: "apiKey", Env: secretEnv}}}
	_, err := newProxy("record", hosts, slog.New(slog.DiscardHandler))
	if err == nil || !strings.Contains(err.Error(), secretEnv) {
		t.Fatalf("newProxy() error = %v, want one naming %s", err, secretEnv)
	}
	if _, err := os.Stat(filepath.Join(dir, "recorded.yaml")); !os.IsNotExist(err) {
		t.Errorf("recording written: stat err = %v", err)
	}
}

// TestRedactsFields checks that the named members of a response are
// replaced in the recording.
func TestRedactsFields(t *testing.T) {
	var calls atomic.Int32
	up := upstream(t, &calls, credential{In: "header", Name: header}, "")
	h := figi(up.URL, t.TempDir())
	h.Redact = []string{"address"}
	record(t, map[string]host{figiHost: h}, func(srv *httptest.Server) {
		post(t, srv, figiHost, mapping, apple)
	})
	if saved := read(t, h.Recorded); strings.Contains(saved, "Main Street") || !strings.Contains(saved, "ANSWER") {
		t.Errorf("recording keeps the address or loses the answer:\n%s", saved)
	}
}

// authoredYAML is two refusals of the Apple request, in the format of
// docker/vcrproxy/openfigi/authored.yaml.
const authoredYAML = `---
version: 2
interactions:
    - id: 0
      request:
        method: POST
        url: %[1]s/v3/mapping
        body: '%[2]s'
      response:
        status: 429 Too Many Requests
        code: 429
        body: Too Many Requests
    - id: 1
      request:
        method: POST
        url: %[1]s/v3/mapping
        body: '%[2]s'
      response:
        status: 429 Too Many Requests
        code: 429
        body: Too Many Requests
`

// TestAuthoredPrecedes checks that the authored cassette answers once per
// interaction, in order, before the recording does.
func TestAuthoredPrecedes(t *testing.T) {
	var calls atomic.Int32
	up := upstream(t, &calls, credential{In: "header", Name: header}, "")
	dir := t.TempDir()
	h := figi(up.URL, dir)
	h.Authored = filepath.Join(dir, "authored.yaml")
	require.NoError(t, os.WriteFile(h.Authored, []byte(fmt.Sprintf(authoredYAML, up.URL, apple)), 0o600))

	srv := serve(t, "record", map[string]host{figiHost: h})
	want := []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusOK, http.StatusOK}
	for i, w := range want {
		if code, _ := post(t, srv, figiHost, mapping, apple); code != w {
			t.Errorf("request %d status = %d, want %d", i, code, w)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1: the refusals are authored and the answer is recorded once", calls.Load())
	}
}

// TestHealthz checks the health endpoint answers without a cassette match.
func TestHealthz(t *testing.T) {
	srv := serve(t, "replay", nil)
	resp, err := http.Get(srv.URL + "/healthz")
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	if resp.StatusCode != http.StatusOK {
		t.Errorf("healthz status = %d, want 200", resp.StatusCode)
	}
}

// TestReplayNeedsRecording checks that replay mode refuses to start without a
// recording, rather than answering every request with a 502.
func TestReplayNeedsRecording(t *testing.T) {
	hosts := map[string]host{figiHost: figi("https://example.invalid", t.TempDir())}
	if _, err := newProxy("replay", hosts, slog.New(slog.DiscardHandler)); err == nil {
		t.Fatal("newProxy() = nil error, want a missing cassette")
	}
}

// TestLoad checks that cassette paths are read relative to the hosts file,
// and that newProxy refuses a credential carried anywhere else than a header
// or a query.
func TestLoad(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts.json")
	require.NoError(t, os.WriteFile(path, []byte(`{"openfigi.vcr": {"upstream": "https://api.openfigi.com", "recorded": "openfigi/recorded.yaml", "authored": "openfigi/authored.yaml", "interval": "2.5s", "credential": {"in": "header", "name": "X-OPENFIGI-APIKEY"}}}`), 0o600))
	hosts, err := load(path)
	require.NoError(t, err)
	h := hosts[figiHost]
	if h.Recorded != filepath.Join(dir, "openfigi", "recorded.yaml") || h.Authored != filepath.Join(dir, "openfigi", "authored.yaml") || h.Interval != duration(2500*time.Millisecond) {
		t.Errorf("load() = %+v, want paths under %s and 2.5s", h, dir)
	}

	h.Credential.In = "body"
	if _, err := newProxy("replay", map[string]host{figiHost: h}, slog.New(slog.DiscardHandler)); err == nil || !strings.Contains(err.Error(), "body") {
		t.Errorf("newProxy() error = %v, want one refusing a credential in the body", err)
	}
}
