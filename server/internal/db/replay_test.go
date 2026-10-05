//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// names records one transaction against key, so a replay can select it.
func names(t *testing.T, q *gen.Queries, statement gen.Statement, key gen.StatedKey) {
	t.Helper()
	day := date(2026, 3, 1)
	_, err := q.CreateTransaction(context.Background(), gen.CreateTransactionParams{
		ID: db.NewID(), UserID: key.UserID, Broker: gen.BrokerIbkr, StatementID: statement.ID, StatedKeyID: key.ID,
		OrderDate: day, SettlementDate: day, AsAt: day, Quantity: decimal.NewFromInt(1),
	})
	require.NoError(t, err)
}

// newResolution records a resolution run under statement.
func newResolution(t *testing.T, q *gen.Queries, user gen.User, statement gen.Statement) gen.Run {
	t.Helper()
	row, err := q.CreateRun(context.Background(), gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &statement.ID})
	require.NoError(t, err)
	return row
}

// resolved records key's outcome under run.
func resolved(t *testing.T, q *gen.Queries, run gen.Run, key gen.StatedKey, outcome gen.ResolutionOutcome) {
	t.Helper()
	arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: run.UserID, StatedKeyID: key.ID, Outcome: outcome}
	if outcome != gen.ResolutionOutcomeMatched {
		arg.Reason = ptr.To(string(outcome))
	}
	_, err := q.CreateResolutionKey(context.Background(), arg)
	require.NoError(t, err)
}

func keyIDs(keys ...gen.StatedKey) []uuid.UUID {
	ids := make([]uuid.UUID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	return ids
}

// TestListResolvedKeys checks that a resolution's keys are read in id order
// and scoped to its user.
func TestListResolvedKeys(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "resolved@example.com")
	statement := newStatement(t, q, user)
	run := newResolution(t, q, user, statement)
	one, two, other := newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement)
	resolved(t, q, run, two, gen.ResolutionOutcomeMatched)
	resolved(t, q, run, one, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, newResolution(t, q, user, statement), other, gen.ResolutionOutcomeMatched)

	rows, err := q.ListResolvedKeys(ctx, gen.ListResolvedKeysParams{RunID: run.ID, UserID: user.ID})
	require.NoError(t, err)
	if len(rows) != 2 || rows[0].StatedKey.ID != one.ID || rows[1].StatedKey.ID != two.ID {
		t.Errorf("ListResolvedKeys = %+v, want %s then %s", rows, one.ID, two.ID)
	}
	rows, err = q.ListResolvedKeys(ctx, gen.ListResolvedKeysParams{RunID: run.ID, UserID: db.NewID()})
	require.NoError(t, err)
	if len(rows) != 0 {
		t.Errorf("ListResolvedKeys for another user = %+v, want none", rows)
	}
}

// TestListUnavailableKeys checks that a key is selected on its latest
// outcome alone, that a key no transaction names or no run resolved is left
// out, and that only the ids given are read.
func TestListUnavailableKeys(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "unavailable@example.com")
	statement := newStatement(t, q, user)
	first, second := newResolution(t, q, user, statement), newResolution(t, q, user, statement)
	healed := newStatedKey(t, q, user, statement)
	broken := newStatedKey(t, q, user, statement)
	idle := newStatedKey(t, q, user, statement)
	fresh := newStatedKey(t, q, user, statement)
	outside := newStatedKey(t, q, user, statement)
	for _, k := range []gen.StatedKey{healed, broken, fresh, outside} {
		names(t, q, statement, k)
	}
	resolved(t, q, first, healed, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, second, healed, gen.ResolutionOutcomeMatched)
	resolved(t, q, first, broken, gen.ResolutionOutcomeMatched)
	resolved(t, q, second, broken, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, first, idle, gen.ResolutionOutcomeUnavailable)
	resolved(t, q, first, outside, gen.ResolutionOutcomeUnavailable)

	rows, err := q.ListUnavailableKeys(ctx, keyIDs(healed, broken, idle, fresh))
	require.NoError(t, err)
	if len(rows) != 1 || rows[0].StatedKey.ID != broken.ID {
		t.Errorf("ListUnavailableKeys = %+v, want only %s", rows, broken.ID)
	}
}

// TestListKeysUncoveredBy checks that an unresolved key and a key on an
// instrument the datasource has not covered are selected, and that a key the
// datasource covered, a key on reference data, and a key no transaction names
// are left out.
func TestListKeysUncoveredBy(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "uncovered@example.com")
	statement := newStatement(t, q, user)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	alpha, beta := newDatasource(t, q, "alpha", 10), newDatasource(t, q, "beta", 20)
	fetch := newFetch(t, q, user, run, alpha)
	associate := func(key gen.StatedKey, listing gen.Listing, via gen.Identifier) {
		t.Helper()
		arg := gen.SetStatedKeyAssociationParams{ID: key.ID, UserID: user.ID, InstrumentID: &listing.InstrumentID, ListingID: &listing.ID, ViaID: &via.ID, Validity: ptr.To(gen.ValidityConfirmed)}
		require.NoError(t, q.SetStatedKeyAssociation(ctx, arg))
	}
	fetched := func(isin string) (gen.Listing, gen.Identifier, uuid.UUID) {
		t.Helper()
		key := servedKey(t, q, fetch, newStatedKey(t, q, user, statement), isin)
		instrument, err := q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: gen.AssetClassStock, FetchKeyID: &key})
		require.NoError(t, err)
		listing, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: instrument.ID, Currency: "USD", FetchKeyID: &key})
		require.NoError(t, err)
		via := newIdentifier(t, q, instrument, isin)
		return listing, via, key
	}

	unresolved := newStatedKey(t, q, user, statement)
	covered := newStatedKey(t, q, user, statement)
	uncovered := newStatedKey(t, q, user, statement)
	cash := newStatedKey(t, q, user, statement)
	silent := newStatedKey(t, q, user, statement)
	for _, k := range []gen.StatedKey{unresolved, covered, uncovered, cash} {
		names(t, q, statement, k)
	}
	listing, via, key := fetched("US0000000001")
	associate(covered, listing, via)
	require.NoError(t, q.UpsertIdentityCoverage(ctx, gen.UpsertIdentityCoverageParams{InstrumentID: listing.InstrumentID, Datasource: alpha.Name, FetchKeyID: key}))
	listing, via, _ = fetched("US0000000002")
	associate(uncovered, listing, via)
	usd, usdVia := cashListing(t, q, "USD")
	associate(cash, usd, usdVia)

	ids := keyIDs(unresolved, covered, uncovered, cash, silent)
	rows, err := q.ListKeysUncoveredBy(ctx, gen.ListKeysUncoveredByParams{Ids: ids, Datasource: alpha.Name})
	require.NoError(t, err)
	if len(rows) != 2 || rows[0].StatedKey.ID != unresolved.ID || rows[1].StatedKey.ID != uncovered.ID {
		t.Errorf("ListKeysUncoveredBy(alpha) = %+v, want %s then %s", rows, unresolved.ID, uncovered.ID)
	}
	rows, err = q.ListKeysUncoveredBy(ctx, gen.ListKeysUncoveredByParams{Ids: ids, Datasource: beta.Name})
	require.NoError(t, err)
	if len(rows) != 3 || rows[0].StatedKey.ID != unresolved.ID || rows[1].StatedKey.ID != covered.ID || rows[2].StatedKey.ID != uncovered.ID {
		t.Errorf("ListKeysUncoveredBy(beta) = %+v, want %s, %s then %s", rows, unresolved.ID, covered.ID, uncovered.ID)
	}
}

// TestReplays checks that a replay reads back with the administrator's
// email, and that it names a run of its own user.
func TestReplays(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "replayed@example.com")
	admin := newUser(t, q, "replayer@example.com")
	statement := newStatement(t, q, user)
	ds := newDatasource(t, q, "alpha", 10)
	run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	require.NoError(t, q.CreateReplay(ctx, gen.CreateReplayParams{ID: run.ID, UserID: user.ID, SourceID: statement.ID, Datasource: &ds.Name, StartedBy: admin.ID}))
	got, err := q.GetReplay(ctx, run.ID)
	require.NoError(t, err)
	if got.Replay.SourceID != statement.ID || got.Replay.Datasource == nil || *got.Replay.Datasource != ds.Name || got.StartedByEmail != admin.Email {
		t.Errorf("GetReplay = %+v, want source %s, datasource %s, started by %s", got, statement.ID, ds.Name, admin.Email)
	}

	other := newUser(t, q, "other@example.com")
	foreign := newStatement(t, q, other)
	run, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	if err := q.CreateReplay(ctx, gen.CreateReplayParams{ID: run.ID, UserID: user.ID, SourceID: foreign.ID, StartedBy: admin.ID}); err == nil {
		t.Error("CreateReplay over another user's run: err = nil, want a constraint violation")
	}
}
