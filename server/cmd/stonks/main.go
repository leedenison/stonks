// Command stonks is the service binary. It wires every package and serves
// the handlers on one mux with unencrypted HTTP/2 enabled, so a gRPC client
// reaches it without TLS.
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

	"github.com/leedenison/stonks/proto/admin/block/v1/adminblockv1connect"
	"github.com/leedenison/stonks/proto/admin/coverage/v1/admincoveragev1connect"
	"github.com/leedenison/stonks/proto/admin/datasource/v1/admindatasourcev1connect"
	"github.com/leedenison/stonks/proto/admin/extend_validity/v1/adminextendvalidityv1connect"
	"github.com/leedenison/stonks/proto/admin/instrument/v1/admininstrumentv1connect"
	"github.com/leedenison/stonks/proto/admin/replay/v1/adminreplayv1connect"
	"github.com/leedenison/stonks/proto/admin/run/v1/adminrunv1connect"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	"github.com/leedenison/stonks/proto/run/v1/runv1connect"
	"github.com/leedenison/stonks/proto/statement/v1/statementv1connect"
	"github.com/leedenison/stonks/server/internal/attest"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/config"
	"github.com/leedenison/stonks/server/internal/coverage"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/datasource"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/extendvalidity"
	"github.com/leedenison/stonks/server/internal/logger"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/eodhd"
	"github.com/leedenison/stonks/server/internal/market/massive"
	"github.com/leedenison/stonks/server/internal/market/openfigi"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/rdb"
	"github.com/leedenison/stonks/server/internal/replay"
	"github.com/leedenison/stonks/server/internal/resolve"
	runner "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/service"
	adminblock "github.com/leedenison/stonks/server/internal/service/admin/block"
	admincoverage "github.com/leedenison/stonks/server/internal/service/admin/coverage"
	admindatasource "github.com/leedenison/stonks/server/internal/service/admin/datasource"
	adminextendvalidity "github.com/leedenison/stonks/server/internal/service/admin/extendvalidity"
	admininstrument "github.com/leedenison/stonks/server/internal/service/admin/instrument"
	adminreplay "github.com/leedenison/stonks/server/internal/service/admin/replay"
	adminrun "github.com/leedenison/stonks/server/internal/service/admin/run"
	authsvc "github.com/leedenison/stonks/server/internal/service/auth"
	holdingsvc "github.com/leedenison/stonks/server/internal/service/holding"
	runsvc "github.com/leedenison/stonks/server/internal/service/run"
	stmtsvc "github.com/leedenison/stonks/server/internal/service/statement"
	stmt "github.com/leedenison/stonks/server/internal/statement"
	"github.com/leedenison/stonks/server/internal/telemetry"
)

var buildRevision = "unknown"

const (
	// serviceName is the service.name of every signal.
	serviceName = "stonks"
	// flushTimeout bounds the final export of buffered telemetry at shutdown.
	flushTimeout = 5 * time.Second
	// httpTimeout bounds a call to an external service. The JWKS fetch holds
	// the key cache's lock for its duration, so an unbounded one would stall
	// every concurrent sign-in behind whatever the far end is doing.
	httpTimeout = 10 * time.Second
	// providerTimeout bounds a call to a market data provider. A batch of a
	// hundred identifiers takes longer than the sign-in calls that
	// httpTimeout bounds.
	providerTimeout = 30 * time.Second
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
	queries := gen.New(pool)
	runLog := logger.WithCategory(log, "internal/run")
	if err := runner.Sweep(ctx, queries, runLog); err != nil {
		return err
	}
	runs := runner.New(queries, runLog)
	defer runs.Close()
	mics, err := mic.Load(ctx, queries)
	if err != nil {
		return err
	}
	families, err := currency.Load(ctx, queries)
	if err != nil {
		return err
	}
	providers := tracedClient(providerTimeout)
	integrations := map[string]market.Factory{
		"openfigi": openfigi.Factory(mics, providers),
		"massive":  massive.Factory(mics, providers),
		"eodhd":    eodhd.Factory(mics, providers),
	}
	sources, err := market.New(ctx, queries, integrations, logger.WithCategory(log, "internal/market"))
	if err != nil {
		return err
	}
	log.Info("datasources", "enabled", sources.Names())
	rc, err := rdb.Open(ctx, cfg.RedisURL, rdb.WithTracing())
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rc.Close(); cerr != nil && err == nil {
			err = fmt.Errorf("close redis: %w", cerr)
		}
	}()

	authn := auth.New(auth.Options{
		Verifier: google.New(cfg.GoogleClientID, google.WithHTTPClient(tracedClient(httpTimeout))),
		Users:    queries,
		Sessions: session.New(rc, time.Now),
		Allowed:  cfg.AllowedEmails,
	})
	cache := market.NewCache(rc, cfg.FetchCacheTTL)
	fetcher := market.NewFetcher(db.New[market.Queries](pool), runs, logger.WithCategory(log, "internal/market"), market.WithCache(cache))
	resolver := resolve.New(db.New[resolve.Queries](pool), fetcher, sources, mics, families, time.Now, logger.WithCategory(log, "internal/resolve"))
	replays := replay.New(db.New[replay.Queries](pool), runs, resolver, sources, mics, logger.WithCategory(log, "internal/replay"))
	extender := resolve.New(db.New[resolve.Queries](pool), fetcher.Fresh(), sources, mics, families, time.Now, logger.WithCategory(log, "internal/resolve"))
	extensions := extendvalidity.New(queries, runs, extender, replays)
	ingester := stmt.New(db.New[stmt.Queries](pool), runs, resolver, replays, families, time.Now, logger.WithCategory(log, "internal/statement"))
	datasources := admindatasource.New(queries, sources, datasource.New(db.New[datasource.Queries](pool), sources, cache))
	srv, err := newServer(cfg.ListenAddr, log, authn, queries, adminrun.New(queries), adminblock.New(queries), datasources, admininstrument.New(queries), adminreplay.New(queries, replays), adminextendvalidity.New(extensions), admincoverage.New(coverage.New(queries, mics), attest.New(db.New[attest.Queries](pool), runs, replays, mics, time.Now), time.Now), ingester, cfg.CookieSecure)
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

// tracedClient returns a client for outbound calls bounded by timeout, so a
// call to an external service is a span of the request that provoked it.
func tracedClient(timeout time.Duration) *http.Client {
	return &http.Client{Timeout: timeout, Transport: otelhttp.NewTransport(http.DefaultTransport)}
}

// newServer mounts /healthz, every Connect handler and gRPC reflection on one
// mux and serves it over HTTP/1.1 and unencrypted HTTP/2. /healthz answers 200
// once the server listens, which is after the migrations have applied. It is
// outside the Connect chain and the mux carries no HTTP instrumentation, so
// the container probing it every two seconds produces no telemetry.
func newServer(addr string, log *slog.Logger, authn *auth.Authenticator, queries *gen.Queries, runs *adminrun.Server, blocks *adminblock.Server, datasources *admindatasource.Server, instruments *admininstrument.Server, replays *adminreplay.Server, extensions *adminextendvalidity.Server, coverages *admincoverage.Server, statements *stmt.Service, secure bool) (*http.Server, error) {
	opts, err := service.HandlerOptions(logger.WithCategory(log, "internal/service"), authn)
	if err != nil {
		return nil, err
	}
	mux := http.NewServeMux()
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
	mux.Handle(adminblockv1connect.NewBlockServiceHandler(blocks, opts...))
	mux.Handle(admincoveragev1connect.NewCoverageServiceHandler(coverages, opts...))
	mux.Handle(admindatasourcev1connect.NewDatasourceServiceHandler(datasources, opts...))
	mux.Handle(admininstrumentv1connect.NewInstrumentServiceHandler(instruments, opts...))
	mux.Handle(adminextendvalidityv1connect.NewExtendValidityServiceHandler(extensions, opts...))
	mux.Handle(adminreplayv1connect.NewReplayServiceHandler(replays, opts...))
	mux.Handle(adminrunv1connect.NewRunServiceHandler(runs, opts...))
	mux.Handle(authv1connect.NewAuthServiceHandler(authsvc.New(authn, secure), opts...))
	mux.Handle(holdingv1connect.NewHoldingServiceHandler(holdingsvc.New(queries), opts...))
	mux.Handle(runv1connect.NewRunServiceHandler(runsvc.New(queries), opts...))
	mux.Handle(statementv1connect.NewStatementServiceHandler(stmtsvc.New(statements, statements, queries), opts...))
	reflector := grpcreflect.NewStaticReflector(
		adminblockv1connect.BlockServiceName, admincoveragev1connect.CoverageServiceName, admindatasourcev1connect.DatasourceServiceName, admininstrumentv1connect.InstrumentServiceName, adminextendvalidityv1connect.ExtendValidityServiceName, adminreplayv1connect.ReplayServiceName, adminrunv1connect.RunServiceName, authv1connect.AuthServiceName, holdingv1connect.HoldingServiceName,
		runv1connect.RunServiceName, statementv1connect.StatementServiceName,
	)
	mux.Handle(grpcreflect.NewHandlerV1(reflector, opts...))
	mux.Handle(grpcreflect.NewHandlerV1Alpha(reflector, opts...))

	protocols := new(http.Protocols)
	protocols.SetHTTP1(true)
	protocols.SetUnencryptedHTTP2(true)
	return &http.Server{Addr: addr, Handler: mux, Protocols: protocols, ReadHeaderTimeout: 5 * time.Second}, nil
}
