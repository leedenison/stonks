package telemetry_test

import (
	"bytes"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"

	"go.opentelemetry.io/otel"
	"go.uber.org/goleak"

	"github.com/leedenison/stonks/server/internal/logger"
	"github.com/leedenison/stonks/server/internal/telemetry"
)

// The exporter posts over an http.Transport that keeps its idle connections
// after Shutdown, so its read and write loops are not part of what a leak check
// is asserting. The provider's own batcher and reader goroutines are.
func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m,
		goleak.IgnoreAnyFunction("net/http.(*persistConn).readLoop"),
		goleak.IgnoreAnyFunction("net/http.(*persistConn).writeLoop"),
	)
}

func newLogger(t *testing.T) (*slog.Logger, *bytes.Buffer) {
	t.Helper()
	buf := &bytes.Buffer{}
	levels, err := logger.ParseLevels("debug")
	if err != nil {
		t.Fatalf("ParseLevels() error = %v", err)
	}
	return logger.New(buf, levels), buf
}

// TestSetupWithoutEndpoint asserts the no-op path installs nothing, which is
// what keeps telemetry out of tests and out of the end-to-end stack.
func TestSetupWithoutEndpoint(t *testing.T) {
	log, _ := newLogger(t)
	before := otel.GetTracerProvider()

	stop, err := telemetry.Setup(t.Context(), telemetry.Options{Service: "stonks", Log: log})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}
	if stop == nil {
		t.Fatal("Setup() returned a nil shutdown function")
	}
	if got := otel.GetTracerProvider(); got != before {
		t.Errorf("Setup() with no endpoint installed a tracer provider %T, want the process to be untouched", got)
	}
	if err := stop(t.Context()); err != nil {
		t.Errorf("shutdown() error = %v, want nil", err)
	}
}

// TestBridgeReportsToLog asserts a failure inside the SDK reaches the service
// log rather than being dropped.
func TestBridgeReportsToLog(t *testing.T) {
	log, buf := newLogger(t)
	if _, err := telemetry.Setup(t.Context(), telemetry.Options{Service: "stonks", Log: log}); err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	otel.Handle(errors.New("boom"))

	if got := buf.String(); !strings.Contains(got, "boom") {
		t.Errorf("log = %q, want it to contain %q", got, "boom")
	}
}

// TestSetupExports asserts a span reaches the collector. It is the only test
// here that installs real providers, because the globals install once per
// process.
func TestSetupExports(t *testing.T) {
	var mu sync.Mutex
	var paths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	log, _ := newLogger(t)
	stop, err := telemetry.Setup(t.Context(), telemetry.Options{
		Endpoint: srv.URL,
		Service:  "stonks",
		Version:  "test-revision",
		Env:      "test",
		Log:      log,
	})
	if err != nil {
		t.Fatalf("Setup() error = %v", err)
	}

	_, span := otel.Tracer("test").Start(t.Context(), "unit")
	if !span.IsRecording() {
		t.Error("span.IsRecording() = false, want true once a collector is configured")
	}
	span.End()

	if err := stop(t.Context()); err != nil {
		t.Fatalf("shutdown() error = %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	// A base URL carrying no path targets the root, so both signal paths
	// being posted to is what proves the endpoint is joined rather than left
	// to the exporter's default.
	for _, want := range []string{"/v1/traces", "/v1/metrics"} {
		if !slices.Contains(paths, want) {
			t.Errorf("collector received %v, want a post to %s", paths, want)
		}
	}
}
