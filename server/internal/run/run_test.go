package run

import (
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/run/mock"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

var (
	userA = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	userB = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	// wait bounds a step that must happen for the test to mean anything.
	wait = 5 * time.Second
)

type fixture struct {
	store  *mock.MockStore
	runner *Runner
}

// newFixture returns a runner whose store records creations and starts as a
// database would. Completion and failure are left to each test.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{store: mock.NewMockStore(ctrl)}
	f.store.EXPECT().CreateRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateRunParams) (gen.Run, error) {
		return gen.Run{ID: arg.ID, UserID: arg.UserID, Kind: arg.Kind, Trigger: arg.Trigger, ParentID: arg.ParentID, State: gen.RunStatePending}, nil
	}).AnyTimes()
	f.store.EXPECT().StartRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) (gen.Run, error) {
		return gen.Run{ID: id, State: gen.RunStateRunning}, nil
	}).AnyTimes()
	f.runner = New(f.store, slog.New(slog.DiscardHandler))
	t.Cleanup(f.runner.Close)
	return f
}

// signal returns a channel closed when the store call it stands in for is
// made, so a test can wait for the write that ends a run.
func signal() (chan struct{}, func()) {
	ch := make(chan struct{})
	var once sync.Once
	return ch, func() { once.Do(func() { close(ch) }) }
}

func await(t *testing.T, ch <-chan struct{}, what string) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(wait):
		t.Fatalf("timed out waiting for %s", what)
	}
}

func TestStart(t *testing.T) {
	f := newFixture(t)
	completed, done := signal()
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error { done(); return nil })

	ran := make(chan struct{})
	row, err := f.runner.Start(context.Background(), Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr"}, func(context.Context, gen.Run) error {
		close(ran)
		return nil
	})
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	want := gen.Run{ID: row.ID, UserID: userA, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser, State: gen.RunStatePending}
	if diff := cmp.Diff(want, row); diff != "" {
		t.Errorf("Start() row mismatch (-want +got):\n%s", diff)
	}
	await(t, ran, "the work to run")
	await(t, completed, "CompleteRun")
}

func TestStartRecordsFailure(t *testing.T) {
	tests := []struct {
		name string
		work Work
		want string
	}{
		{name: "error", work: func(context.Context, gen.Run) error { return errors.New("boom") }, want: "boom"},
		{name: "panic", work: func(context.Context, gen.Run) error { panic("boom") }, want: "panic: boom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			failed, done := signal()
			var got gen.FailRunParams
			f.store.EXPECT().FailRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.FailRunParams) error {
				got = arg
				done()
				return nil
			})
			row, err := f.runner.Start(context.Background(), Spec{Kind: gen.RunKindStatement, UserID: userA}, tc.work)
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			await(t, failed, "FailRun")
			if got.ID != row.ID || got.Error != tc.want {
				t.Errorf("FailRun(%s, %q), want (%s, %q)", got.ID, got.Error, row.ID, tc.want)
			}
		})
	}
}

// TestStartCreateFails checks that a run whose row cannot be inserted is
// reported as never started and does not hold its lane.
func TestStartCreateFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := mock.NewMockStore(ctrl)
	boom := errors.New("boom")
	store.EXPECT().CreateRun(gomock.Any(), gomock.Any()).Return(gen.Run{}, boom)
	store.EXPECT().CreateRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateRunParams) (gen.Run, error) {
		return gen.Run{ID: arg.ID, UserID: arg.UserID, Kind: arg.Kind, Trigger: arg.Trigger, State: gen.RunStatePending}, nil
	})
	store.EXPECT().StartRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) (gen.Run, error) {
		return gen.Run{ID: id, State: gen.RunStateRunning}, nil
	})
	completed, done := signal()
	store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error { done(); return nil })
	r := New(store, slog.New(slog.DiscardHandler))
	t.Cleanup(r.Close)

	spec := Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr", Prepare: func(context.Context, gen.Run) error {
		t.Error("Prepare ran for a run that was not inserted")
		return nil
	}}
	_, err := r.Start(context.Background(), spec, func(context.Context, gen.Run) error {
		t.Error("work ran for a run that was not inserted")
		return nil
	})
	if !errors.Is(err, boom) {
		t.Errorf("Start() error = %v, want %v", err, boom)
	}
	next := Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr"}
	if _, err := r.Start(context.Background(), next, func(context.Context, gen.Run) error { return nil }); err != nil {
		t.Fatalf("Start() after the failed insert error = %v", err)
	}
	await(t, completed, "CompleteRun of the next run in the lane")
}

// TestStartOrdersLane checks that a run of one user and lane does not start
// until the earlier one has stopped, while other lanes are unaffected.
func TestStartOrdersLane(t *testing.T) {
	f := newFixture(t)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()

	var mu sync.Mutex
	var order []string
	record := func(s string) {
		mu.Lock()
		defer mu.Unlock()
		order = append(order, s)
	}
	release := make(chan struct{})
	firstStarted := make(chan struct{})
	otherStarted := make(chan struct{})
	secondDone := make(chan struct{})
	first := func(context.Context, gen.Run) error {
		record("first start")
		close(firstStarted)
		<-release
		record("first end")
		return nil
	}
	second := func(context.Context, gen.Run) error {
		record("second")
		close(secondDone)
		return nil
	}
	other := func(context.Context, gen.Run) error {
		close(otherStarted)
		return nil
	}
	ctx := context.Background()
	for _, s := range []struct {
		spec Spec
		work Work
	}{
		{Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr"}, first},
		{Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr"}, second},
		{Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "schwab"}, other},
	} {
		if _, err := f.runner.Start(ctx, s.spec, s.work); err != nil {
			t.Fatalf("Start(%+v) error = %v", s.spec, err)
		}
	}
	await(t, firstStarted, "the first run to start")
	await(t, otherStarted, "the other lane to start while the first run blocks")
	close(release)
	await(t, secondDone, "the second run to follow the first")
	f.runner.Close()
	want := []string{"first start", "first end", "second"}
	if diff := cmp.Diff(want, order); diff != "" {
		t.Errorf("lane order mismatch (-want +got):\n%s", diff)
	}
}

// TestParallelUsers checks that runs of two users sharing a lane name run at
// once: each blocks until both have started.
func TestParallelUsers(t *testing.T) {
	f := newFixture(t)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil).Times(2)
	var wg sync.WaitGroup
	wg.Add(2)
	both := make(chan struct{})
	go func() { wg.Wait(); close(both) }()
	work := func(ctx context.Context, _ gen.Run) error {
		wg.Done()
		select {
		case <-both:
			return nil
		case <-ctx.Done():
			return ctx.Err()
		}
	}
	for _, u := range []uuid.UUID{userA, userB} {
		if _, err := f.runner.Start(context.Background(), Spec{Kind: gen.RunKindStatement, UserID: u, Lane: "ibkr"}, work); err != nil {
			t.Fatalf("Start() error = %v", err)
		}
	}
	await(t, both, "both users' runs to start")
	f.runner.Close()
}

// TestClose checks that closing cancels running work, drops pending work,
// writes no terminal state for either, and refuses runs afterwards.
func TestClose(t *testing.T) {
	f := newFixture(t)
	started := make(chan struct{})
	blocking := func(ctx context.Context, _ gen.Run) error {
		close(started)
		<-ctx.Done()
		return ctx.Err()
	}
	pending := func(context.Context, gen.Run) error {
		t.Error("pending work ran after Close")
		return nil
	}
	ctx := context.Background()
	spec := Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr"}
	if _, err := f.runner.Start(ctx, spec, blocking); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	await(t, started, "the run to start")
	if _, err := f.runner.Start(ctx, spec, pending); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	f.runner.Close()
	if _, err := f.runner.Start(ctx, spec, pending); !errors.Is(err, ErrClosed) {
		t.Errorf("Start() after Close: err = %v, want ErrClosed", err)
	}
	if _, err := f.runner.Child(ctx, gen.Run{UserID: userA}, gen.RunKindResolution, pending); !errors.Is(err, ErrClosed) {
		t.Errorf("Child() after Close: err = %v, want ErrClosed", err)
	}
}

func TestChild(t *testing.T) {
	parent := gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000010"), UserID: userA, Kind: gen.RunKindStatement}
	tests := []struct {
		name    string
		work    Work
		expect  func(f *fixture)
		wantErr string
	}{
		{name: "completed", work: func(context.Context, gen.Run) error { return nil }, expect: func(f *fixture) {
			f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
		}},
		{name: "failed", work: func(context.Context, gen.Run) error { return errors.New("boom") }, expect: func(f *fixture) {
			f.store.EXPECT().FailRun(gomock.Any(), gomock.Any()).Return(nil)
		}, wantErr: "boom"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.expect(f)
			ran := false
			row, err := f.runner.Child(context.Background(), parent, gen.RunKindResolution, func(ctx context.Context, run gen.Run) error {
				ran = true
				return tc.work(ctx, run)
			})
			if !ran {
				t.Fatal("Child() returned before its work ran")
			}
			if (err == nil) != (tc.wantErr == "") || (err != nil && err.Error() != tc.wantErr) {
				t.Errorf("Child() error = %v, want %q", err, tc.wantErr)
			}
			want := gen.Run{ID: row.ID, UserID: userA, Kind: gen.RunKindResolution, Trigger: gen.RunTriggerRun, ParentID: &parent.ID, State: gen.RunStatePending}
			if diff := cmp.Diff(want, row); diff != "" {
				t.Errorf("Child() row mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestPrepare checks that the work waits for Prepare, and that an error from
// Prepare fails the run and is returned by Start.
func TestPrepare(t *testing.T) {
	t.Run("work waits", func(t *testing.T) {
		f := newFixture(t)
		completed, done := signal()
		f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error { done(); return nil })
		prepared := false
		var preparedFor gen.Run
		spec := Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr", Prepare: func(_ context.Context, run gen.Run) error {
			time.Sleep(20 * time.Millisecond)
			prepared = true
			preparedFor = run
			return nil
		}}
		row, err := f.runner.Start(context.Background(), spec, func(_ context.Context, run gen.Run) error {
			if !prepared {
				t.Error("work ran before Prepare returned")
			}
			if run.ID != preparedFor.ID {
				t.Errorf("work ran as %s, Prepare ran for %s", run.ID, preparedFor.ID)
			}
			return nil
		})
		if err != nil || !prepared || row.ID != preparedFor.ID {
			t.Fatalf("Start() = %+v, %v after Prepare for %+v", row, err, preparedFor)
		}
		await(t, completed, "CompleteRun")
	})
	t.Run("error fails the run", func(t *testing.T) {
		f := newFixture(t)
		failed, done := signal()
		var got gen.FailRunParams
		f.store.EXPECT().FailRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.FailRunParams) error {
			got = arg
			done()
			return nil
		})
		boom := errors.New("boom")
		spec := Spec{Kind: gen.RunKindStatement, UserID: userA, Prepare: func(context.Context, gen.Run) error { return boom }}
		row, err := f.runner.Start(context.Background(), spec, func(context.Context, gen.Run) error {
			t.Error("work ran after Prepare failed")
			return nil
		})
		if !errors.Is(err, boom) {
			t.Errorf("Start() error = %v, want %v", err, boom)
		}
		await(t, failed, "FailRun")
		if got.ID != row.ID || got.Error != "boom" {
			t.Errorf("FailRun(%s, %q), want (%s, %q)", got.ID, got.Error, row.ID, "boom")
		}
	})
	t.Run("panic fails the run and frees the lane", func(t *testing.T) {
		f := newFixture(t)
		failed, failedDone := signal()
		f.store.EXPECT().FailRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, gen.FailRunParams) error { failedDone(); return nil })
		completed, completedDone := signal()
		f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error { completedDone(); return nil })
		spec := Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr", Prepare: func(context.Context, gen.Run) error { panic("boom") }}
		_, err := f.runner.Start(context.Background(), spec, func(context.Context, gen.Run) error {
			t.Error("work ran after Prepare panicked")
			return nil
		})
		if err == nil || !strings.Contains(err.Error(), "panic: boom") {
			t.Errorf("Start() error = %v, want the panic", err)
		}
		await(t, failed, "FailRun")
		next := Spec{Kind: gen.RunKindStatement, UserID: userA, Lane: "ibkr"}
		if _, err := f.runner.Start(context.Background(), next, func(context.Context, gen.Run) error { return nil }); err != nil {
			t.Fatalf("Start() after the panic error = %v", err)
		}
		await(t, completed, "CompleteRun of the next run in the lane")
	})
}

func TestSweep(t *testing.T) {
	tests := []struct {
		name    string
		n       int64
		err     error
		wantErr bool
	}{
		{name: "none", n: 0},
		{name: "some", n: 2},
		{name: "failure", err: errors.New("boom"), wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			store := mock.NewMockStore(ctrl)
			store.EXPECT().InterruptRuns(gomock.Any()).Return(tc.n, tc.err)
			err := Sweep(context.Background(), store, slog.New(slog.DiscardHandler))
			if (err != nil) != tc.wantErr {
				t.Errorf("Sweep() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}
