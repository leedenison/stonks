//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// TestRuns covers the transitions that depend on the state a row is in, and
// the read scoped to the user who started the run.
func TestRuns(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()

	owner, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: "runs-owner@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	other, err := q.CreateUser(ctx, gen.CreateUserParams{ID: db.NewID(), Email: "runs-other@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	create := func(kind gen.RunKind, parent *gen.Run) gen.Run {
		t.Helper()
		arg := gen.CreateRunParams{ID: db.NewID(), UserID: owner.ID, Kind: kind, Trigger: gen.RunTriggerUser}
		if parent != nil {
			arg.Trigger = gen.RunTriggerRun
			arg.ParentID = &parent.ID
		}
		row, err := q.CreateRun(ctx, arg)
		require.NoError(t, err)
		return row
	}
	state := func(id uuid.UUID) gen.Run {
		t.Helper()
		row, err := q.GetRun(ctx, gen.GetRunParams{ID: id, UserID: owner.ID})
		require.NoError(t, err)
		return row
	}

	pending := create(gen.RunKindStatement, nil)
	if pending.State != gen.RunStatePending || pending.StartedAt != nil || pending.FinishedAt != nil {
		t.Errorf("CreateRun = %+v, want pending with no started_at or finished_at", pending)
	}
	if _, err := q.GetRun(ctx, gen.GetRunParams{ID: pending.ID, UserID: other.ID}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetRun as another user: err = %v, want ErrNotFound", err)
	}

	// Completion and failure apply only to a running run.
	require.NoError(t, q.CompleteRun(ctx, pending.ID))
	require.NoError(t, q.FailRun(ctx, gen.FailRunParams{ID: pending.ID, Error: "boom"}))
	if got := state(pending.ID); got.State != gen.RunStatePending || got.Error != nil {
		t.Errorf("CompleteRun and FailRun on a pending run left %+v, want it pending", got)
	}

	running := create(gen.RunKindStatement, nil)
	started, err := q.StartRun(ctx, running.ID)
	require.NoError(t, err)
	if started.State != gen.RunStateRunning || started.StartedAt == nil {
		t.Errorf("StartRun = %+v, want running with started_at", started)
	}
	if _, err := q.StartRun(ctx, running.ID); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("StartRun on a running run: err = %v, want ErrNotFound", err)
	}

	completed := create(gen.RunKindStatement, nil)
	_, err = q.StartRun(ctx, completed.ID)
	require.NoError(t, err)
	require.NoError(t, q.CompleteRun(ctx, completed.ID))
	if got := state(completed.ID); got.State != gen.RunStateCompleted || got.FinishedAt == nil {
		t.Errorf("CompleteRun left %+v, want completed with finished_at", got)
	}

	failed := create(gen.RunKindResolution, &completed)
	_, err = q.StartRun(ctx, failed.ID)
	require.NoError(t, err)
	require.NoError(t, q.FailRun(ctx, gen.FailRunParams{ID: failed.ID, Error: "boom"}))
	if got := state(failed.ID); got.State != gen.RunStateFailed || got.FinishedAt == nil || got.Error == nil || *got.Error != "boom" || got.ParentID == nil || *got.ParentID != completed.ID {
		t.Errorf("FailRun left %+v, want failed with finished_at, the error and the parent", got)
	}

	n, err := q.InterruptRuns(ctx)
	require.NoError(t, err)
	if n != 2 {
		t.Errorf("InterruptRuns = %d rows, want 2", n)
	}
	for _, id := range []uuid.UUID{pending.ID, running.ID} {
		if got := state(id); got.State != gen.RunStateInterrupted || got.FinishedAt == nil {
			t.Errorf("InterruptRuns left %+v, want interrupted with finished_at", got)
		}
	}
	for _, id := range []uuid.UUID{completed.ID, failed.ID} {
		if got := state(id); got.State == gen.RunStateInterrupted {
			t.Errorf("InterruptRuns changed a terminal run: %+v", got)
		}
	}

	// The check constraint refuses a parent on a user's run, and aborts the
	// transaction, so it is the last statement.
	_, err = q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: owner.ID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser, ParentID: &completed.ID})
	if err == nil {
		t.Error("CreateRun with trigger user and a parent succeeded, want a check violation")
	}
}

// TestRunParent checks that a run names as parent only a run of its own user.
func TestRunParent(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	owner := newUser(t, q, "parent-owner@example.com")
	other := newUser(t, q, "parent-other@example.com")
	parent := newRun(t, q, owner)
	// The violation aborts the transaction, so it is the last statement.
	_, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: other.ID, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	if !sqlstate(err, pgerrcode.ForeignKeyViolation) {
		t.Errorf("CreateRun under another user's run: err = %v, want a foreign key violation", err)
	}
}
