// Package run starts background work and records each piece of it as a run.
//
// A run is a row created before its work starts. The call starting it answers
// with the row, and progress and the outcome are read against it.
//
// A user's run executes in a goroutine of its own once the call starting it
// has answered, after carrying out its prepare step in the caller. A run
// started by another run executes inline in its parent's goroutine, and the
// parent decides whether its failure fails it.
//
// Runs of one user and lane execute in the order they were started: a run
// stays pending until every earlier run of the same user and lane has
// stopped. Runs of different users or lanes proceed in parallel.
//
// The work is a function held in memory, so nothing of it survives the
// process. When the process stops, pending work is dropped and running work
// is cancelled, and their rows stay as they were; Sweep, run at boot before
// any run starts, marks them interrupted. An interrupted run is neither
// resumed nor restarted, and is kept apart from failed because nothing
// recorded why it stopped.
//
// Work reaching its end marks the run completed unless the work did so
// itself. A kind whose writes and completion must be one database
// transaction calls CompleteRun inside that transaction, and the update made
// afterwards matches no row. Work returning an error marks the run failed
// with the error's text. A panic in Prepare or the work fails the run.
package run

//go:generate go tool mockgen -source=run.go -destination=mock/run_mock.go -package=mock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// ErrClosed is returned by Start and Child once Close has been called.
var ErrClosed = errors.New("runner closed")

// Store is the view of the run queries this package depends on.
type Store interface {
	CreateRun(ctx context.Context, arg gen.CreateRunParams) (gen.Run, error)
	StartRun(ctx context.Context, id uuid.UUID) (gen.Run, error)
	CompleteRun(ctx context.Context, id uuid.UUID) error
	FailRun(ctx context.Context, arg gen.FailRunParams) error
	InterruptRuns(ctx context.Context) (int64, error)
}

var _ Store = (*gen.Queries)(nil)

// Work is the body of a run. ctx is cancelled when the runner closes; work
// that returns an error after that is left for Sweep rather than failed.
type Work func(ctx context.Context, run gen.Run) error

// Spec describes a run a user starts.
type Spec struct {
	Kind   gen.RunKind
	UserID uuid.UUID
	// Lane indicates which of a user's runs must be serialized (ie. two
	// runs with the same lane must be serialized).
	Lane string
	// Prepare runs in the caller once the run has been created but
	// before the goroutine is started.
	Prepare Work
}

type laneKey struct {
	user uuid.UUID
	lane string
}

// Runner starts runs and executes them in the background.
type Runner struct {
	store Store
	log   *slog.Logger

	// ctx is the context every run's work receives; Close cancels it.
	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup

	mu sync.Mutex
	// lanes holds, per user and lane, the done channel of the last run
	// started in it.
	lanes map[laneKey]chan struct{}
}

// New returns a Runner over store.
func New(store Store, log *slog.Logger) *Runner {
	ctx, cancel := context.WithCancel(context.Background())
	return &Runner{store: store, log: log, ctx: ctx, cancel: cancel, lanes: map[laneKey]chan struct{}{}}
}

// handoff is what Start gives its goroutine once the row exists and Prepare
// has run. The channel carrying it is closed without one when the insert
// failed.
type handoff struct {
	row gen.Run
	err error
}

// Start records a run of trigger user, runs spec.Prepare, and queues the
// work. It returns the pending row without waiting for the work. A run whose
// row could not be inserted was never started and holds no place in its lane.
func (r *Runner) Start(ctx context.Context, spec Spec, work Work) (gen.Run, error) {
	// Only the id and the lane link are under the lock, so lane order is id
	// order.
	r.mu.Lock()
	if r.ctx.Err() != nil {
		r.mu.Unlock()
		return gen.Run{}, ErrClosed
	}
	id := db.NewID()
	key := laneKey{user: spec.UserID, lane: spec.Lane}
	prev := r.lanes[key]
	done := make(chan struct{})
	r.lanes[key] = done
	ready := make(chan handoff, 1)
	r.wg.Add(1)
	go r.background(work, key, prev, done, ready)
	r.mu.Unlock()
	row, err := r.store.CreateRun(ctx, gen.CreateRunParams{ID: id, UserID: spec.UserID, Kind: spec.Kind, Trigger: gen.RunTriggerUser})
	if err != nil {
		close(ready)
		return gen.Run{}, fmt.Errorf("create run: %w", err)
	}
	if spec.Prepare != nil {
		err = call(ctx, row, spec.Prepare)
	}
	ready <- handoff{row: row, err: err}
	return row, err
}

// Child records a run of trigger run under parent and executes its work
// inline. It returns the row as created and the error the work returned.
func (r *Runner) Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work Work) (gen.Run, error) {
	if r.ctx.Err() != nil {
		return gen.Run{}, ErrClosed
	}
	row, err := r.store.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: parent.UserID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID})
	if err != nil {
		return gen.Run{}, fmt.Errorf("create run: %w", err)
	}
	return row, r.execute(ctx, row, work)
}

// Close stops accepting runs, cancels running work and waits for every
// goroutine to stop. Pending runs are dropped.
func (r *Runner) Close() {
	r.mu.Lock()
	r.cancel()
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Runner) background(work Work, key laneKey, prev <-chan struct{}, done chan struct{}, ready <-chan handoff) {
	defer r.wg.Done()
	defer func() {
		close(done)
		r.mu.Lock()
		if r.lanes[key] == done {
			delete(r.lanes, key)
		}
		r.mu.Unlock()
	}()
	if prev != nil {
		select {
		case <-prev:
		case <-r.ctx.Done():
			return
		}
	}
	var (
		h  handoff
		ok bool
	)
	select {
	case h, ok = <-ready:
		if !ok {
			return
		}
	case <-r.ctx.Done():
		return
	}
	if h.err != nil {
		err := h.err
		work = func(context.Context, gen.Run) error { return err }
	}
	if r.ctx.Err() != nil {
		return
	}
	if err := r.execute(r.ctx, h.row, work); err != nil && r.ctx.Err() == nil {
		r.log.Warn("run failed", "run", h.row.ID, "kind", h.row.Kind, "err", err)
	}
}

// execute moves row through running to a terminal state and returns the
// error the work returned. The state writes outlive a cancelled ctx so work
// that ended is recorded; work that stopped because ctx was cancelled is not.
func (r *Runner) execute(ctx context.Context, row gen.Run, work Work) error {
	if _, err := r.store.StartRun(ctx, row.ID); err != nil {
		r.log.Error("start run", "run", row.ID, "kind", row.Kind, "err", err)
		return fmt.Errorf("start run: %w", err)
	}
	err := call(ctx, row, work)
	write := context.WithoutCancel(ctx)
	switch {
	case err == nil:
		if cerr := r.store.CompleteRun(write, row.ID); cerr != nil {
			r.log.Error("complete run", "run", row.ID, "kind", row.Kind, "err", cerr)
		}
	case ctx.Err() != nil:
	default:
		if ferr := r.store.FailRun(write, gen.FailRunParams{ID: row.ID, Error: err.Error()}); ferr != nil {
			r.log.Error("fail run", "run", row.ID, "kind", row.Kind, "err", ferr)
		}
	}
	return err
}

// call runs work and returns a panic in it as its error, so a run that
// panics fails rather than wedging its lane.
func call(ctx context.Context, row gen.Run, work Work) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return work(ctx, row)
}

// Sweep marks every pending and running run interrupted. It runs at boot,
// before any run starts, so every row it finds belongs to a process that
// stopped.
func Sweep(ctx context.Context, store Store, log *slog.Logger) error {
	n, err := store.InterruptRuns(ctx)
	if err != nil {
		return fmt.Errorf("interrupt runs: %w", err)
	}
	if n > 0 {
		log.Info("interrupted runs", "count", n)
	}
	return nil
}
