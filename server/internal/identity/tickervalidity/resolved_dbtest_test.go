//go:build dbtest

package tickervalidity

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/types"
)

// assertsOn records datasource asserting that v names instrument on day n,
// through a fetch key of its own, and returns the ticker's row.
func (f *fixture) assertsOn(t *testing.T, v types.MarketTicker, datasource string, n int, instr *uuid.UUID) gen.Ticker {
	t.Helper()
	ctx := context.Background()
	row := dbtest.Ticker(t, f.q, v)
	key := db.NewID()
	sent := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: v.Value}
	require.NoError(t, f.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: f.fetches[datasource], UserID: &f.user.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: &sent.Type, SentDomain: sent.Domain, SentValue: &sent.Value, AnsweredAt: ptr.To(day(n)),
	}))
	require.NoError(t, f.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, TickerID: row.ID, Market: v.Market, InstrID: instr, AssertedFrom: day(n), AssertedBefore: day(n).AddDate(0, 0, 1),
	}))
	return row
}

// resolved is a row of resolved_tickers as a test compares it.
type resolved struct {
	Value        string
	Instr        uuid.UUID
	From, Before *time.Time
}

func (f *fixture) resolved(t *testing.T, instrs ...uuid.UUID) []resolved {
	t.Helper()
	rows, err := f.q.ListResolvedTickers(context.Background(), instrs)
	require.NoError(t, err)
	var out []resolved
	for _, r := range rows {
		out = append(out, resolved{Value: r.Value, Instr: r.InstrID, From: r.ValidFrom, Before: r.ValidBefore})
	}
	return out
}

func (f *fixture) rebuild(t *testing.T, ids ...uuid.UUID) {
	t.Helper()
	require.NoError(t, Rebuild(context.Background(), f.q, ids))
}

// TestRebuildReadsOtherTickers checks that a rebuild of one ticker weighs
// every ticker that names the instrument, though it is given only the one.
func TestRebuildReadsOtherTickers(t *testing.T) {
	f := newFixture(t)
	old := types.MarketTicker{Market: "US", Value: "OLD" + uuid.NewString()[:8]}
	f.assertsOn(t, old, f.alpha, 1, &f.a)
	f.assertsOn(t, old, f.alpha, 5, &f.b)
	f.rebuild(t, f.assertsOn(t, f.foo, f.alpha, 6, &f.a).ID)
	want := []resolved{{Value: f.foo.Value, Instr: f.a}}
	if diff := cmp.Diff(want, f.resolved(t, f.a)); diff != "" {
		t.Errorf("rows mismatch (-want +got):\n%s", diff)
	}
}

// TestRebuildEndsTheEarlierInstr checks that when b takes a ticker from a, a
// rebuild of the ticker ends a's row and opens b's.
func TestRebuildEndsTheEarlierInstr(t *testing.T) {
	f := newFixture(t)
	foo := f.assertsOn(t, f.foo, f.alpha, 1, &f.a)
	f.rebuild(t, foo.ID)
	f.assertsOn(t, f.foo, f.alpha, 5, &f.b)
	f.rebuild(t, foo.ID)
	want := []resolved{
		{Value: f.foo.Value, Instr: f.a, Before: ptr.To(day(5))},
		{Value: f.foo.Value, Instr: f.b, From: ptr.To(day(5))},
	}
	got := f.resolved(t, f.a, f.b)
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("rows mismatch (-want +got):\n%s", diff)
	}
}

// TestRebuildDropsAPairWithoutFacts checks that a rebuild deletes the row of
// an instrument whose facts are gone.
func TestRebuildDropsAPairWithoutFacts(t *testing.T) {
	f := newFixture(t)
	foo := f.assertsOn(t, f.foo, f.alpha, 1, &f.a)
	f.rebuild(t, foo.ID)
	f.exec(t, "DELETE FROM identifier_assertions WHERE ticker_id = $1", foo.ID)
	f.rebuild(t, foo.ID)
	if got := f.resolved(t, f.a); len(got) != 0 {
		t.Errorf("rows = %v, want none", got)
	}
}

// TestRebuildStoresValidity checks that a rebuild stores the ticker validity
// that the facts imply, and replaces it when the facts change.
func TestRebuildStoresValidity(t *testing.T) {
	f := newFixture(t)
	foo := f.assertsOn(t, f.foo, f.alpha, 1, &f.a)
	f.rebuild(t, foo.ID)
	f.assertsOn(t, f.foo, f.alpha, 5, &f.b)
	f.rebuild(t, foo.ID)
	rows, err := ReadRows(context.Background(), f.q, f.foo)
	require.NoError(t, err)
	want := derive(rows.facts()).Intervals
	stored, err := Load(context.Background(), f.q, []types.MarketTicker{f.foo})
	require.NoError(t, err)
	if diff := cmp.Diff(want, stored[f.foo]); diff != "" {
		t.Errorf("stored ticker validity mismatch (-want +got):\n%s", diff)
	}
	if n := len(stored[f.foo]); n == 0 || stored[f.foo][0].From != nil || stored[f.foo][n-1].Before != nil {
		t.Errorf("stored ticker validity = %+v, want intervals open at both ends", stored[f.foo])
	}
}

// TestRebuildClearsANegativeTicker checks that a rebuild clears the ticker
// validity of a ticker whose facts name nothing.
func TestRebuildClearsANegativeTicker(t *testing.T) {
	f := newFixture(t)
	foo := f.assertsOn(t, f.foo, f.alpha, 1, &f.a)
	f.rebuild(t, foo.ID)
	f.exec(t, "DELETE FROM identifier_assertions WHERE ticker_id = $1", foo.ID)
	f.assertsOn(t, f.foo, f.alpha, 2, nil)
	f.rebuild(t, foo.ID)
	stored, err := Load(context.Background(), f.q, []types.MarketTicker{f.foo})
	require.NoError(t, err)
	if got := stored[f.foo]; got.Any() {
		t.Errorf("stored ticker validity = %+v, want none", got)
	}
}

// TestTickerValidityExcludesOverlap checks that the table refuses two ranges
// of one ticker that share a day, whichever instrument they name.
func TestTickerValidityExcludesOverlap(t *testing.T) {
	f := newFixture(t)
	foo := dbtest.Ticker(t, f.q, f.foo)
	f.exec(t, "INSERT INTO ticker_validity (ticker_id, market, instr_id, valid_from, valid_before, validity) VALUES ($1, $2, $3, NULL, $4, 'confirmed')", foo.ID, foo.Market, f.a, day(5))
	_, err := f.tx.Exec(context.Background(), "INSERT INTO ticker_validity (ticker_id, market, instr_id, valid_from, valid_before, validity) VALUES ($1, $2, $3, $4, NULL, 'provisional')", foo.ID, foo.Market, f.b, day(4))
	var pgErr *pgconn.PgError
	if !errors.As(err, &pgErr) || pgErr.Code != pgerrcode.ExclusionViolation {
		t.Errorf("overlapping insert error = %v, want an exclusion violation", err)
	}
}

// TestRebuildLocksTheInstr checks that two rebuilds of one instrument
// serialise, though they rebuild different tickers. The tickers are at
// different markets, so their rows cannot collide in a unique index. The
// second transaction can then wait only on the instrument's lock. Both
// transactions roll back. The instrument is the seeded ISK, which no other
// test names, so the lock delays no other test.
func TestRebuildLocksTheInstr(t *testing.T) {
	ctx := context.Background()
	open := func() pgx.Tx {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("rollback: %v", err)
			}
		})
		return tx
	}
	isk := func(q *gen.Queries) uuid.UUID {
		row, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "ISK"})
		require.NoError(t, err)
		return row.Instr.ID
	}
	attest := func(tx pgx.Tx, market string) uuid.UUID {
		q := gen.New(tx)
		admin, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleAdmin})
		require.NoError(t, err)
		run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
		require.NoError(t, err)
		row := dbtest.Ticker(t, q, types.MarketTicker{Market: market, Value: "ISK" + uuid.NewString()[:8]})
		instr := isk(q)
		require.NoError(t, q.CreateIdentifierAttestation(ctx, gen.CreateIdentifierAttestationParams{
			ID: db.NewID(), RunID: run.ID, AttestedBy: admin.ID, TickerID: row.ID, Market: market, InstrID: &instr, AssertedFrom: day(1), AssertedBefore: day(1).AddDate(0, 0, 1),
		}))
		return row.ID
	}
	one, two := open(), open()
	x, y := attest(one, "US"), attest(two, "XLON")
	require.NoError(t, Rebuild(ctx, gen.New(one), []uuid.UUID{x}))
	_, err := two.Exec(ctx, "SET LOCAL lock_timeout = '50ms'")
	require.NoError(t, err)
	err = Rebuild(ctx, gen.New(two), []uuid.UUID{y})
	var pgErr *pgconn.PgError
	require.ErrorAs(t, err, &pgErr)
	if pgErr.Code != pgerrcode.LockNotAvailable {
		t.Errorf("code = %s, want %s", pgErr.Code, pgerrcode.LockNotAvailable)
	}
}
