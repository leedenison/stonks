// Package server names the conventions every package below it follows. It
// declares nothing; each rule points at the package that owns its detail.
//
// Amounts. Money and quantities are exact decimals,
// github.com/shopspring/decimal, never a float. The module is required by the
// first code that handles an amount, because go mod tidy drops a module
// nothing imports.
//
// Time. Every time.Time is UTC. A time interval is half open: a function
// taking validFrom and validBefore acts on an instant equal to validFrom and
// not on one equal to validBefore.
//
// Errors. A failure below the boundary is wrapped with %w under a lowercase
// context prefix, as in fmt.Errorf("get portfolio: %w", err). A condition a
// caller acts on crosses a package boundary as a sentinel, tested with
// errors.Is and decorated as fmt.Errorf("%w: detail", ErrX):
// auth.ErrUnauthenticated, db.ErrNotFound, session.ErrNotFound. A condition a
// driver states as a code rather than an error value crosses as a predicate
// instead, db.IsConflict. Translation into Connect codes belongs to
// internal/service.
//
// Logging. log/slog only, through internal/logger, which defines the
// categories. internal/service defines where a failure is logged.
//
// Telemetry. OpenTelemetry installed as process globals. Hand-written
// metrics are counters of decisions the code makes, named
// stonks.<area>.<plural noun>, carrying attributes drawn from a set closed in
// the file that declares the instrument, and never a user, session or request
// identifier. Export is a no-op unless a collector endpoint is configured
// which disables telemetry in tests.
//
// Context. Every function that does IO takes a context.Context first. It is
// never stored in a struct, and context.TODO never appears outside a test.
package server
