//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// TestFindings checks what each kind of finding carries: a block its block
// alone, the other kinds their stated key and grounds, and a dropped
// candidate its fetch key and one of the steps that is a finding. A finding
// is listed with what its key states.
func TestFindings(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "findings@example.com")
	statement := newStatement(t, q, user)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	fetch := newFetch(t, q, user, run, newDatasource(t, q, "findings", 10))
	key := newStatedKey(t, q, user, statement)
	fetchKey := servedKey(t, q, fetch, key, "GB00B03MLX29")

	dropped := gen.CreateFindingParams{
		ID: db.NewID(), RunID: run.ID, Kind: gen.FindingKindDropped, StatedKeyID: &key.ID, FetchKeyID: &fetchKey,
		Step: ptr.To(gen.DropStepStated), Detail: ptr.To("candidates in USD, not the stated GBP"),
	}
	require.NoError(t, q.CreateFinding(ctx, dropped))
	require.NoError(t, q.CreateFinding(ctx, gen.CreateFindingParams{
		ID: db.NewID(), RunID: run.ID, Kind: gen.FindingKindContradiction, StatedKeyID: &key.ID, Detail: ptr.To("ISIN and CUSIP name different instruments"),
	}))
	findings, err := q.ListRunFindings(ctx, []uuid.UUID{run.ID})
	require.NoError(t, err)
	if len(findings) != 2 {
		t.Fatalf("ListRunFindings = %d rows, want 2", len(findings))
	}
	if f := findings[0].Finding; f.Kind != gen.FindingKindDropped || f.Step == nil || *f.Step != gen.DropStepStated || f.FetchKeyID == nil || *f.FetchKeyID != fetchKey || f.Detail == nil {
		t.Errorf("dropped finding = %+v, want its step, fetch key and detail", f)
	}
	below := gen.CreateFindingParams{
		ID: db.NewID(), RunID: fetch.ID, Kind: gen.FindingKindContradiction, StatedKeyID: &key.ID, Detail: ptr.To("found below"),
	}
	require.NoError(t, q.CreateFinding(ctx, below))
	if rolled, err := q.ListRunFindings(ctx, []uuid.UUID{run.ID, fetch.ID}); err != nil || len(rolled) != 3 || rolled[2].Finding.ID != below.ID {
		t.Errorf("ListRunFindings(run, fetch) = %d rows, %v, want the run's two then the fetch's", len(rolled), err)
	}
	for _, f := range findings {
		if f.KeyDescription == nil || *f.KeyDescription != *key.Description || len(f.KeyIdentifiers) != 0 {
			t.Errorf("finding %s key = %v %v, want the key's description %q and no identifiers", f.Finding.Kind, f.KeyIdentifiers, f.KeyDescription, *key.Description)
		}
	}

	tests := []struct {
		name string
		arg  gen.CreateFindingParams
	}{
		{name: "dropped without a step", arg: gen.CreateFindingParams{Kind: gen.FindingKindDropped, FetchKeyID: &fetchKey, Detail: ptr.To("d")}},
		{name: "dropped without a fetch key", arg: gen.CreateFindingParams{Kind: gen.FindingKindDropped, Step: ptr.To(gen.DropStepStated), Detail: ptr.To("d")}},
		{name: "a block with detail", arg: gen.CreateFindingParams{Kind: gen.FindingKindBlock, Detail: ptr.To("d")}},
		{name: "a contradiction without detail", arg: gen.CreateFindingParams{Kind: gen.FindingKindContradiction}},
		{name: "a merge with a step", arg: gen.CreateFindingParams{Kind: gen.FindingKindMerged, Step: ptr.To(gen.DropStepStated), Detail: ptr.To("d")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			user := newUser(t, q, "findings-"+uuid.NewString()+"@example.com")
			statement := newStatement(t, q, user)
			run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
			require.NoError(t, err)
			key := newStatedKey(t, q, user, statement)
			tc.arg.ID, tc.arg.RunID = db.NewID(), run.ID
			if tc.arg.Kind != gen.FindingKindBlock {
				tc.arg.StatedKeyID = &key.ID
			}
			if tc.arg.FetchKeyID != nil {
				fetch := newFetch(t, q, user, run, newDatasource(t, q, "findings-"+uuid.NewString(), 10))
				tc.arg.FetchKeyID = ptr.To(servedKey(t, q, fetch, key, "GB00B03MLX29"))
			}
			if err := q.CreateFinding(ctx, tc.arg); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateFinding(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}
