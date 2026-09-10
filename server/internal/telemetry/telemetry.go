// Package telemetry configures the OpenTelemetry SDK for the process.
//
// Setup installs a tracer provider, a meter provider and a W3C trace context
// propagator as the process globals, so an instrumented library finds them
// without being handed one. It is called once, at startup, and returns the
// function that flushes and stops what it started.
//
// Export is over OTLP/HTTP to one collector endpoint taken from the
// configuration rather than from the OTEL_* variables the SDK reads for
// itself, because the environment is read in one package and this is not it.
// See [config.go](../config/config.go). An empty endpoint installs no
// providers, so a process with no collector starts no exporter, no background
// goroutine and no periodic reader, and every span and measurement is a no-op.
// That is what keeps telemetry out of tests and out of the end-to-end stack,
// which run the same binary with the endpoint unset.
//
// Every span is sampled. A deployment this size is diagnosed one request at a
// time, and a sampler drops the request being diagnosed. Metrics are pushed on
// a fixed interval and held by the collector for Prometheus to scrape, so a
// value is at most one interval plus one scrape old.
//
// The resource identifies the process. service.instance.id is what separates
// two processes' series, so it is the hostname, stable across a restart of the
// same container, rather than an identifier minted at each start.
//
// The SDK reports its own failures through an error handler and a logger, both
// bridged to slog, so a collector that cannot be reached says so in the
// service log rather than silently dropping telemetry.
package telemetry

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"time"

	"github.com/go-logr/logr"
	"go.opentelemetry.io/contrib/instrumentation/runtime"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.43.0"
)

// exportInterval is how often metrics are pushed. It matches the scrape
// interval on the other side of the collector.
const exportInterval = 15 * time.Second

// Options are the settings of the process's telemetry.
type Options struct {
	// Endpoint is the base URL of the collector's OTLP/HTTP receiver, such as
	// "http://otel-collector:4318". The path each signal is posted to is
	// appended to it. Empty exports nothing.
	Endpoint string
	// Service is the service.name every signal carries.
	Service string
	// Version is the service.version, the build revision.
	Version string
	// Env is the deployment.environment.name.
	Env string
	// Log receives the SDK's own failures.
	Log *slog.Logger
}

// Setup installs the process's OpenTelemetry providers and returns the
// function that shuts them down. The returned function is never nil, so a
// caller defers it without checking, and it is a no-op when o.Endpoint is
// empty.
func Setup(ctx context.Context, o Options) (func(context.Context) error, error) {
	bridge(o.Log)
	nop := func(context.Context) error { return nil }
	if o.Endpoint == "" {
		return nop, nil
	}
	res, err := newResource(o)
	if err != nil {
		return nop, err
	}
	traceURL, err := signalURL(o.Endpoint, "traces")
	if err != nil {
		return nop, err
	}
	metricURL, err := signalURL(o.Endpoint, "metrics")
	if err != nil {
		return nop, err
	}
	texp, err := otlptracehttp.New(ctx, otlptracehttp.WithEndpointURL(traceURL))
	if err != nil {
		return nop, fmt.Errorf("trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithResource(res),
		sdktrace.WithBatcher(texp),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	mexp, err := otlpmetrichttp.New(ctx, otlpmetrichttp.WithEndpointURL(metricURL))
	if err != nil {
		return nop, errors.Join(fmt.Errorf("metric exporter: %w", err), tp.Shutdown(ctx))
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(mexp, sdkmetric.WithInterval(exportInterval))),
	)
	stop := func(ctx context.Context) error {
		return errors.Join(tp.Shutdown(ctx), mp.Shutdown(ctx))
	}
	otel.SetTracerProvider(tp)
	otel.SetMeterProvider(mp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{}, propagation.Baggage{}))
	if err := runtime.Start(runtime.WithMeterProvider(mp)); err != nil {
		return nop, errors.Join(fmt.Errorf("runtime metrics: %w", err), stop(ctx))
	}
	return stop, nil
}

// signalURL appends a signal's OTLP path to the collector's base URL. A URL
// carrying no path targets the root, so the path is joined here rather than
// left to the exporter's default.
func signalURL(base, signal string) (string, error) {
	u, err := url.JoinPath(base, "v1", signal)
	if err != nil {
		return "", fmt.Errorf("endpoint %q: %w", base, err)
	}
	return u, nil
}

// newResource describes the process every signal is attributed to. It is built
// by hand rather than from resource.Default, which reads the environment.
func newResource(o Options) (*resource.Resource, error) {
	host, err := os.Hostname()
	if err != nil {
		return nil, fmt.Errorf("hostname: %w", err)
	}
	return resource.NewWithAttributes(
		semconv.SchemaURL,
		semconv.ServiceName(o.Service),
		semconv.ServiceVersion(o.Version),
		semconv.ServiceInstanceID(host),
		semconv.DeploymentEnvironmentNameKey.String(o.Env),
	), nil
}

// bridge routes the SDK's own errors and internal logging to log.
func bridge(log *slog.Logger) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		log.Error("opentelemetry", "err", err)
	}))
	otel.SetLogger(logr.FromSlogHandler(log.Handler()))
}
