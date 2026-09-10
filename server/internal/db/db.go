// Package db confines all SQL to one place, so the rest of the server is unit
// tested against a mock.
//
// Queries are SQL under queries/<area>/, compiled by sqlc into the gen package:
// one package, one Querier, for every area. Nothing depends on Querier
// directly. Each consumer declares the narrow interface it needs, which
// *gen.Queries satisfies structurally, and mocks that.
//
// The driver is pgx v5, which sqlc targets natively.
//
// A condition a caller acts on crosses the package boundary as a sentinel
// error, such as ErrNotFound, checked with errors.Is. A condition the driver
// states as a SQLSTATE rather than an error value crosses as a predicate
// instead, IsConflict. Every other failure is wrapped.
//
// Generated code is gitignored, so make generate precedes compilation.
package db

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"

	"github.com/leedenison/stonks/server/internal/migrations"
)

// sqlcNamePrefix introduces the query name sqlc emits above each statement.
const sqlcNamePrefix = "-- name: "

// ErrNotFound is what a generated single-row query returns when no row matches.
var ErrNotFound = pgx.ErrNoRows

// IsConflict reports whether err is a write a unique constraint refused, which
// is how a caller learns that the row it meant to insert already exists.
func IsConflict(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == pgerrcode.UniqueViolation
}

// Option configures a pool.
type Option func(*options)

type options struct{ tracing bool }

// WithTracing traces every query and reports the pool's statistics through the
// process's OpenTelemetry providers.
func WithTracing() Option { return func(o *options) { o.tracing = true } }

// Open connects a pool to url and verifies it with a ping.
func Open(ctx context.Context, url string, opts ...Option) (*pgxpool.Pool, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	cfg, err := pgxpool.ParseConfig(url)
	if err != nil {
		return nil, fmt.Errorf("parse pool config: %w", err)
	}
	if o.tracing {
		cfg.ConnConfig.Tracer = otelpgx.NewTracer(
			otelpgx.WithTrimSQLInSpanName(),
			otelpgx.WithSpanNameFunc(queryName),
		)
	}
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("open pool: %w", err)
	}
	if err := pool.Ping(ctx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping: %w", err)
	}
	if o.tracing {
		if err := otelpgx.RecordStats(pool); err != nil {
			pool.Close()
			return nil, fmt.Errorf("record pool stats: %w", err)
		}
	}
	return pool, nil
}

// queryName names the span of a statement. sqlc emits each query with its name
// as a leading "-- name: <Name> :<kind>" comment, and that name is what a
// reader recognises. A statement carrying no such comment is named by its
// leading keyword, so the set of span names stays bounded by the queries that
// exist rather than growing with traffic.
func queryName(sql string) string {
	sql = strings.TrimSpace(sql)
	if rest, ok := strings.CutPrefix(sql, sqlcNamePrefix); ok {
		if name, _, _ := strings.Cut(strings.TrimSpace(rest), " "); name != "" {
			return name
		}
	}
	keyword, _, _ := strings.Cut(sql, " ")
	return keyword
}

// Migrate applies every pending migration in migrations.FS under goose's
// session lock.
func Migrate(ctx context.Context, pool *pgxpool.Pool) (err error) {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return fmt.Errorf("session locker: %w", err)
	}
	sqlDB := stdlib.OpenDBFromPool(pool)
	defer func() { err = errors.Join(err, sqlDB.Close()) }()
	p, err := goose.NewProvider(goose.DialectPostgres, sqlDB, migrations.FS, goose.WithSessionLocker(locker))
	if err != nil {
		return fmt.Errorf("migration provider: %w", err)
	}
	if _, err := p.Up(ctx); err != nil {
		return fmt.Errorf("migrate up: %w", err)
	}
	return nil
}
