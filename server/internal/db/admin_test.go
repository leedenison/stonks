//go:build dbtest

package db_test

import (
	"context"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// TestListUserRuns checks each filter, the grouping of matches under their
// top-level runs with the path to them, and the page boundary between
// top-level runs.
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

	list := func(arg gen.ListUserRunsParams) []gen.ListUserRunsRow {
		t.Helper()
		if arg.Lim == 0 {
			arg.Lim = 10
		}
		rows, err := q.ListUserRuns(ctx, arg)
		require.NoError(t, err)
		return rows
	}
	ids := func(rows []gen.ListUserRunsRow) []uuid.UUID {
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
		{name: "user, the top-level run then its child", arg: gen.ListUserRunsParams{UserID: &owner.ID}, want: []uuid.UUID{statement.ID, child.ID}},
		{name: "kind, the match under the path to it", arg: gen.ListUserRunsParams{UserID: &owner.ID, Kind: &resolution}, want: []uuid.UUID{statement.ID, child.ID}},
		{name: "trigger", arg: gen.ListUserRunsParams{Trigger: &administrator, UserID: &admin.ID}, want: []uuid.UUID{started.ID}},
		{name: "state", arg: gen.ListUserRunsParams{UserID: &owner.ID, State: &pending}, want: []uuid.UUID{statement.ID, child.ID}},
		{name: "first page holds a whole family", arg: gen.ListUserRunsParams{UserID: &owner.ID, Lim: 1}, want: []uuid.UUID{statement.ID, child.ID}},
		{name: "next page", arg: gen.ListUserRunsParams{UserID: &owner.ID, Before: &statement.ID}, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ids(list(tc.arg))); diff != "" {
				t.Errorf("ListUserRuns(%+v) mismatch (-want +got):\n%s", tc.arg, diff)
			}
		})
	}

	byKind := list(gen.ListUserRunsParams{UserID: &owner.ID, Kind: &resolution})
	if byKind[0].Matched || !byKind[1].Matched || byKind[0].RootID != statement.ID || byKind[1].RootID != statement.ID {
		t.Errorf("ListUserRuns by kind = %+v, want the statement unmatched and the child matched, both under the statement", byKind)
	}

	ancestors, err := q.ListRunAncestors(ctx, child.ID)
	require.NoError(t, err)
	if len(ancestors) != 1 || ancestors[0].Run.ID != statement.ID || ancestors[0].Email != owner.Email {
		t.Errorf("ListRunAncestors(child) = %+v, want the statement with its user", ancestors)
	}
	if top, err := q.ListRunAncestors(ctx, statement.ID); err != nil || len(top) != 0 {
		t.Errorf("ListRunAncestors(top-level) = %v, %v, want none", top, err)
	}
	descendants, err := q.ListRunDescendants(ctx, &statement.ID)
	require.NoError(t, err)
	if len(descendants) != 1 || descendants[0].Run.ID != child.ID || descendants[0].Email != owner.Email {
		t.Errorf("ListRunDescendants(statement) = %+v, want the child with its user", descendants)
	}

	all := ids(list(gen.ListUserRunsParams{}))
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

// TestListRootRuns checks that a page holds whole families of its top-level
// runs, newest first and each run after its parent, and that before pages by
// top-level run.
func TestListRootRuns(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	owner := newUser(t, q, "root-runs@example.com")
	child := func(parent gen.Run, kind gen.RunKind) gen.Run {
		t.Helper()
		row, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: owner.ID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
		require.NoError(t, err)
		return row
	}
	var roots, children []gen.Run
	for range 3 {
		root := newRun(t, q, owner)
		roots = append(roots, root)
		children = append(children, child(root, gen.RunKindResolution))
	}
	grandchild := child(children[2], gen.RunKindFetch)
	// The test's rows are visible to its own transaction alone, so the
	// pages hold these runs and no others.
	page := func(arg gen.ListRootRunsParams) []uuid.UUID {
		t.Helper()
		rows, err := q.ListRootRuns(ctx, arg)
		require.NoError(t, err)
		var out []uuid.UUID
		for _, r := range rows {
			if !r.Matched {
				t.Errorf("row %s unmatched, want every row matched", r.Run.ID)
			}
			out = append(out, r.Run.ID)
		}
		return out
	}
	first := []uuid.UUID{roots[2].ID, children[2].ID, grandchild.ID, roots[1].ID, children[1].ID}
	if diff := cmp.Diff(first, page(gen.ListRootRunsParams{Lim: 2})); diff != "" {
		t.Errorf("first page mismatch (-want +got):\n%s", diff)
	}
	next := page(gen.ListRootRunsParams{Before: &roots[1].ID, Lim: 1})
	if diff := cmp.Diff([]uuid.UUID{roots[0].ID, children[0].ID}, next); diff != "" {
		t.Errorf("next page mismatch (-want +got):\n%s", diff)
	}

	descendants, err := q.ListRunDescendants(ctx, &roots[2].ID)
	require.NoError(t, err)
	var below []uuid.UUID
	for _, d := range descendants {
		below = append(below, d.Run.ID)
	}
	if diff := cmp.Diff([]uuid.UUID{children[2].ID, grandchild.ID}, below); diff != "" {
		t.Errorf("ListRunDescendants mismatch (-want +got):\n%s", diff)
	}
}

// TestListRootRunsIndex checks that the walk down from the top-level runs
// searches runs_parent_idx on the parent.
func TestListRootRunsIndex(t *testing.T) {
	q, e := explain(t)
	_, err := q.ListRootRuns(context.Background(), gen.ListRootRunsParams{Lim: 10})
	require.NoError(t, err)
	conds := indexConds(t, e.plan, "runs_parent_idx")
	if !slices.ContainsFunc(conds, func(c string) bool { return strings.Contains(c, "parent_id = ") }) {
		t.Errorf("runs_parent_idx is not searched on the parent:\n%s", e.plan)
	}
}

// TestRunItems checks that the items of a resolution and of a fetch each carry
// their stated key.
func TestRunItems(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "admin-items@example.com")
	statement := newStatement(t, q, user)
	parent, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	key := newStatedKey(t, q, user, statement)

	_, err = q.CreateResolutionKey(ctx, gen.CreateResolutionKeyParams{
		RunID: parent.ID, UserID: user.ID, StatedKeyID: key.ID, Outcome: gen.ResolutionOutcomeUnrecognised,
	})
	require.NoError(t, err)
	resolved, err := q.ListResolutionItems(ctx, parent.ID)
	require.NoError(t, err)
	if len(resolved) != 1 || resolved[0].StatedKey.ID != key.ID || resolved[0].ResolutionKey.Outcome != gen.ResolutionOutcomeUnrecognised {
		t.Errorf("ListResolutionItems = %+v, want the unrecognised key", resolved)
	}

	fetch := newFetch(t, q, user, parent, newDatasource(t, q, "admin-items", 10))
	servedKey(t, q, fetch, key, "GB00B03MLX29")
	fetched, err := q.ListFetchItems(ctx, fetch.ID)
	require.NoError(t, err)
	if len(fetched) != 1 || fetched[0].StatedKey.ID != key.ID || fetched[0].FetchKey.Outcome != gen.FetchOutcomeServed {
		t.Errorf("ListFetchItems = %+v, want the served key", fetched)
	}
}

// TestFindingsAndBlocks checks the open and cleared listings of findings and
// blocks, that a finding is not cleared alone where it reports a block, and
// that the datasource settings leave the credential out.
func TestFindingsAndBlocks(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "admin-findings@example.com")
	statement := newStatement(t, q, user)
	parent, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	ds, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{
		Name: "admin-findings", Enabled: true, Precedence: 10, Credential: ptr.To("secret"), Endpoint: ptr.To("http://stub"),
	})
	require.NoError(t, err)
	fetch := newFetch(t, q, user, parent, ds)
	key := servedKey(t, q, fetch, newStatedKey(t, q, user, statement), "GB00B03MLX29")

	open := func(value string) (block, finding uuid.UUID) {
		t.Helper()
		block, finding = db.NewID(), db.NewID()
		_, err := q.CreateDatasourceBlock(ctx, gen.CreateDatasourceBlockParams{
			ID: block, FindingID: finding, RunID: fetch.ID, Datasource: ds.Name, Kind: gen.FetchKindIdentity,
			Scope: gen.BlockScopeIdentifier, SentType: ptr.To(types.IdentifierTypeIsin), SentValue: ptr.To(value),
			Reason: "refused", FetchKeyID: key,
		})
		require.NoError(t, err)
		return block, finding
	}
	firstBlock, firstFinding := open("GB00B03MLX29")
	secondBlock, secondFinding := open("US0378331005")

	require.NoError(t, q.ClearFinding(ctx, firstFinding))
	if f, err := q.GetFinding(ctx, firstFinding); err != nil || f.ClearedAt != nil {
		t.Errorf("GetFinding after ClearFinding of a block's finding = %+v, %v, want it open", f, err)
	}
	require.NoError(t, q.ClearDatasourceBlock(ctx, firstBlock))

	rows, err := q.ListRunFindings(ctx, []uuid.UUID{fetch.ID})
	require.NoError(t, err)
	cleared := map[uuid.UUID]bool{}
	for _, r := range rows {
		cleared[r.Finding.ID] = r.Finding.ClearedAt != nil
	}
	if diff := cmp.Diff(map[uuid.UUID]bool{firstFinding: true, secondFinding: false}, cleared); diff != "" {
		t.Errorf("ListRunFindings cleared mismatch (-want +got):\n%s", diff)
	}
	got, err := q.GetUserRun(ctx, fetch.ID)
	require.NoError(t, err)
	if got.OpenFindings != 1 {
		t.Errorf("GetUserRun(fetch).OpenFindings = %d, want 1: one of two findings was cleared", got.OpenFindings)
	}
	listed, err := q.ListUserRuns(ctx, gen.ListUserRunsParams{UserID: &user.ID, Lim: 10})
	require.NoError(t, err)
	for _, r := range listed {
		if want := map[bool]int32{true: 1}[r.Run.ID == fetch.ID]; r.OpenFindings != want {
			t.Errorf("ListUserRuns row %s open findings = %d, want %d", r.Run.ID, r.OpenFindings, want)
		}
	}

	blocks, err := q.ListBlocks(ctx, gen.ListBlocksParams{IncludeCleared: true, Lim: 10})
	require.NoError(t, err)
	var ours []uuid.UUID
	for _, b := range blocks {
		if b.DatasourceBlock.Datasource == ds.Name {
			ours = append(ours, b.DatasourceBlock.ID)
			if b.FetchID != fetch.ID {
				t.Errorf("ListBlocks row %s names run %s, want the fetch %s", b.DatasourceBlock.ID, b.FetchID, fetch.ID)
			}
		}
	}
	if diff := cmp.Diff([]uuid.UUID{secondBlock, firstBlock}, ours); diff != "" {
		t.Errorf("ListBlocks including the cleared mismatch (-want +got):\n%s", diff)
	}
	held, err := q.ListBlocks(ctx, gen.ListBlocksParams{Lim: 10})
	require.NoError(t, err)
	for _, b := range held {
		if b.DatasourceBlock.ID == firstBlock {
			t.Errorf("ListBlocks of the open includes the cleared block %s", firstBlock)
		}
	}

	settings, err := q.ListDatasourceSettings(ctx)
	require.NoError(t, err)
	want := []gen.ListDatasourceSettingsRow{{Name: ds.Name, Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), HasCredential: true}}
	if diff := cmp.Diff(want, settings); diff != "" {
		t.Errorf("ListDatasourceSettings mismatch (-want +got):\n%s", diff)
	}
}
