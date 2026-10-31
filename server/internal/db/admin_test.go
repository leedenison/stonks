//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestListUserRuns checks each filter, the grouping of matches under their
// top-level runs with the path to them, and the page boundary between
// top-level runs.
func TestListUserRuns(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	owner := newUser(t, q, "admin-runs-owner@example.com")
	admin := newUser(t, q, "admin-runs-admin@example.com")
	stmt := newRun(t, q, owner)
	child, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: &owner.ID, Kind: gen.RunKindResolveStmtKeys, Trigger: gen.RunTriggerRun, ParentID: &stmt.ID,
	})
	require.NoError(t, err)
	started, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: &admin.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerAdministrator,
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
	resolution := gen.RunKindResolveStmtKeys
	administrator := gen.RunTriggerAdministrator
	pending := gen.RunStatePending
	tests := []struct {
		name string
		arg  gen.ListUserRunsParams
		want []uuid.UUID
	}{
		{name: "user, the top-level run then its child", arg: gen.ListUserRunsParams{UserID: &owner.ID}, want: []uuid.UUID{stmt.ID, child.ID}},
		{name: "kind, the match under the path to it", arg: gen.ListUserRunsParams{UserID: &owner.ID, Kind: &resolution}, want: []uuid.UUID{stmt.ID, child.ID}},
		{name: "trigger", arg: gen.ListUserRunsParams{Trigger: &administrator, UserID: &admin.ID}, want: []uuid.UUID{started.ID}},
		{name: "state", arg: gen.ListUserRunsParams{UserID: &owner.ID, State: &pending}, want: []uuid.UUID{stmt.ID, child.ID}},
		{name: "first page holds a whole family", arg: gen.ListUserRunsParams{UserID: &owner.ID, Lim: 1}, want: []uuid.UUID{stmt.ID, child.ID}},
		{name: "next page", arg: gen.ListUserRunsParams{UserID: &owner.ID, Before: &stmt.ID}, want: nil},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if diff := cmp.Diff(tc.want, ids(list(tc.arg))); diff != "" {
				t.Errorf("ListUserRuns(%+v) mismatch (-want +got):\n%s", tc.arg, diff)
			}
		})
	}

	byKind := list(gen.ListUserRunsParams{UserID: &owner.ID, Kind: &resolution})
	if byKind[0].Matched || !byKind[1].Matched || byKind[0].RootID != stmt.ID || byKind[1].RootID != stmt.ID {
		t.Errorf("ListUserRuns by kind = %+v, want the statement unmatched and the child matched, both under the statement", byKind)
	}

	ancestors, err := q.ListRunAncestors(ctx, child.ID)
	require.NoError(t, err)
	if len(ancestors) != 1 || ancestors[0].Run.ID != stmt.ID || ancestors[0].RunDetail.Email == nil || *ancestors[0].RunDetail.Email != owner.Email {
		t.Errorf("ListRunAncestors(child) = %+v, want the statement with its user", ancestors)
	}
	if top, err := q.ListRunAncestors(ctx, stmt.ID); err != nil || len(top) != 0 {
		t.Errorf("ListRunAncestors(top-level) = %v, %v, want none", top, err)
	}
	descendants, err := q.ListRunDescendants(ctx, &stmt.ID)
	require.NoError(t, err)
	if len(descendants) != 1 || descendants[0].Run.ID != child.ID || descendants[0].RunDetail.Email == nil || *descendants[0].RunDetail.Email != owner.Email {
		t.Errorf("ListRunDescendants(statement) = %+v, want the child with its user", descendants)
	}

	all := ids(list(gen.ListUserRunsParams{}))
	for _, id := range []uuid.UUID{stmt.ID, child.ID, started.ID} {
		if !slices.Contains(all, id) {
			t.Errorf("ListUserRuns with no filter = %v, want it to include %s", all, id)
		}
	}

	got, err := q.GetUserRun(ctx, started.ID)
	require.NoError(t, err)
	if got.RunDetail.Email == nil || *got.RunDetail.Email != admin.Email || got.Run.Trigger != gen.RunTriggerAdministrator {
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
		row, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &owner.ID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
		require.NoError(t, err)
		return row
	}
	var roots, children []gen.Run
	for range 3 {
		root := newRun(t, q, owner)
		roots = append(roots, root)
		children = append(children, child(root, gen.RunKindResolveStmtKeys))
	}
	grandchild := child(children[2], gen.RunKindDatasourceFetch)
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

// TestRunFetchColumns checks that every listing of runs names the datasource,
// endpoint and kind of a datasource fetch run, and leaves them unset for
// another run.
func TestRunFetchColumns(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	owner := newUser(t, q, "run-fetch@example.com")
	root := newRun(t, q, owner)
	ds := newDatasource(t, q, "run-fetch", 100)
	fetch := newFetch(t, q, owner, root, ds)
	type columns struct {
		Datasource, Endpoint *string
		Kind                 *gen.FetchKind
	}
	want := map[uuid.UUID]columns{
		root.ID:  {},
		fetch.ID: {ptr.To(ds.Name), ptr.To("https://" + ds.Name + ".test"), ptr.To(gen.FetchKindIdentity)},
	}
	check := func(query string, id uuid.UUID, datasource, endpoint *string, kind *gen.FetchKind) {
		t.Helper()
		if diff := cmp.Diff(want[id], columns{datasource, endpoint, kind}); diff != "" {
			t.Errorf("%s: run %s fetch columns mismatch (-want +got):\n%s", query, id, diff)
		}
	}

	roots, err := q.ListRootRuns(ctx, gen.ListRootRunsParams{Lim: 1})
	require.NoError(t, err)
	for _, r := range roots {
		check("ListRootRuns", r.Run.ID, r.RunDetail.Datasource, r.RunDetail.Endpoint, r.RunDetail.FetchKind)
	}
	users, err := q.ListUserRuns(ctx, gen.ListUserRunsParams{UserID: &owner.ID, Lim: 1})
	require.NoError(t, err)
	for _, r := range users {
		check("ListUserRuns", r.Run.ID, r.RunDetail.Datasource, r.RunDetail.Endpoint, r.RunDetail.FetchKind)
	}
	if len(roots) != 2 || len(users) != 2 {
		t.Errorf("ListRootRuns and ListUserRuns returned %d and %d runs, want 2 each", len(roots), len(users))
	}
	below, err := q.ListRunDescendants(ctx, &root.ID)
	require.NoError(t, err)
	require.Len(t, below, 1)
	check("ListRunDescendants", below[0].Run.ID, below[0].RunDetail.Datasource, below[0].RunDetail.Endpoint, below[0].RunDetail.FetchKind)
	above, err := q.ListRunAncestors(ctx, fetch.ID)
	require.NoError(t, err)
	require.Len(t, above, 1)
	check("ListRunAncestors", above[0].Run.ID, above[0].RunDetail.Datasource, above[0].RunDetail.Endpoint, above[0].RunDetail.FetchKind)
	got, err := q.GetUserRun(ctx, fetch.ID)
	require.NoError(t, err)
	check("GetUserRun", got.Run.ID, got.RunDetail.Datasource, got.RunDetail.Endpoint, got.RunDetail.FetchKind)
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

// TestRunItems checks that each kind's items carry their stated key and
// page in the order the run wrote them: a statement's by ordinal, and a
// resolution's and a fetch's by stated key id.
func TestRunItems(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "admin-items@example.com")
	stmt := newStmt(t, q, user)
	parent, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
	require.NoError(t, err)
	one, two := newStatedKey(t, q, user, stmt), newStatedKey(t, q, user, stmt)
	one32 := int32(1)

	for _, k := range []gen.StatedKey{two, one} {
		_, err = q.CreateResolutionKey(ctx, gen.CreateResolutionKeyParams{
			RunID: parent.ID, UserID: user.ID, StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised, Reasons: []string{"r"},
		})
		require.NoError(t, err)
	}
	resolved, err := q.ListResolutionItems(ctx, gen.ListResolutionItemsParams{RunID: parent.ID, Lim: &one32})
	require.NoError(t, err)
	if len(resolved) != 1 || resolved[0].StatedKey.ID != one.ID || resolved[0].ResolutionKey.Outcome != gen.ResolutionOutcomeUnrecognised {
		t.Errorf("ListResolutionItems first page = %+v, want the first key, unrecognised", resolved)
	}
	resolved, err = q.ListResolutionItems(ctx, gen.ListResolutionItemsParams{RunID: parent.ID, After: &one.ID, Lim: &one32})
	require.NoError(t, err)
	if len(resolved) != 1 || resolved[0].StatedKey.ID != two.ID {
		t.Errorf("ListResolutionItems after the first key = %+v, want the second", resolved)
	}

	fetch := newFetch(t, q, user, parent, newDatasource(t, q, "admin-items", 10))
	servedKey(t, q, fetch, two, "US0378331005")
	servedKey(t, q, fetch, one, "GB00B03MLX29")
	// Items list in the order the fetch made them.
	fetched, err := q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: fetch.ID, Lim: &one32})
	require.NoError(t, err)
	if len(fetched) != 1 || *fetched[0].FetchKey.StatedKeyID != two.ID || fetched[0].FetchKey.Outcome != gen.FetchOutcomeServed {
		t.Errorf("ListFetchItems first page = %+v, want the first key made, served", fetched)
	}
	fetched, err = q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: fetch.ID, After: &fetched[0].FetchKey.ID})
	require.NoError(t, err)
	if len(fetched) != 1 || *fetched[0].FetchKey.StatedKeyID != one.ID {
		t.Errorf("ListFetchItems after the first key = %+v, want the second", fetched)
	}

	for _, ordinal := range []int32{2, 0, 1} {
		require.NoError(t, q.CreateStmtItem(ctx, gen.CreateStmtItemParams{StmtID: stmt.ID, UserID: user.ID, Ordinal: ordinal, Reason: "r", Stated: []byte(`{}`)}))
	}
	two32 := int32(2)
	items, err := q.ListStmtItems(ctx, gen.ListStmtItemsParams{StmtID: stmt.ID, UserID: user.ID, After: ptr.To(int32(0)), Lim: &two32})
	require.NoError(t, err)
	var ordinals []int32
	for _, it := range items {
		ordinals = append(ordinals, it.Ordinal)
	}
	if diff := cmp.Diff([]int32{1, 2}, ordinals); diff != "" {
		t.Errorf("ListStmtItems after ordinal 0 mismatch (-want +got):\n%s", diff)
	}
}

// TestFindingsAndBlocks checks the open and cleared listings of findings and
// blocks, that a finding is not cleared alone where it reports a block, and
// that the datasource settings leave the credential out.
func TestFindingsAndBlocks(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "admin-findings@example.com")
	stmt := newStmt(t, q, user)
	parent, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
	require.NoError(t, err)
	ds, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{
		Name: "admin-findings", Enabled: true, Precedence: 10, Credential: ptr.To("secret"), Endpoint: ptr.To("http://stub"),
	})
	require.NoError(t, err)
	fetch := newFetch(t, q, user, parent, ds)
	key := servedKey(t, q, fetch, newStatedKey(t, q, user, stmt), "GB00B03MLX29")

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

	reported, err := q.ClearFinding(ctx, firstFinding)
	require.NoError(t, err)
	if reported.BlockID == nil || *reported.BlockID != firstBlock {
		t.Errorf("ClearFinding of a block's finding = %v, want the block %s", reported, firstBlock)
	}
	if _, err := q.ClearDatasourceBlock(ctx, firstBlock); err != nil {
		t.Fatalf("ClearDatasourceBlock() error = %v", err)
	}
	if _, err := q.ClearDatasourceBlock(ctx, db.NewID()); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("ClearDatasourceBlock of no block: err = %v, want ErrNotFound", err)
	}
	if _, err := q.ClearFinding(ctx, db.NewID()); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("ClearFinding of no finding: err = %v, want ErrNotFound", err)
	}

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
	if got.RunDetail.OpenFindings != 1 {
		t.Errorf("GetUserRun(fetch).OpenFindings = %d, want 1: one of two findings was cleared", got.RunDetail.OpenFindings)
	}
	listed, err := q.ListUserRuns(ctx, gen.ListUserRunsParams{UserID: &user.ID, Lim: 10})
	require.NoError(t, err)
	for _, r := range listed {
		if want := map[bool]int32{true: 1}[r.Run.ID == fetch.ID]; r.RunDetail.OpenFindings != want {
			t.Errorf("ListUserRuns row %s open findings = %d, want %d", r.Run.ID, r.RunDetail.OpenFindings, want)
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
	openBlocks, err := q.CountOpenBlocks(ctx)
	require.NoError(t, err)
	if openBlocks != 1 {
		t.Errorf("CountOpenBlocks = %d, want 1: one of two blocks was cleared", openBlocks)
	}
}

// TestFailedRuns checks that the failed listing holds the uncleared failed
// and interrupted runs alone, newest first, pages by id, that the count
// agrees, and that only a failed or interrupted run clears.
func TestFailedRuns(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "failed-runs@example.com")
	create := func(start bool) gen.Run {
		t.Helper()
		row, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
		require.NoError(t, err)
		if start {
			_, err = q.StartRun(ctx, row.ID)
			require.NoError(t, err)
		}
		return row
	}
	completed := create(true)
	require.NoError(t, q.CompleteRun(ctx, completed.ID))
	failed := create(true)
	require.NoError(t, q.FailRun(ctx, gen.FailRunParams{ID: failed.ID, Error: "boom"}))
	interrupted := create(true)
	_, err := q.InterruptRuns(ctx)
	require.NoError(t, err)
	cleared := create(true)
	require.NoError(t, q.FailRun(ctx, gen.FailRunParams{ID: cleared.ID, Error: "boom"}))
	state, err := q.ClearRun(ctx, cleared.ID)
	require.NoError(t, err)
	if state != gen.RunStateFailed {
		t.Errorf("ClearRun of a failed run = %s, want failed", state)
	}
	state, err = q.ClearRun(ctx, completed.ID)
	require.NoError(t, err)
	if state != gen.RunStateCompleted {
		t.Errorf("ClearRun of a completed run = %s, want completed", state)
	}
	if _, err := q.ClearRun(ctx, db.NewID()); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("ClearRun of no run: err = %v, want ErrNotFound", err)
	}
	create(false)
	create(true)

	ids := func(rows []gen.ListFailedRunsRow) []uuid.UUID {
		var out []uuid.UUID
		for _, r := range rows {
			out = append(out, r.Run.ID)
			if r.RunDetail.Email == nil || *r.RunDetail.Email != user.Email {
				t.Errorf("ListFailedRuns row %s email = %v, want %s", r.Run.ID, r.RunDetail.Email, user.Email)
			}
		}
		return out
	}
	rows, err := q.ListFailedRuns(ctx, gen.ListFailedRunsParams{Lim: 10})
	require.NoError(t, err)
	if diff := cmp.Diff([]uuid.UUID{interrupted.ID, failed.ID}, ids(rows)); diff != "" {
		t.Errorf("ListFailedRuns mismatch (-want +got):\n%s", diff)
	}
	if rows[0].Run.State != gen.RunStateInterrupted || rows[1].Run.State != gen.RunStateFailed {
		t.Errorf("ListFailedRuns states = %s, %s, want interrupted, failed", rows[0].Run.State, rows[1].Run.State)
	}
	rows, err = q.ListFailedRuns(ctx, gen.ListFailedRunsParams{Before: &interrupted.ID, Lim: 10})
	require.NoError(t, err)
	if diff := cmp.Diff([]uuid.UUID{failed.ID}, ids(rows)); diff != "" {
		t.Errorf("ListFailedRuns after the interrupted run mismatch (-want +got):\n%s", diff)
	}
	n, err := q.CountFailedRuns(ctx)
	require.NoError(t, err)
	if n != 2 {
		t.Errorf("CountFailedRuns = %d, want 2", n)
	}
}

// TestListFailedRunsIndex checks that the failed listing scans
// runs_failed_idx.
func TestListFailedRunsIndex(t *testing.T) {
	q, e := explain(t)
	_, err := q.ListFailedRuns(context.Background(), gen.ListFailedRunsParams{Lim: 10})
	require.NoError(t, err)
	if conds := indexConds(t, e.plan, "runs_failed_idx"); len(conds) == 0 {
		t.Errorf("runs_failed_idx is not scanned:\n%s", e.plan)
	}
}

// TestOpenFindings checks that the open listing holds the uncleared findings
// of the asked kinds alone, newest first, each with what its key states, pages
// by id, and that the counts group by kind.
func TestOpenFindings(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "open-findings@example.com")
	stmt := newStmt(t, q, user)
	parent, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
	require.NoError(t, err)
	key := newStatedKey(t, q, user, stmt)
	fetch := newFetch(t, q, user, parent, newDatasource(t, q, "open-findings", 10))
	fetched := servedKey(t, q, fetch, key, "GB00B03MLX29")
	create := func(arg gen.CreateFindingParams) uuid.UUID {
		t.Helper()
		arg.ID, arg.RunID, arg.Detail = db.NewID(), parent.ID, ptr.To("grounds")
		require.NoError(t, q.CreateFinding(ctx, arg))
		return arg.ID
	}
	contradiction := create(gen.CreateFindingParams{Kind: gen.FindingKindContradiction, StatedKeyID: &key.ID})
	cleared := create(gen.CreateFindingParams{Kind: gen.FindingKindContradiction, StatedKeyID: &key.ID})
	_, err = q.ClearFinding(ctx, cleared)
	require.NoError(t, err)
	create(gen.CreateFindingParams{Kind: gen.FindingKindDropped, StatedKeyID: &key.ID, FetchKeyID: &fetched, Step: ptr.To(gen.DropStepStated)})
	missed := create(gen.CreateFindingParams{Kind: gen.FindingKindMissedEvent, FetchKeyID: &fetched})

	kinds := []string{string(gen.FindingKindContradiction), string(gen.FindingKindMissedEvent)}
	rows, err := q.ListOpenFindings(ctx, gen.ListOpenFindingsParams{Kinds: kinds, Lim: 10})
	require.NoError(t, err)
	type row struct {
		ID    uuid.UUID
		KeyID *uuid.UUID
	}
	var got []row
	for _, r := range rows {
		got = append(got, row{ID: r.Finding.ID, KeyID: r.KeyID})
		if r.KeyID != nil {
			if diff := cmp.Diff(key.Identifiers, r.KeyIdentifiers); diff != "" {
				t.Errorf("ListOpenFindings row %s key identifiers mismatch (-want +got):\n%s", r.Finding.ID, diff)
			}
		}
	}
	if diff := cmp.Diff([]row{{ID: missed}, {ID: contradiction, KeyID: &key.ID}}, got); diff != "" {
		t.Errorf("ListOpenFindings mismatch (-want +got):\n%s", diff)
	}
	rows, err = q.ListOpenFindings(ctx, gen.ListOpenFindingsParams{Kinds: kinds, Before: &missed, Lim: 10})
	require.NoError(t, err)
	if len(rows) != 1 || rows[0].Finding.ID != contradiction {
		t.Errorf("ListOpenFindings after the missed event = %+v, want the contradiction alone", rows)
	}

	counts, err := q.CountOpenFindings(ctx)
	require.NoError(t, err)
	byKind := map[gen.FindingKind]int32{}
	for _, c := range counts {
		byKind[c.Kind] = c.Count
	}
	want := map[gen.FindingKind]int32{gen.FindingKindContradiction: 1, gen.FindingKindDropped: 1, gen.FindingKindMissedEvent: 1}
	if diff := cmp.Diff(want, byKind); diff != "" {
		t.Errorf("CountOpenFindings mismatch (-want +got):\n%s", diff)
	}
}

// TestListOpenFindingsIndex checks that the open listing searches
// findings_open_idx on the kind.
func TestListOpenFindingsIndex(t *testing.T) {
	q, e := explain(t)
	_, err := q.ListOpenFindings(context.Background(), gen.ListOpenFindingsParams{Kinds: []string{string(gen.FindingKindContradiction)}, Lim: 10})
	require.NoError(t, err)
	conds := indexConds(t, e.plan, "findings_open_idx")
	if !slices.ContainsFunc(conds, func(c string) bool { return strings.Contains(c, "kind = ANY") }) {
		t.Errorf("findings_open_idx is not searched on the kind:\n%s", e.plan)
	}
}

// TestListSystemRuns checks that the listings hold a system run, with no
// email, and the user run beneath it.
func TestListSystemRuns(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	owner := newUser(t, q, "system-runs@example.com")
	system, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	replay, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: &owner.ID, Kind: gen.RunKindReplayRun, Trigger: gen.RunTriggerRun, ParentID: &system.ID,
	})
	require.NoError(t, err)

	type row struct {
		ID    uuid.UUID
		Email *string
	}
	want := []row{{ID: system.ID}, {ID: replay.ID, Email: &owner.Email}}
	for _, arg := range []gen.ListUserRunsParams{{Lim: 10}, {Lim: 10, UserID: &owner.ID}} {
		rows, err := q.ListUserRuns(ctx, arg)
		require.NoError(t, err)
		var got []row
		for _, r := range rows {
			if r.RootID == system.ID {
				got = append(got, row{ID: r.Run.ID, Email: r.RunDetail.Email})
			}
		}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("ListUserRuns(%+v) mismatch (-want +got):\n%s", arg, diff)
		}
	}

	roots, err := q.ListRootRuns(ctx, gen.ListRootRunsParams{Lim: 10})
	require.NoError(t, err)
	var got []row
	for _, r := range roots {
		if r.RootID == system.ID {
			got = append(got, row{ID: r.Run.ID, Email: r.RunDetail.Email})
		}
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListRootRuns mismatch (-want +got):\n%s", diff)
	}

	top, err := q.GetUserRun(ctx, system.ID)
	require.NoError(t, err)
	if top.RunDetail.Email != nil || top.Run.UserID != nil {
		t.Errorf("GetUserRun(system) = %+v, want no user and no email", top)
	}
	ancestors, err := q.ListRunAncestors(ctx, replay.ID)
	require.NoError(t, err)
	if len(ancestors) != 1 || ancestors[0].Run.ID != system.ID || ancestors[0].RunDetail.Email != nil {
		t.Errorf("ListRunAncestors(replay) = %+v, want the system run with no email", ancestors)
	}
	descendants, err := q.ListRunDescendants(ctx, &system.ID)
	require.NoError(t, err)
	if len(descendants) != 1 || descendants[0].Run.ID != replay.ID || descendants[0].RunDetail.Email == nil || *descendants[0].RunDetail.Email != owner.Email {
		t.Errorf("ListRunDescendants(system) = %+v, want the replay with its user", descendants)
	}
}
