// Package vcr replays recorded HTTP traffic, so a client of an external
// service is tested against what that service really sent.
//
// A cassette is replayed unless STONKS_RECORD names it.
//
// Request headers are redacted by allowlist.
//
// Query parameters, response headers and bodies are named by the client under
// test, in a Scrub it has to state.  A redacted value is replaced with
// Placeholder, and Credential returns Placeholder when replaying.
//
// Cassettes are YAML under testdata/ beside the test that plays them, one per
// scenario, named for the case.
//
// A recording carries the timestamps of the moment it was made, and they
// recede. Code that compares against the current time takes a clock, and the
// test pins it to a time the cassette is consistent with.
package vcr

import (
	"bytes"
	"io"
	"maps"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"
)

// Placeholder replaces every redacted value.
const Placeholder = "REDACTED"

// recordEnv holds the base name of the one cassette to re-record.
const recordEnv = "STONKS_RECORD"

// allowedHeaders are the request headers a recording keeps. Everything else
// is dropped as the interaction is saved.
var allowedHeaders = []string{"Accept", "Accept-Encoding", "Content-Type", "User-Agent"}

// droppedResponseHeaders are removed from every recording, whatever the client
// declares.
var droppedResponseHeaders = []string{"Set-Cookie"}

// Scrub is a client's declaration of what its traffic carries that must not
// reach disk, and of what distinguishes one of its requests from another.
type Scrub struct {
	// Query names the query parameters replaced with Placeholder.
	Query []string
	// ResponseHeaders names response headers dropped from the recording.
	ResponseHeaders []string
	// Body rewrites a request or response body. It is mandatory: a client
	// with nothing to remove declares NoScrub rather than leaving it nil.
	Body func(string) string
	// MatchBody matches on the request body as well as the method and URL,
	// for a provider whose requests differ only there.
	MatchBody bool
}

// NoScrub is the declaration of a client whose bodies carry nothing that has
// to be removed.
var NoScrub = Scrub{Body: func(body string) string { return body }}

// Recording reports whether STONKS_RECORD names this cassette.
func Recording(cassette string) bool {
	return os.Getenv(recordEnv) == filepath.Base(cassette)
}

// Credential returns the named environment variable while cassette is being
// recorded, and Placeholder otherwise. Recording without the variable set
// fails the test rather than recording an unauthenticated exchange.
func Credential(t *testing.T, env, cassette string) string {
	t.Helper()
	if !Recording(cassette) {
		return Placeholder
	}
	v := os.Getenv(env)
	if v == "" {
		t.Fatalf("recording %s: %s is not set", cassette, env)
	}
	return v
}

// New returns a client serving cassette, which names a file under testdata/
// without its .yaml suffix.
func New(t *testing.T, cassette string, s Scrub) *http.Client {
	t.Helper()
	if s.Body == nil {
		t.Fatalf("vcr.New(%q): Scrub.Body is nil; say what the bodies carry, or declare NoScrub", cassette)
	}

	mode := recorder.ModeReplayOnly
	if Recording(cassette) {
		mode = recorder.ModeRecordOnly
	}

	rec, err := recorder.New(cassette,
		recorder.WithMode(mode),
		recorder.WithSkipRequestLatency(true),
		recorder.WithMatcher(s.match),
		recorder.WithHook(s.redact, recorder.BeforeSaveHook),
	)
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, rec.Stop()) })

	return rec.GetDefaultClient()
}

// redact rewrites an interaction on its way to disk.
func (s Scrub) redact(i *cassette.Interaction) error {
	for _, name := range slices.Collect(maps.Keys(i.Request.Headers)) {
		if !slices.Contains(allowedHeaders, name) {
			i.Request.Headers.Del(name)
		}
	}

	i.Request.URL = s.redactQuery(i.Request.URL)
	i.Request.RequestURI = s.redactQuery(i.Request.RequestURI)
	for _, name := range s.Query {
		if _, ok := i.Request.Form[name]; ok {
			i.Request.Form[name] = []string{Placeholder}
		}
	}

	for _, name := range droppedResponseHeaders {
		i.Response.Headers.Del(name)
	}
	for _, name := range s.ResponseHeaders {
		i.Response.Headers.Del(name)
	}

	i.Request.Body = s.Body(i.Request.Body)
	i.Request.ContentLength = resize(i.Request.Headers, i.Request.Body)
	i.Response.Body = s.Body(i.Response.Body)
	i.Response.ContentLength = resize(i.Response.Headers, i.Response.Body)

	return nil
}

// resize restates the length of a body that redaction has shortened, so a
// client that reads Content-Length on replay is not told the length of the
// text the recording no longer holds. It returns the new length.
func resize(h http.Header, body string) int64 {
	n := int64(len(body))
	if h.Get("Content-Length") != "" {
		h.Set("Content-Length", strconv.FormatInt(n, 10))
	}
	return n
}

// redactQuery replaces the declared query parameters in raw. A URL it cannot
// parse is emptied rather than kept, so an unreadable one cannot carry a
// credential to disk.
func (s Scrub) redactQuery(raw string) string {
	if raw == "" {
		return raw
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	q := u.Query()
	for _, name := range s.Query {
		if q.Has(name) {
			q.Set(name, Placeholder)
		}
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// match compares the method and the URL, and the body when the client says
// that is what distinguishes its requests. Never the headers: dates, nonces
// and authorization differ between a recording and a replay.
func (s Scrub) match(r *http.Request, i cassette.Request) bool {
	if r.Method != i.Method || canonical(r.URL.String()) != canonical(i.URL) {
		return false
	}
	if !s.MatchBody {
		return true
	}
	return requestBody(r) == i.Body
}

// canonical re-encodes the query so that a URL saved through url.Values and
// one built by hand compare equal.
func canonical(raw string) string {
	u, err := url.Parse(raw)
	if err != nil {
		return raw
	}
	u.RawQuery = u.Query().Encode()
	return u.String()
}

// requestBody reads the body and puts it back, so the matcher can be called
// once per interaction in the cassette.
func requestBody(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	b, err := io.ReadAll(r.Body)
	if err != nil {
		return ""
	}
	r.Body = io.NopCloser(bytes.NewReader(b))
	return string(b)
}
