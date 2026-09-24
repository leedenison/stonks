//go:build dbtest

package db_test

import (
	"context"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// TestListUserRuns checks each filter, the order and the page boundary of the
// listing across users.
func TestListUserRuns(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	owner := newUser(t, q, "admin-runs-owner@example.com")
	admin := newUser(t, q, "admin-runs-admin@example.com")
	statement := newRun(t, q, owner)
	child, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: owner.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &statement.ID,
	})
	require.NoError(t, err)
	started, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: admin.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerAdministrator,
	})
	require.NoError(t, err)

	ids := func(arg gen.ListUserRunsParams) []uuid.UUID {
		t.Helper()
		if arg.Lim == 0 {
			arg.Lim = 10
		}
		rows, err := q.ListUserRuns(ctx, arg)
		require.NoError(t, err)
		var out []uuid.UUID
		for _, r := range rows {
			out = append(out, r.Run.ID)
		}
		return out
	}
	resolution := gen.RunKindResolution
	administrator := gen.RunTriggerAdministrator
	pending := gen.RunStatePending
	tests := []struct {
		name string
		arg  gen.ListUserRunsParams
		want []uuid.UUID
	}{
		{name: "user, newest first", arg: gen.ListUserRunsParams{UserID: &owner.ID}, want: []uuid.UUID{child.ID, statement.ID}},
		{name: "kind", arg: gen.ListUserRunsParams{UserID: &owner.ID, Kind: &resolution}, want: []uuid.UUID{child.ID}},
		{name: "trigger", arg: gen.ListUserRunsParams{Trigger: &administrator, UserID: &admin.ID}, want: []uuid.UUID{started.ID}},
		{name: "state", arg: gen.ListUserRunsParams{UserID: &owner.ID, State: &pending}, want: []uuid.UUID{child.ID, statement.ID}},
		{name: "first page", arg: gen.ListUserRunsParams{UserID: &owner.ID, Lim: 1}, want: []uuid.UUID{child.ID}},
		{name: "next page", arg: gen.ListUserRunsParams{UserID: &owner.ID, Before: &child.ID}, want: []uuid.UUID{statement.ID}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ids(tc.arg)); diff != "" {
				t.Errorf("ListUserRuns(%+v) mismatch (-want +got):\n%s", tc.arg, diff)
			}
		})
	}

	all := ids(gen.ListUserRunsParams{})
	for _, id := range []uuid.UUID{statement.ID, child.ID, started.ID} {
		if !slices.Contains(all, id) {
			t.Errorf("ListUserRuns with no filter = %v, want it to include %s", all, id)
		}
	}

	got, err := q.GetUserRun(ctx, started.ID)
	require.NoError(t, err)
	if got.Email != admin.Email || got.Run.Trigger != gen.RunTriggerAdministrator {
		t.Errorf("GetUserRun = %+v, want the administrator's run and email", got)
	}
}

// TestRunItems checks that the items of a resolution and of a fetch carry the
// stated key each is about.
func TestRunItems(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "admin-items@example.com")
	statement := newStatement(t, q, user)
	parent, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	key := newStatedKey(t, q, user, statement)

	require.NoError(t, q.CreateResolutionKey(ctx, gen.CreateResolutionKeyParams{
		RunID: parent.ID, UserID: user.ID, StatedKeyID: key.ID, Outcome: gen.ResolutionOutcomeUnresolved,
	}))
	resolved, err := q.ListResolutionItems(ctx, parent.ID)
	require.NoError(t, err)
	if len(resolved) != 1 || resolved[0].StatedKey.ID != key.ID || resolved[0].ResolutionKey.Outcome != gen.ResolutionOutcomeUnresolved {
		t.Errorf("ListResolutionItems = %+v, want the unresolved key", resolved)
	}

	fetch := newFetch(t, q, user, parent, newDatasource(t, q, "admin-items", 10))
	servedKey(t, q, fetch, key, "GB00B03MLX29")
	fetched, err := q.ListFetchItems(ctx, fetch.ID)
	require.NoError(t, err)
	if len(fetched) != 1 || fetched[0].StatedKey.ID != key.ID || fetched[0].FetchKey.Outcome != gen.FetchOutcomeServed {
		t.Errorf("ListFetchItems = %+v, want the served key", fetched)
	}
}
