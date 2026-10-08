// Command vcrproxy stands in for the external providers in the e2e stack. It
// is a reverse proxy whose transport is go-vcr, so the service it fronts is
// answered with what each provider once sent.
//
// One proxy serves every provider. It routes a request by the hostname it was
// sent to, and the compose network aliases each provider's hostname to the
// proxy. A hosts file maps each hostname to the provider's upstream, its
// cassettes, its credential, the response fields it redacts and the least
// time between upstream calls when recording. Each provider keeps its own
// cassettes, so one is re-recorded without touching the others.
//
// In replay mode the proxy answers from the recorded cassette, replaying an
// interaction as often as it is asked for, and reaches nothing. In record
// mode it replays what the recording holds, forwards the rest to the upstream
// and appends what comes back. A request is matched on its method, URL and
// body.
//
// The credential the stack sends is removed before matching or forwarding.
// When recording, the provider's own credential is read from the proxy's
// environment and added beneath the recorder, so the recording never holds
// it.
//
// An authored cassette, when one is given, is consulted before the recording,
// once per interaction in order, so a hand-written sequence, such as three
// rate limit refusals, precedes the recorded answer to the same request.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"time"

	"golang.org/x/time/rate"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/cassette"
	"gopkg.in/dnaeon/go-vcr.v4/pkg/recorder"

	"github.com/leedenison/stonks/server/internal/testutil/vcr"
)

// options is the command line.
type options struct {
	addr  string
	mode  string
	hosts string
}

func main() {
	var o options
	flag.StringVar(&o.addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&o.mode, "mode", "replay", "replay or record")
	flag.StringVar(&o.hosts, "hosts", "", "file mapping each provider's hostname to its settings")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(context.Background(), o, log); err != nil {
		log.Error("vcrproxy", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options, log *slog.Logger) error {
	hosts, err := load(o.hosts)
	if err != nil {
		return err
	}
	p, err := newProxy(o.mode, hosts, log)
	if err != nil {
		return err
	}
	srv := &http.Server{Addr: o.addr, Handler: p, ReadHeaderTimeout: 5 * time.Second}

	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() { //nolint:gosec // shutdown cannot derive from ctx, which is already cancelled
		<-ctx.Done()
		shutdown, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdown)
	}()

	log.Info("listening", "addr", o.addr, "mode", o.mode, "hosts", slices.Sorted(maps.Keys(hosts)))
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return errors.Join(err, p.Close())
	}
	return p.Close()
}

// host is one provider's entry in the hosts file. Cassette paths are
// relative to the file.
type host struct {
	Upstream string   `json:"upstream"`
	Recorded string   `json:"recorded"`
	Authored string   `json:"authored"`
	Interval duration `json:"interval"`
	// Credential is absent for a provider recorded without one.
	Credential *credential `json:"credential"`
	// Redact names the JSON members replaced in what a recording saves.
	Redact []string `json:"redact"`
}

// credential is where a provider's requests carry the credential, and the
// environment variable holding the one used to record.
type credential struct {
	// In is header or query.
	In   string `json:"in"`
	Name string `json:"name"`
	// Env is empty for a provider recorded without a credential.
	Env string `json:"env"`
}

// duration reads a Go duration string, such as "2.5s".
type duration time.Duration

func (d *duration) UnmarshalJSON(b []byte) error {
	var s string
	if err := json.Unmarshal(b, &s); err != nil {
		return err
	}
	v, err := time.ParseDuration(s)
	*d = duration(v)
	return err
}

// load reads the hosts file at path.
func load(path string) (map[string]host, error) {
	if path == "" {
		return nil, errors.New("-hosts names no file")
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("hosts: %w", err)
	}
	dec := json.NewDecoder(bytes.NewReader(b))
	dec.DisallowUnknownFields()
	var hosts map[string]host
	if err := dec.Decode(&hosts); err != nil {
		return nil, fmt.Errorf("hosts %s: %w", path, err)
	}
	dir := filepath.Dir(path)
	for name, h := range hosts {
		h.Recorded = filepath.Join(dir, h.Recorded)
		if h.Authored != "" {
			h.Authored = filepath.Join(dir, h.Authored)
		}
		hosts[name] = h
	}
	return hosts, nil
}

// proxy answers requests from each provider's cassettes and, when
// recording, from its upstream.
type proxy struct {
	routes map[string]*httputil.ReverseProxy
	recs   []*recorder.Recorder
	log    *slog.Logger
}

func newProxy(mode string, hosts map[string]host, log *slog.Logger) (*proxy, error) {
	if mode != "replay" && mode != "record" {
		return nil, fmt.Errorf("mode %q is neither replay nor record", mode)
	}
	p := &proxy{routes: map[string]*httputil.ReverseProxy{}, log: log}
	for name, h := range hosts {
		rp, err := p.route(mode, h)
		if err != nil {
			return nil, errors.Join(fmt.Errorf("host %s: %w", name, err), p.Close())
		}
		p.routes[name] = rp
	}
	return p, nil
}

// route builds the reverse proxy of one provider, adding its recorders to
// p. Recording fails to start when the environment lacks the provider's
// credential, since the cassette would hold only refusals.
func (p *proxy) route(mode string, h host) (*httputil.ReverseProxy, error) {
	up, err := url.Parse(h.Upstream)
	if err != nil {
		return nil, fmt.Errorf("upstream: %w", err)
	}
	if h.Recorded == "" {
		return nil, errors.New("names no recorded cassette")
	}
	scrub := vcr.Scrub{Body: vcr.RedactFields(h.Redact...), MatchBody: true}
	var secret string
	if c := h.Credential; c != nil {
		switch c.In {
		case "header":
		case "query":
			scrub.Query = []string{c.Name}
		default:
			return nil, fmt.Errorf("credential in %q is neither header nor query", c.In)
		}
		if mode == "record" && c.Env != "" {
			if secret = os.Getenv(c.Env); secret == "" {
				return nil, fmt.Errorf("recording needs the credential in %s", c.Env)
			}
		}
	}
	common := []recorder.Option{recorder.WithMatcher(scrub.Match), recorder.WithSkipRequestLatency(true)}

	var rts []http.RoundTripper
	if h.Authored != "" {
		switch _, err := os.Stat(h.Authored); {
		case err == nil:
			rec, err := recorder.New(name(h.Authored), append(common, recorder.WithMode(recorder.ModeReplayOnly))...)
			if err != nil {
				return nil, fmt.Errorf("authored cassette: %w", err)
			}
			p.recs, rts = append(p.recs, rec), append(rts, rec)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("authored cassette: %w", err)
		}
	}

	opts := append(common, recorder.WithReplayableInteractions(true))
	if mode == "replay" {
		opts = append(opts, recorder.WithMode(recorder.ModeReplayOnly))
	} else {
		real := http.DefaultTransport
		if secret != "" {
			real = signer{cred: *h.Credential, secret: secret, next: real}
		}
		opts = append(opts,
			recorder.WithMode(recorder.ModeReplayWithNewEpisodes),
			recorder.WithRealTransport(throttle{rate.NewLimiter(rate.Every(time.Duration(h.Interval)), 1), real}),
			recorder.WithHook(scrub.Redact, recorder.BeforeSaveHook),
			recorder.WithHook(unaddressed, recorder.BeforeSaveHook),
		)
	}
	rec, err := recorder.New(name(h.Recorded), opts...)
	if err != nil {
		return nil, fmt.Errorf("recorded cassette: %w", err)
	}
	p.recs = append(p.recs, rec)
	if mode == "record" {
		rts = append(rts, &serial{next: rec})
	} else {
		rts = append(rts, rec)
	}

	return &httputil.ReverseProxy{
		// The credential the stack holds is a placeholder, removed so it
		// never reaches a cassette or the provider. Without a stated
		// Accept-Encoding the transport negotiates compression itself and
		// hands back the decoded body, so the recording holds readable text.
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(up)
			if c := h.Credential; c != nil {
				strip(r.Out, *c)
			}
			r.Out.Header.Del("Accept-Encoding")
		},
		Transport:    chain(rts),
		ErrorHandler: p.unanswered,
	}, nil
}

// strip removes the credential c names from r.
func strip(r *http.Request, c credential) {
	if c.In == "header" {
		r.Header.Del(c.Name)
		return
	}
	q := r.URL.Query()
	if q.Has(c.Name) {
		q.Del(c.Name)
		r.URL.RawQuery = q.Encode()
	}
}

// signer adds the recording credential to a copy of each request. The
// recorder keeps the request it passed down, header map included, so the
// original stays unsigned.
type signer struct {
	cred   credential
	secret string
	next   http.RoundTripper
}

func (s signer) RoundTrip(req *http.Request) (*http.Response, error) {
	out := req.Clone(req.Context())
	if s.cred.In == "header" {
		out.Header.Set(s.cred.Name, s.secret)
	} else {
		q := out.URL.Query()
		q.Set(s.cred.Name, s.secret)
		out.URL.RawQuery = q.Encode()
	}
	return s.next.RoundTrip(out)
}

// unaddressed drops the caller's address from an interaction on its way to
// disk; it is the stack's internal address and distinguishes nothing.
func unaddressed(i *cassette.Interaction) error {
	i.Request.RemoteAddr = ""
	return nil
}

// name is the cassette name go-vcr wants, the path without its .yaml suffix.
func name(path string) string {
	return strings.TrimSuffix(path, ".yaml")
}

// bodyKey carries the request body to the error handler, which names it.
type bodyKey struct{}

// ServeHTTP answers GET /healthz with 200 and proxies everything else to the
// provider of the request's hostname. A hostname the hosts file does not name
// gets a 502.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	hostname := r.Host
	if h, _, err := net.SplitHostPort(r.Host); err == nil {
		hostname = h
	}
	rp, ok := p.routes[hostname]
	if !ok {
		p.log.Warn("unknown host", "host", hostname, "method", r.Method, "path", r.URL.Path)
		http.Error(w, "vcrproxy: no provider for host "+hostname, http.StatusBadGateway)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), bodyKey{}, body)))
}

// unanswered reports a request neither cassette nor upstream answered, as a
// 502 whose body names the request.
func (p *proxy) unanswered(w http.ResponseWriter, r *http.Request, err error) {
	body, _ := r.Context().Value(bodyKey{}).([]byte)
	text := fmt.Sprintf("vcrproxy: %v: %s %s %s %s", err, r.Method, r.Host, r.URL.Path, body)
	p.log.Warn("unanswered", "method", r.Method, "host", r.Host, "path", r.URL.Path, "body", string(body), "err", err)
	http.Error(w, text, http.StatusBadGateway)
}

// Close stops the recorders, writing the recordings in record mode.
func (p *proxy) Close() error {
	var errs []error
	for _, rec := range p.recs {
		errs = append(errs, rec.Stop())
	}
	return errors.Join(errs...)
}

// chain tries each round tripper in turn, moving on when a cassette holds no
// interaction for the request.
type chain []http.RoundTripper

func (c chain) RoundTrip(req *http.Request) (*http.Response, error) {
	var err error
	for _, rt := range c {
		var resp *http.Response
		resp, err = rt.RoundTrip(req)
		if !errors.Is(err, cassette.ErrInteractionNotFound) {
			return resp, err
		}
	}
	return nil, err
}

// serial admits one request at a time, so two requests alike in flight at
// once are matched after the first is recorded and the recording holds each
// request once.
type serial struct {
	mu   sync.Mutex
	next http.RoundTripper
}

func (s *serial) RoundTrip(req *http.Request) (*http.Response, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.next.RoundTrip(req)
}

// throttle spaces upstream calls to the provider's rate.
type throttle struct {
	lim  *rate.Limiter
	next http.RoundTripper
}

func (t throttle) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := t.lim.Wait(req.Context()); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req)
}
