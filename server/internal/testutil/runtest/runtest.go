// Package runtest runs a test's runs inline, over the queries it is given,
// in place of the run framework's goroutines.
package runtest

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run"
)

// Runner records each run over Q and executes its work before returning.
type Runner struct {
	Q *gen.Queries
}

// Start runs work to completion before it returns. An error from the prepare
// step or from work is returned, and an error from work fails the run.
func (r Runner) Start(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
	row, err := r.Q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: spec.UserID, Kind: spec.Kind, Trigger: spec.Trigger})
	if err != nil {
		return row, err
	}
	if spec.Prepare != nil {
		if err := spec.Prepare(ctx, row); err != nil {
			return row, err
		}
	}
	return row, r.execute(ctx, row, work)
}

// Child runs work to completion, as a child of parent, before it returns.
func (r Runner) Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
	row, err := r.Q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: parent.UserID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	if err != nil {
		return row, err
	}
	return row, r.execute(ctx, row, work)
}

// Sync runs work to completion before it returns, as a run user started.
func (r Runner) Sync(ctx context.Context, user uuid.UUID, kind gen.RunKind, trigger gen.RunTrigger, work run.Work) (gen.Run, error) {
	row, err := r.Q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user, Kind: kind, Trigger: trigger})
	if err != nil {
		return row, err
	}
	return row, r.execute(ctx, row, work)
}

func (r Runner) execute(ctx context.Context, row gen.Run, work run.Work) error {
	if _, err := r.Q.StartRun(ctx, row.ID); err != nil {
		return err
	}
	if err := work(ctx, row); err != nil {
		return errors.Join(err, r.Q.FailRun(ctx, gen.FailRunParams{ID: row.ID, Error: err.Error()}))
	}
	return r.Q.CompleteRun(ctx, row.ID)
}
