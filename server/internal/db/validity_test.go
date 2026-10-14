//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestIdentifierAssertions checks that an assertion has one source, a fetch
// key or an administrator's attestation run, and goes with that source.
func TestIdentifierAssertions(t *testing.T) {
	ctx := context.Background()
	on := date(2026, 3, 2)
	assertion := func() gen.CreateIdentifierAssertionParams {
		return gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "SHEL", AssertedOn: on,
		}
	}

	t.Run("one per fetch key, identifier and day", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)
		key := servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
		arg := assertion()
		arg.FetchKeyID = &key
		require.NoError(t, q.CreateIdentifierAssertion(ctx, arg))
		listed, err := q.ListFetchKeyAssertions(ctx, &key)
		require.NoError(t, err)
		if len(listed) != 1 || listed[0].InstrumentID != nil || !listed[0].AssertedOn.Equal(on) {
			t.Errorf("ListFetchKeyAssertions = %+v, want the negative assertion on %s", listed, on.Format("2006-01-02"))
		}
		arg.ID = db.NewID()
		if err := q.CreateIdentifierAssertion(ctx, arg); !sqlstate(err, pgerrcode.UniqueViolation) {
			t.Errorf("CreateIdentifierAssertion of the same day twice: err = %v, want a unique violation", err)
		}
	})

	t.Run("an attestation goes with its run", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		f := newFetching(t, q)
		admin := newUser(t, q, "attest-"+f.user.Email)
		run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestation, Trigger: gen.RunTriggerAdministrator})
		require.NoError(t, err)
		arg := assertion()
		arg.RunID, arg.AttestedBy = &run.ID, &admin.ID
		require.NoError(t, q.CreateIdentifierAssertion(ctx, arg))
		_, err = tx.Exec(ctx, "DELETE FROM runs WHERE id = $1", run.ID)
		require.NoError(t, err)
		var n int
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM identifier_assertions WHERE id = $1", arg.ID).Scan(&n))
		if n != 0 {
			t.Errorf("assertion survived its attestation run")
		}
	})

	refusals := []struct {
		name string
		arg  func(q *gen.Queries, f fetching) gen.CreateIdentifierAssertionParams
	}{
		{name: "no source", arg: func(*gen.Queries, fetching) gen.CreateIdentifierAssertionParams { return assertion() }},
		{name: "a fetch key and a run", arg: func(q *gen.Queries, f fetching) gen.CreateIdentifierAssertionParams {
			key := servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
			arg := assertion()
			arg.FetchKeyID, arg.RunID, arg.AttestedBy = &key, &f.run.ID, &f.user.ID
			return arg
		}},
		{name: "a run with no administrator", arg: func(_ *gen.Queries, f fetching) gen.CreateIdentifierAssertionParams {
			arg := assertion()
			arg.RunID = &f.run.ID
			return arg
		}},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			// The violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			f := newFetching(t, q)
			if err := q.CreateIdentifierAssertion(ctx, tc.arg(q, f)); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateIdentifierAssertion(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}

// TestIdentifierEventChecks checks the shape the schema demands of an event
// and of coverage.
func TestIdentifierEventChecks(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		sql  string
	}{
		{name: "a rename with no new value", sql: "INSERT INTO identifier_events (id, fetch_key_id, domain, value, action, effective_on) VALUES ($1, $2, 'XNYS', 'FB', 'renamed', '2022-06-09')"},
		{name: "a retirement with a new value", sql: "INSERT INTO identifier_events (id, fetch_key_id, domain, value, action, to_value, effective_on) VALUES ($1, $2, 'XNYS', 'FB', 'retired', 'META', '2022-06-09')"},
		{name: "an event with no domain", sql: "INSERT INTO identifier_events (id, fetch_key_id, domain, value, action, effective_on) VALUES ($1, $2, '', 'FB', 'retired', '2022-06-09')"},
		{name: "coverage that ends as it starts", sql: "INSERT INTO identifier_event_coverage (id, fetch_key_id, domain, covered_from, covered_before) VALUES ($1, $2, 'XNYS', '2022-06-09', '2022-06-09')"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := begin(t)
			q := gen.New(tx)
			f := newFetching(t, q)
			key := servedKey(t, q, f.fetch, f.key, "US30303M1027")
			if _, err := tx.Exec(ctx, tc.sql, db.NewID(), key); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("%s: err = %v, want a check violation", tc.name, err)
			}
		})
	}
}
