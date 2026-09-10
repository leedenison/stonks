// Package migrations holds the schema as goose-format SQL, embedded in the
// binary and applied by the service as it starts. cmd/migrate applies it to a
// database the service is not running against, such as the test stack's.
//
// Migrations run under a Postgres session lock, so concurrent replicas cannot
// race; the lock is held for the duration. A failed migration stops the service.
//
// The schema is pre-release. A change edits the migration that defines the
// object, and a database that has already applied that migration is recreated:
// the dev volume through make clean-docker, while the test and e2e databases
// are tmpfs and start empty on every run.
//
// Money and quantities are NUMERIC, never a float type. Every time is
// timestamptz, stored and read as UTC. A validity interval is half open: a row
// covers an instant at its valid_from and not one at its valid_before.
package migrations

import "embed"

// FS holds the migrations, one goose SQL file per version at its root.
//
//go:embed *.sql
var FS embed.FS
