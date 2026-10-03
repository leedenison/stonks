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
	credential = "X-OPENFIGI-APIKEY"
)

// upstream is a provider that counts its calls and refuses a credential.
func upstream(t *testing.T, calls *atomic.Int32) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		if r.Header.Get(credential) != "" {
			http.Error(w, "credential forwarded", http.StatusUnauthorized)
			return
		}
		body, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"data":[{"ticker":"ANSWER"}],"sent":` + string(body) + `}]`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func serve(t *testing.T, o options) *httptest.Server {
	t.Helper()
	p, err := newProxy(o, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	srv := httptest.NewServer(p)
	t.Cleanup(func() {
		srv.Close()
		require.NoError(t, p.Close())
	})
	return srv
}

func post(t *testing.T, srv *httptest.Server, body string) (int, string) {
	t.Helper()
	req, err := http.NewRequest(http.MethodPost, srv.URL+mapping, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(credential, "placeholder")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	text, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.NoError(t, resp.Body.Close())
	return resp.StatusCode, string(text)
}

// TestRecordThenReplay checks that record mode calls the upstream once per
// distinct request, writes the recording on Close, and that replay mode
// answers from it without reaching the upstream.
func TestRecordThenReplay(t *testing.T) {
	var calls atomic.Int32
	up := upstream(t, &calls)
	recorded := filepath.Join(t.TempDir(), "recorded.yaml")

	p, err := newProxy(options{upstream: up.URL, mode: "record", recorded: recorded, strip: credential, interval: time.Millisecond}, slog.New(slog.DiscardHandler))
	require.NoError(t, err)
	rec := httptest.NewServer(p)
	for range 2 {
		code, body := post(t, rec, apple)
		if code != http.StatusOK || !strings.Contains(body, "ANSWER") {
			t.Fatalf("recording: status %d body %q", code, body)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1: the second request replays the first", calls.Load())
	}
	rec.Close()
	require.NoError(t, p.Close())

	replay := serve(t, options{upstream: up.URL, mode: "replay", recorded: recorded, strip: credential})
	for range 2 {
		code, body := post(t, replay, apple)
		if code != http.StatusOK || !strings.Contains(body, "ANSWER") {
			t.Errorf("replaying: status %d body %q", code, body)
		}
	}
	code, body := post(t, replay, `[{"idType":"ID_ISIN","idValue":"GB00BH4HKS39"}]`)
	if code != http.StatusBadGateway || !strings.Contains(body, "GB00BH4HKS39") || !strings.Contains(body, mapping) {
		t.Errorf("unrecorded: status %d body %q, want 502 naming the path and body", code, body)
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1: replay reaches nothing", calls.Load())
	}
}

// authoredYAML is two refusals of the Apple request, in the format of
// docker/vcrproxy/authored.yaml.
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
	up := upstream(t, &calls)
	dir := t.TempDir()
	authored := filepath.Join(dir, "authored.yaml")
	require.NoError(t, os.WriteFile(authored, []byte(fmt.Sprintf(authoredYAML, up.URL, apple)), 0o600))

	srv := serve(t, options{upstream: up.URL, mode: "record", recorded: filepath.Join(dir, "recorded.yaml"), authored: authored, strip: credential, interval: time.Millisecond})
	want := []int{http.StatusTooManyRequests, http.StatusTooManyRequests, http.StatusOK, http.StatusOK}
	for i, w := range want {
		if code, _ := post(t, srv, apple); code != w {
			t.Errorf("request %d status = %d, want %d", i, code, w)
		}
	}
	if calls.Load() != 1 {
		t.Errorf("upstream calls = %d, want 1: the refusals are authored and the answer is recorded once", calls.Load())
	}
}

// TestHealthz checks the health endpoint answers without a cassette match.
func TestHealthz(t *testing.T) {
	recorded := filepath.Join(t.TempDir(), "recorded.yaml")
	require.NoError(t, os.WriteFile(recorded, []byte("---\nversion: 2\ninteractions: []\n"), 0o600))
	srv := serve(t, options{upstream: "https://example.invalid", mode: "replay", recorded: recorded})
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
	_, err := newProxy(options{upstream: "https://example.invalid", mode: "replay", recorded: filepath.Join(t.TempDir(), "missing.yaml")}, slog.New(slog.DiscardHandler))
	if err == nil {
		t.Fatal("newProxy() = nil error, want a missing cassette")
	}
}
