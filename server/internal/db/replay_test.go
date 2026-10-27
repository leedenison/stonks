//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// names records one transaction against key, so a replay can select it.
func names(t *testing.T, q *gen.Queries, stmt gen.Stmt, key gen.StatedKey) {
	t.Helper()
	day := date(2026, 3, 1)
	_, err := q.CreateTxn(context.Background(), gen.CreateTxnParams{
		ID: db.NewID(), UserID: key.UserID, Broker: gen.BrokerIbkr, StmtID: stmt.ID, StatedKeyID: key.ID,
		OrderDate: day, SettlementDate: day, AsAt: day, Quantity: decimal.NewFromInt(1),
	})
	require.NoError(t, err)
}

// newResolution records a resolve statement keys run under statement.
func newResolution(t *testing.T, q *gen.Queries, user gen.User, stmt gen.Stmt) gen.Run {
	t.Helper()
	row, err := q.CreateRun(context.Background(), gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindResolveStmtKeys, Trigger: gen.RunTriggerRun, ParentID: &stmt.ID})
	require.NoError(t, err)
	return row
}

// resolved records key's outcome under run.
func resolved(t *testing.T, q *gen.Queries, run gen.Run, key gen.StatedKey, outcome gen.ResolutionOutcome) {
	t.Helper()
	arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: *run.UserID, StatedKeyID: key.ID, Outcome: outcome}
	if outcome != gen.ResolutionOutcomeMatched {
		arg.Reasons = []string{string(outcome)}
	}
	_, err := q.CreateResolutionKey(context.Background(), arg)
	require.NoError(t, err)
}

// TestListUnavailableKeys checks that a key is selected on its latest
// outcome alone, from the keys of the source run: the keys its statement
// states, whichever run resolved them, or the keys it resolved. A key no
// transaction names or no run resolved is left out, and so is every key of
// another user.
func TestListUnavailableKeys(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "unavailable@example.com")
	stmt, later := newStmt(t, q, user), newStmt(t, q, user)
	first, second := newResolution(t, q, user, stmt), newResolution(t, q, user, stmt)
	third := newResolution(t, q, user, later)
	healed := newStatedKey(t, q, user, stmt)
	broken := newStatedKey(t, q, user, stmt)
	idle := newStatedKey(t, q, user, stmt)
	fresh := newStatedKey(t, q, user, stmt)
	outside := newStatedKey(t, q, user, later)
	shared := newStatedKey(t, q, user, stmt)
	link(t, q, later, shared)
	for _, k := range []gen.StatedKey{healed, broken, fresh, shared} {
		names(t, q, stmt, k)
	}
	names(t, q, later, outside)
	resolved(t, q, first, healed, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, second, healed, gen.ResolutionOutcomeMatched)
	resolved(t, q, first, broken, gen.ResolutionOutcomeMatched)
	resolved(t, q, second, broken, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, first, idle, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, first, shared, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, third, outside, gen.ResolutionOutcomeUnavailable)

	tests := []struct {
		name   string
		source uuid.UUID
		user   uuid.UUID
		want   []uuid.UUID
	}{
		{name: "a statement", source: stmt.ID, user: user.ID, want: []uuid.UUID{broken.ID, shared.ID}},
		{name: "a resolution", source: second.ID, user: user.ID, want: []uuid.UUID{broken.ID}},
		{name: "a later statement stating a key it did not resolve", source: later.ID, user: user.ID, want: []uuid.UUID{outside.ID, shared.ID}},
		{name: "a sibling resolution", source: third.ID, user: user.ID, want: []uuid.UUID{outside.ID}},
		{name: "another user", source: stmt.ID, user: db.NewID(), want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rows, err := q.ListUnavailableKeys(ctx, gen.ListUnavailableKeysParams{UserID: tc.user, SourceID: tc.source})
			require.NoError(t, err)
			var got []uuid.UUID
			for _, r := range rows {
				got = append(got, r.StatedKey.ID)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("ListUnavailableKeys mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestListKeysUncoveredBy checks that an unresolved key and a key on an
// instrument the datasource has not covered are selected, and that a key the
// datasource covered, a key on reference data, a dated key, and a key no
// transaction names are left out.
func TestListKeysUncoveredBy(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "uncovered@example.com")
	stmt := newStmt(t, q, user)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
	require.NoError(t, err)
	alpha, beta := newDatasource(t, q, "alpha", 10), newDatasource(t, q, "beta", 20)
	fetch := newFetch(t, q, user, run, alpha)
	associate := func(key gen.StatedKey, listing gen.Listing, via gen.Identifier, arbiter gen.Arbiter) {
		t.Helper()
		arg := gen.SetStatedKeyAssociationParams{ID: key.ID, UserID: user.ID, InstrID: &listing.InstrID, ListingID: &listing.ID, ViaID: &via.ID, Arbiter: arbiter}
		_, err := q.SetStatedKeyAssociation(ctx, arg)
		require.NoError(t, err)
	}
	fetched := func(isin string) (gen.Listing, gen.Identifier, uuid.UUID) {
		t.Helper()
		key := servedKey(t, q, fetch, newStatedKey(t, q, user, stmt), isin)
		instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock, FetchKeyID: &key})
		require.NoError(t, err)
		listing, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: "USD", FetchKeyID: &key})
		require.NoError(t, err)
		via := newIdentifier(t, q, instr, isin)
		return listing, via, key
	}

	unresolved := newStatedKey(t, q, user, stmt)
	covered := newStatedKey(t, q, user, stmt)
	uncovered := newStatedKey(t, q, user, stmt)
	cash := newStatedKey(t, q, user, stmt)
	arbitrated := newStatedKey(t, q, user, stmt)
	dated := newStatedKey(t, q, user, stmt)
	newStatedKey(t, q, user, stmt)
	for _, k := range []gen.StatedKey{unresolved, covered, uncovered, cash, arbitrated, dated} {
		names(t, q, stmt, k)
	}
	listing, via, key := fetched("US0000000001")
	associate(covered, listing, via, gen.ArbiterDatasource)
	require.NoError(t, q.UpsertIdentityCoverage(ctx, gen.UpsertIdentityCoverageParams{InstrID: listing.InstrID, Datasource: alpha.Name, FetchKeyID: key}))
	listing, via, _ = fetched("US0000000002")
	associate(uncovered, listing, via, gen.ArbiterDatasource)
	usd, usdVia := cashListing(t, q, "USD")
	associate(cash, usd, usdVia, gen.ArbiterStated)
	// The user's choice stands, so neither scope selects the arbitrated
	// key, though no datasource covers its instrument.
	listing, via, _ = fetched("US0000000003")
	associate(arbitrated, listing, via, gen.ArbiterUser)
	_, err = q.SetStatedKeyDatedAssociation(ctx, gen.SetStatedKeyDatedAssociationParams{ID: dated.ID, UserID: user.ID, ViaDomain: "XNAS", ViaValue: "META", Arbiter: gen.ArbiterDatasource})
	require.NoError(t, err)

	source := gen.ListKeysUncoveredByParams{UserID: user.ID, SourceID: stmt.ID, Datasource: alpha.Name}
	rows, err := q.ListKeysUncoveredBy(ctx, source)
	require.NoError(t, err)
	if len(rows) != 2 || rows[0].StatedKey.ID != unresolved.ID || rows[1].StatedKey.ID != uncovered.ID {
		t.Errorf("ListKeysUncoveredBy(alpha) = %+v, want %s then %s", rows, unresolved.ID, uncovered.ID)
	}
	source.Datasource = beta.Name
	rows, err = q.ListKeysUncoveredBy(ctx, source)
	require.NoError(t, err)
	if len(rows) != 3 || rows[0].StatedKey.ID != unresolved.ID || rows[1].StatedKey.ID != covered.ID || rows[2].StatedKey.ID != uncovered.ID {
		t.Errorf("ListKeysUncoveredBy(beta) = %+v, want %s, %s then %s", rows, unresolved.ID, covered.ID, uncovered.ID)
	}
	source.UserID = db.NewID()
	rows, err = q.ListKeysUncoveredBy(ctx, source)
	require.NoError(t, err)
	if len(rows) != 0 {
		t.Errorf("ListKeysUncoveredBy for another user = %+v, want none", rows)
	}
}

// TestReplays checks that a replay reads back with the administrator's
// email, and that it names a run of its own user.
func TestReplays(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "replayed@example.com")
	admin := newUser(t, q, "replayer@example.com")
	stmt := newStmt(t, q, user)
	ds := newDatasource(t, q, "alpha", 10)
	run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindReplayRun, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	require.NoError(t, q.CreateReplay(ctx, gen.CreateReplayParams{ID: run.ID, UserID: user.ID, SourceID: stmt.ID, Datasource: &ds.Name, StartedBy: admin.ID}))
	got, err := q.GetReplay(ctx, run.ID)
	require.NoError(t, err)
	if got.Replay.SourceID != stmt.ID || got.Replay.Datasource == nil || *got.Replay.Datasource != ds.Name || got.StartedByEmail != admin.Email {
		t.Errorf("GetReplay = %+v, want source %s, datasource %s, started by %s", got, stmt.ID, ds.Name, admin.Email)
	}

	other := newUser(t, q, "other@example.com")
	foreign := newStmt(t, q, other)
	run, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindReplayRun, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	if err := q.CreateReplay(ctx, gen.CreateReplayParams{ID: run.ID, UserID: user.ID, SourceID: foreign.ID, StartedBy: admin.ID}); err == nil {
		t.Error("CreateReplay over another user's run: err = nil, want a constraint violation")
	}
}
