// Package run starts background work and records each piece of it as a run.
//
// A run is a row created before its work starts. The call starting it answers
// with the row, and progress and the outcome are read against it. The states
// and the columns each sets are on the runs table in
// [002_runs.sql](../migrations/002_runs.sql). Each kind of work owns the
// shape of its per-item rows, and the mix of outcomes for one run is a query
// over them.
//
// A run is started by a user or by another run. A user's run executes in a
// goroutine of its own once the call starting it has answered; receipt never
// waits. A run started by another run executes inline in its parent's
// goroutine and takes its parent's place in the order. A parent's state is
// its own: the parent decides whether a child's failure fails it.
//
// Runs of one user and lane execute in the order they were started: a run
// stays pending until every earlier run of the same user and lane has
// stopped. The caller names the lane. An upload's lane is its broker, because
// an upload replaces every transaction of its broker in a period and the
// later upload is the one to keep. Runs of different users or lanes proceed
// in parallel. The order is held in memory, which assumes one process.
//
// The work is a function held in memory, so nothing of it survives the
// process. When the process stops, pending work is dropped and running work
// is cancelled, and their rows stay as they were; Sweep, run at boot before
// any run starts, marks them interrupted. An interrupted run is neither
// resumed nor restarted. It is kept apart from failed because nothing
// recorded why it stopped.
//
// Work reaching its end marks the run completed unless the work did so
// itself. A kind whose writes and completion must be one database
// transaction calls CompleteRun inside that transaction, and the update made
// afterwards matches no row. Work returning an error marks the run failed
// with the error's text. A panic in the work fails the run, not the process.
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
type Work func(ctx context.Context) error

// Spec describes a run a user starts.
type Spec struct {
	Kind   gen.RunKind
	UserID uuid.UUID
	// Lane orders the run among the user's runs sharing it, such as a broker.
	Lane string
}

type laneKey struct {
	user uuid.UUID
	lane string
}

// Runner starts runs and executes them in the background.
type Runner struct {
	store Store
	log   *slog.Logger

	closing chan struct{}
	wg      sync.WaitGroup

	mu     sync.Mutex
	closed bool
	// lanes holds, per lane, the channel closed when its latest run stops.
	lanes map[laneKey]chan struct{}
}

// New returns a Runner over store.
func New(store Store, log *slog.Logger) *Runner {
	return &Runner{store: store, log: log, closing: make(chan struct{}), lanes: map[laneKey]chan struct{}{}}
}

// Start records a run of trigger user and queues its work. It returns the
// pending row without waiting for anything.
func (r *Runner) Start(ctx context.Context, spec Spec, work Work) (gen.Run, error) {
	// Held across the insert and the enqueue so lane order is creation order.
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return gen.Run{}, ErrClosed
	}
	row, err := r.store.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: spec.UserID, Kind: spec.Kind, Trigger: gen.RunTriggerUser})
	if err != nil {
		return gen.Run{}, fmt.Errorf("create run: %w", err)
	}
	key := laneKey{user: spec.UserID, lane: spec.Lane}
	prev := r.lanes[key]
	done := make(chan struct{})
	r.lanes[key] = done
	r.wg.Add(1)
	go r.background(row, work, key, prev, done)
	return row, nil
}

// Child records a run of trigger run under parent and executes its work
// inline. It returns the row as created and the error the work returned.
func (r *Runner) Child(ctx context.Context, parent gen.Run, kind gen.RunKind, work Work) (gen.Run, error) {
	r.mu.Lock()
	closed := r.closed
	r.mu.Unlock()
	if closed {
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
	if !r.closed {
		r.closed = true
		close(r.closing)
	}
	r.mu.Unlock()
	r.wg.Wait()
}

func (r *Runner) background(row gen.Run, work Work, key laneKey, prev <-chan struct{}, done chan struct{}) {
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
		case <-r.closing:
			return
		}
	}
	select {
	case <-r.closing:
		return
	default:
	}
	ctx, cancel := r.context()
	defer cancel()
	if err := r.execute(ctx, row, work); err != nil && ctx.Err() == nil {
		r.log.Warn("run failed", "run", row.ID, "kind", row.Kind, "err", err)
	}
}

// context returns a context cancelled when the runner closes.
func (r *Runner) context() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	r.wg.Add(1)
	go func() {
		defer r.wg.Done()
		select {
		case <-r.closing:
			cancel()
		case <-ctx.Done():
		}
	}()
	return ctx, cancel
}

// execute moves row through running to a terminal state and returns the
// error the work returned. The state writes outlive a cancelled ctx so work
// that ended is recorded; work that stopped because ctx was cancelled is not.
func (r *Runner) execute(ctx context.Context, row gen.Run, work Work) error {
	if _, err := r.store.StartRun(ctx, row.ID); err != nil {
		r.log.Error("start run", "run", row.ID, "kind", row.Kind, "err", err)
		return fmt.Errorf("start run: %w", err)
	}
	err := call(ctx, work)
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

func call(ctx context.Context, work Work) (err error) {
	defer func() {
		if p := recover(); p != nil {
			err = fmt.Errorf("panic: %v", p)
		}
	}()
	return work(ctx)
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
