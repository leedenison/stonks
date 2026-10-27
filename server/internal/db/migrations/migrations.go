// Package migrations holds the schema as goose-format SQL, embedded in the
// binary and applied by the service as it starts. cmd/migrate applies it to a
// database with no running service, such as the test stack's.
//
// A row with no natural key has a surrogate key: a version 7 UUID, which the
// server mints before the insert (see [db.go](../db.go)) and uuid_v7()
// supplies for SQL written by hand. The key is the row's public identifier
// too, so it reveals no row count and cannot be enumerated. A version 7 UUID
// is ordered by its creation time, so inserts append to the index rather
// than scatter across it, and a key states when its row was made. A row with
// a narrow natural key that no other table references, such as a link table
// or a time series keyed by a parent and a time, has only the natural key. A
// row whose natural key is wide, or which other tables reference, keeps a
// surrogate and declares the natural key as a unique constraint.
//
// Money and quantities are NUMERIC, never a float type. Every time is
// timestamptz, stored and read as UTC, except the days of assertions,
// identifier events, corporate actions and their coverage, which are dates and
// half open ranges of dates. A validity interval is half open: a row covers an
// instant at its valid_from and not one at its valid_before.
package migrations

import "embed"

// FS holds the migrations, one goose SQL file per version at its root.
//
//go:embed *.sql
var FS embed.FS
