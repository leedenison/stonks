// The service binary. All wiring happens here: configuration is read from the
// environment, telemetry is set up before anything instrumented connects,
// migrations are applied, Postgres and Redis are connected, and every handler
// is mounted on one mux served with unencrypted HTTP/2 enabled, so a gRPC
// client reaches it without TLS.
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"connectrpc.com/grpcreflect"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"

	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/config"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/logger"
	"github.com/leedenison/stonks/server/internal/service"
	authsvc "github.com/leedenison/stonks/server/internal/service/auth"
	"github.com/leedenison/stonks/server/internal/service/instrument"
	"github.com/leedenison/stonks/server/internal/telemetry"
)

var buildRevision = "unknown"

const (
	// serviceName is the service.name every signal is attributed to.
	serviceName = "stonks"
	// flushTimeout bounds the final export of buffered telemetry at shutdown.
	flushTimeout = 5 * time.Second
	// httpTimeout bounds a call to an external service. The JWKS fetch holds
	// the key cache's lock for its duration, so an unbounded one would stall
	// every concurrent sign-in behind whatever the far end is doing.
	httpTimeout = 10 * time.Second
	// shutdownTimeout bounds the wait for in-flight requests once a signal
	// has stopped the listener.
	shutdownTimeout = 10 * time.Second
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "stonks:", err)
		os.Exit(1)
	}
}

func run() (err error) {
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	cfg, err := config.Load()
	if err != nil {
		return err
	}
	log := logger.New(os.Stdout, cfg.LogLevel)
	log.Info("starting", "revision", buildRevision, "addr", cfg.ListenAddr, "levels", cfg.LogLevel.String())

	// Before anything that is instrumented connects: an interceptor resolves
	// the global providers when it is built, not when it runs.
	stopTelemetry, err := telemetry.Setup(ctx, telemetry.Options{
		Endpoint: cfg.OTLPEndpoint,
		Service:  serviceName,
		Version:  buildRevision,
		Env:      cfg.Environment,
		Log:      logger.WithCategory(log, "internal/telemetry"),
	})
	if err != nil {
		return err
	}
	defer func() {
		flushCtx, cancel := context.WithTimeout(context.Background(), flushTimeout)
		defer cancel()
		if terr := stopTelemetry(flushCtx); terr != nil && err == nil {
			err = fmt.Errorf("stop telemetry: %w", terr)
		}
	}()

	pool, err := db.Open(ctx, cfg.DBURL, db.WithTracing())
	if err != nil {
		return err
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		return err
	}
	rdb, err := session.Open(ctx, cfg.RedisURL, session.WithTracing())
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rdb.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close redis: %w", cerr)
		}
	}()

	authn := auth.New(auth.Options{
		Verifier: google.New(cfg.GoogleClientID, google.WithHTTPClient(tracedClient())),
		Users:    gen.New(pool),
		Sessions: session.New(rdb, time.Now),
		Allowed:  cfg.AllowedEmails,
	})
	srv, err := newServer(cfg.ListenAddr, log, authn, cfg.CookieSecure)
	if err != nil {
		return err
	}
	errc := make(chan error, 1)
	go func() { errc <- srv.ListenAndServe() }()
	select {
	case err := <-errc:
		return fmt.Errorf("serve: %w", err)
	case <-ctx.Done():
	}
	shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

// tracedClient returns the client outbound calls are made with, so a call to
// an external service is a span of the request that provoked it.
func tracedClient() *http.Client {
	return &http.Client{Timeout: httpTimeout, Transport: otelhttp.NewTransport(http.DefaultTransport)}
}

// newServer mounts /healthz, every Connect handler and gRPC reflection on one
// mux and serves it over HTTP/1.1 and unencrypted HTTP/2. /healthz answers 200
// once the server listens, which is after the migrations have applied. It is
// outside the Connect chain and the mux carries no HTTP instrumentation, so
// the container probing it every two seconds produces no telemetry.
func newServer(addr string, log *slog.Logger, authn *auth.Authenticator, secure bool) (*http.Server, error) {
	opts, err := service.HandlerOptions(logger.WithCategory(log, "internal/service"), authn)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(authv1connect.NewAuthServiceHandler(authsvc.New(authn, secure), opts...))
	mux.Handle(instrumentv1connect.NewInstrumentServiceHandler(instrument.New(), opts...))
	reflector := grpcreflect.NewStaticReflector(authv1connect.AuthServiceName, instrumentv1connect.InstrumentServiceName)
	mux.Handle(grpcreflect.NewHandlerV1(reflector, opts...))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector, opts...))

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{Addr: addr, Handler: mux, Protocols: protocols, ReadHeaderTimeout: 5 * time.Second}, nil
}
