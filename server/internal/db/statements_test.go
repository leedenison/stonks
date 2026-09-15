//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// TestStatements checks the statement read with its rejected count, the order of
// the list, and the scoping of both to the user.
func TestStatements(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	user := newUser(t, q, "statements@example.com")
	other := newUser(t, q, "statements-other@example.com")
	create := func(run gen.Run, rejected int) gen.Statement {
		t.Helper()
		row, err := q.CreateStatement(ctx, gen.CreateStatementParams{
			ID: run.ID, UserID: user.ID, Broker: gen.BrokerIbkr,
			OrderFrom: date(2026, time.March, 1), OrderBefore: date(2026, time.April, 1), RowCount: 3,
		})
		require.NoError(t, err)
		for i := range rejected {
			require.NoError(t, q.CreateStatementItem(ctx, gen.CreateStatementItemParams{
				StatementID: run.ID, UserID: user.ID, Ordinal: int32(i), Reason: "bad", Stated: []byte(`{"quantity":"1"}`),
			}))
		}
		return row
	}

	first, second := newStatement(t, q, user), newStatement(t, q, user)
	clean, rejected := create(first, 0), create(second, 2)

	got, err := q.GetStatement(ctx, gen.GetStatementParams{ID: second.ID, UserID: user.ID})
	require.NoError(t, err)
	if diff := cmp.Diff(gen.GetStatementRow{Statement: rejected, Run: second, Rejected: 2}, got); diff != "" {
		t.Errorf("GetStatement mismatch (-want +got):\n%s", diff)
	}
	if _, err := q.GetStatement(ctx, gen.GetStatementParams{ID: second.ID, UserID: other.ID}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetStatement as another user: err = %v, want ErrNotFound", err)
	}

	listed, err := q.ListStatements(ctx, user.ID)
	require.NoError(t, err)
	want := []gen.ListStatementsRow{{Statement: rejected, Run: second, Rejected: 2}, {Statement: clean, Run: first, Rejected: 0}}
	if diff := cmp.Diff(want, listed); diff != "" {
		t.Errorf("ListStatements mismatch, want newest first (-want +got):\n%s", diff)
	}
	if listed, err := q.ListStatements(ctx, other.ID); err != nil || len(listed) != 0 {
		t.Errorf("ListStatements as another user = %v, %v, want none", listed, err)
	}

	items, err := q.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: second.ID, UserID: user.ID})
	require.NoError(t, err)
	if len(items) != 2 || items[0].Ordinal != 0 || items[1].Ordinal != 1 || string(items[1].Stated) != `{"quantity": "1"}` {
		t.Errorf("ListStatementItems = %+v, want two items in ordinal order carrying the stated row", items)
	}
	if items, err := q.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: second.ID, UserID: other.ID}); err != nil || len(items) != 0 {
		t.Errorf("ListStatementItems as another user = %v, %v, want none", items, err)
	}

	// A violation aborts the transaction, so it is the last statement.
	if _, err := q.CreateStatement(ctx, gen.CreateStatementParams{ID: newStatement(t, q, user).ID, UserID: user.ID, Broker: gen.BrokerIbkr, OrderFrom: date(2026, time.April, 1), OrderBefore: date(2026, time.April, 1), RowCount: 0}); !sqlstate(err, pgerrcode.CheckViolation) {
		t.Errorf("CreateStatement with an empty period: err = %v, want a check violation", err)
	}
}

// TestResolutionKeys checks the checks tying an outcome to what it names.
func TestResolutionKeys(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "resolution@example.com")
	statement := newStatement(t, q, user)
	resolution, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &statement.ID})
	require.NoError(t, err)
	usd, err := q.GetListingByIdentifier(ctx, gen.GetListingByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: "USD"})
	require.NoError(t, err)
	newKey := func() gen.StatedKey {
		t.Helper()
		key, err := q.CreateStatedKey(ctx, gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: statement.ID, UserID: user.ID, Description: ptr(uuid.NewString()), Identifiers: []types.StatedIdentifier{}})
		require.NoError(t, err)
		return key
	}
	matched, rejected := newKey(), newKey()

	require.NoError(t, q.CreateResolutionKey(ctx, gen.CreateResolutionKeyParams{RunID: resolution.ID, UserID: user.ID, StatedKeyID: matched.ID, Outcome: gen.ResolutionOutcomeMatched, InstrumentID: &usd.Listing.InstrumentID, ListingID: &usd.Listing.ID}))
	require.NoError(t, q.CreateResolutionKey(ctx, gen.CreateResolutionKeyParams{RunID: resolution.ID, UserID: user.ID, StatedKeyID: rejected.ID, Outcome: gen.ResolutionOutcomeRejected, Reason: ptr("no currency")}))
	keys, err := q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: resolution.ID, UserID: user.ID})
	require.NoError(t, err)
	if len(keys) != 2 {
		t.Fatalf("ListResolutionKeys = %d rows, want 2", len(keys))
	}
	if keys[0].StatedKeyID != matched.ID || keys[0].Outcome != gen.ResolutionOutcomeMatched || keys[1].Reason == nil {
		t.Errorf("ListResolutionKeys = %+v, want the matched key then the rejected one with its reason", keys)
	}

	tests := []struct {
		name string
		arg  gen.CreateResolutionKeyParams
	}{
		{name: "rejected with an instrument", arg: gen.CreateResolutionKeyParams{Outcome: gen.ResolutionOutcomeRejected, Reason: ptr("x"), InstrumentID: &usd.Listing.InstrumentID}},
		{name: "rejected without a reason", arg: gen.CreateResolutionKeyParams{Outcome: gen.ResolutionOutcomeRejected}},
		{name: "matched without an instrument", arg: gen.CreateResolutionKeyParams{Outcome: gen.ResolutionOutcomeMatched}},
		{name: "created with a reason", arg: gen.CreateResolutionKeyParams{Outcome: gen.ResolutionOutcomeCreated, InstrumentID: &usd.Listing.InstrumentID, Reason: ptr("x")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			user := newUser(t, q, "resolution-"+uuid.NewString()+"@example.com")
			statement := newStatement(t, q, user)
			run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &statement.ID})
			require.NoError(t, err)
			key, err := q.CreateStatedKey(ctx, gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: statement.ID, UserID: user.ID, Identifiers: []types.StatedIdentifier{}})
			require.NoError(t, err)
			tc.arg.RunID, tc.arg.UserID, tc.arg.StatedKeyID = run.ID, user.ID, key.ID
			if err := q.CreateResolutionKey(ctx, tc.arg); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateResolutionKey(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}
