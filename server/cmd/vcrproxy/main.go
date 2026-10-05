// Command vcrproxy stands in for an external provider in the e2e stack. It is
// a reverse proxy to one upstream whose transport is go-vcr, so the service it
// fronts is answered with what the provider once sent.
//
// In replay mode the proxy answers from the recorded cassette, replaying an
// interaction as often as it is asked for, and reaches nothing. In record
// mode it replays what the recording holds, forwards the rest to the upstream
// and appends what comes back. A request is matched on its method, URL and
// body.
//
// An authored cassette, when one is given, is consulted before the recording,
// once per interaction in order, so a hand-written sequence, such as three
// rate limit refusals, precedes the recorded answer to the same request.
package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"os"
	"os/signal"
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
	addr     string
	upstream string
	mode     string
	recorded string
	authored string
	strip    string
	interval time.Duration
}

func main() {
	var o options
	flag.StringVar(&o.addr, "addr", ":8080", "address to listen on")
	flag.StringVar(&o.upstream, "upstream", "https://api.openfigi.com", "provider the requests are for")
	flag.StringVar(&o.mode, "mode", "replay", "replay or record")
	flag.StringVar(&o.recorded, "recorded", "", "cassette holding the recording")
	flag.StringVar(&o.authored, "authored", "", "cassette consulted before the recording, if the file exists")
	flag.StringVar(&o.strip, "strip", "X-OPENFIGI-APIKEY", "request header removed before matching or forwarding")
	flag.DurationVar(&o.interval, "interval", 2500*time.Millisecond, "least time between upstream calls when recording")
	flag.Parse()

	log := slog.New(slog.NewTextHandler(os.Stderr, nil))
	if err := run(context.Background(), o, log); err != nil {
		log.Error("vcrproxy", "err", err)
		os.Exit(1)
	}
}

func run(ctx context.Context, o options, log *slog.Logger) error {
	p, err := newProxy(o, log)
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

	log.Info("listening", "addr", o.addr, "mode", o.mode, "upstream", o.upstream, "recorded", o.recorded, "authored", o.authored)
	if err := srv.ListenAndServe(); !errors.Is(err, http.ErrServerClosed) {
		return errors.Join(err, p.Close())
	}
	return p.Close()
}

// proxy answers requests from its cassettes and, when recording, from the
// upstream.
type proxy struct {
	rp   *httputil.ReverseProxy
	recs []*recorder.Recorder
	log  *slog.Logger
}

func newProxy(o options, log *slog.Logger) (*proxy, error) {
	up, err := url.Parse(o.upstream)
	if err != nil {
		return nil, fmt.Errorf("upstream: %w", err)
	}
	if o.recorded == "" {
		return nil, errors.New("-recorded names no cassette")
	}
	scrub := vcr.Scrub{Body: func(body string) string { return body }, MatchBody: true}
	common := []recorder.Option{recorder.WithMatcher(scrub.Match), recorder.WithSkipRequestLatency(true)}

	p := &proxy{log: log}
	var rts []http.RoundTripper
	if o.authored != "" {
		switch _, err := os.Stat(o.authored); {
		case err == nil:
			rec, err := recorder.New(name(o.authored), append(common, recorder.WithMode(recorder.ModeReplayOnly))...)
			if err != nil {
				return nil, fmt.Errorf("authored cassette: %w", err)
			}
			p.recs, rts = append(p.recs, rec), append(rts, rec)
		case !errors.Is(err, fs.ErrNotExist):
			return nil, fmt.Errorf("authored cassette: %w", err)
		}
	}

	opts := append(common, recorder.WithReplayableInteractions(true))
	switch o.mode {
	case "replay":
		opts = append(opts, recorder.WithMode(recorder.ModeReplayOnly))
	case "record":
		opts = append(opts,
			recorder.WithMode(recorder.ModeReplayWithNewEpisodes),
			recorder.WithRealTransport(throttle{rate.NewLimiter(rate.Every(o.interval), 1), http.DefaultTransport}),
			recorder.WithHook(scrub.Redact, recorder.BeforeSaveHook),
			recorder.WithHook(unaddressed, recorder.BeforeSaveHook),
		)
	default:
		return nil, fmt.Errorf("mode %q is neither replay nor record", o.mode)
	}
	rec, err := recorder.New(name(o.recorded), opts...)
	if err != nil {
		return nil, fmt.Errorf("recorded cassette: %w", err)
	}
	p.recs = append(p.recs, rec)
	if o.mode == "record" {
		rts = append(rts, &serial{next: rec})
	} else {
		rts = append(rts, rec)
	}

	p.rp = &httputil.ReverseProxy{
		// The credential header is removed so a placeholder the stack holds
		// never reaches the provider and the recording is unauthenticated.
		// Without a stated Accept-Encoding the transport negotiates
		// compression itself and hands back the decoded body, so the
		// recording holds readable text.
		Rewrite: func(r *httputil.ProxyRequest) {
			r.SetURL(up)
			r.Out.Header.Del(o.strip)
			r.Out.Header.Del("Accept-Encoding")
		},
		Transport:    chain(rts),
		ErrorHandler: p.unanswered,
	}
	return p, nil
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

// ServeHTTP answers GET /healthz with 200 and proxies everything else.
func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method == http.MethodGet && r.URL.Path == "/healthz" {
		w.WriteHeader(http.StatusOK)
		return
	}
	body, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.Body = io.NopCloser(bytes.NewReader(body))
	p.rp.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), bodyKey{}, body)))
}

// unanswered reports a request neither cassette nor upstream answered, as a
// 502 whose body names the request.
func (p *proxy) unanswered(w http.ResponseWriter, r *http.Request, err error) {
	body, _ := r.Context().Value(bodyKey{}).([]byte)
	text := fmt.Sprintf("vcrproxy: %v: %s %s %s", err, r.Method, r.URL.Path, body)
	p.log.Warn("unanswered", "method", r.Method, "path", r.URL.Path, "body", string(body), "err", err)
	http.Error(w, text, http.StatusBadGateway)
}

// Close stops the recorders, writing the recording in record mode.
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

// throttle spaces upstream calls, since the recording is unauthenticated and
// the provider's rate is low.
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
