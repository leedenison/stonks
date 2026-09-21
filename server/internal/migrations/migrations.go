// Package migrations holds the schema as goose-format SQL, embedded in the
// binary and applied by the service as it starts. cmd/migrate applies it to a
// database the service is not running against, such as the test stack's.
//
// Migrations run under a Postgres session lock, so concurrent replicas cannot
// race; the lock is held for the duration. A failed migration stops the service.
//
// A row with no natural key has a surrogate key: a version 7 UUID, which the
// server mints before the insert (see [db.go](../db/db.go)) and uuid_v7()
// supplies for SQL written by hand. The key is the row's public identifier
// too, so it reveals no row count and cannot be enumerated. A version 7 UUID
// is ordered by its creation time, so inserts append to the index rather
// than scatter across it, and a key states when its row was made. A row with
// a natural key, such as a link table or a time series keyed by a parent and
// a time, uses it and carries no surrogate.
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
