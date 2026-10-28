package attestidentifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	admin = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	acme  = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	// mics puts XNAS and XNYS in the market US.
	mics = mic.MICMap{
		"XNAS": {Operating: "XNAS", Market: "US"},
		"XNGS": {Operating: "XNAS", Market: "US"},
		"XNYS": {Operating: "XNYS", Market: "US"},
	}
	today  = time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	acmeUS = types.MarketTicker{Market: "US", Value: "ACME"}
	// acmeRow is the row of acmeUS.
	acmeRow = gen.Ticker{ID: uuid.MustParse("00000000-0000-0000-0000-000000000004"), Market: "US", Value: "ACME"}
)

// day is the nth day of March 2026.
func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

type fixture struct {
	store *MockStore
	runs  *MockRunner
	facts *MockFacts
	names *MockRebuilder
	svc   *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl), facts: NewMockFacts(ctrl), names: NewMockRebuilder(ctrl)}
	f.svc = New(f.store, f.runs, f.facts, f.names, mics, func() time.Time { return today })
	return f
}

// starts stands in for Runner.Start and runs Prepare and the work in the
// caller. Prepare locks the ticker's row and rebuilds its names once.
func (f *fixture) starts(t *testing.T, row gen.Run) {
	t.Helper()
	f.runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
		if spec.Kind != gen.RunKindAttestIdentifier || spec.Trigger != gen.RunTriggerAdministrator || spec.UserID != nil || spec.Lane != lane {
			t.Errorf("spec = %+v, want a system attest identifier run an administrator started", spec)
		}
		if err := spec.Prepare(ctx, row); err != nil {
			return row, err
		}
		return row, work(ctx, row)
	})
	f.store.EXPECT().CreateTickers(gomock.Any(), gomock.Any())
	f.store.EXPECT().ListTickers(gomock.Any(), gen.ListTickersParams{Markets: []string{"US"}, Values: []string{"ACME"}}).Return([]gen.Ticker{acmeRow}, nil)
	f.tx()
	f.locks()
}

// tx runs one transaction in the caller.
func (f *fixture) tx() {
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(Queries) error) error {
		return fn(f.store)
	})
}

// locks expects the transaction to lock the ticker's row and rebuild its
// names once.
func (f *fixture) locks() {
	ids := []uuid.UUID{acmeRow.ID}
	f.store.EXPECT().LockTickers(gomock.Any(), ids).Return(ids, nil)
	f.names.EXPECT().Rebuild(gomock.Any(), f.store, ids)
}

func assertion(row gen.Run, instr *uuid.UUID, on time.Time) gen.CreateIdentifierAttestationParams {
	return gen.CreateIdentifierAttestationParams{
		RunID: row.ID, AttestedBy: admin, TickerID: acmeRow.ID, Market: "US",
		InstrID: instr, AssertedFrom: on, AssertedBefore: on.AddDate(0, 0, 1),
	}
}

// TestStart checks that a range asserts every day of it at the market in one
// assertion, and replays the market ticker.
func TestStart(t *testing.T) {
	f := newFixture(t)
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator}
	f.store.EXPECT().GetInstr(gomock.Any(), acme).Return(gen.Instr{ID: acme}, nil)
	f.starts(t, row)
	var written []gen.CreateIdentifierAttestationParams
	f.store.EXPECT().CreateIdentifierAttestation(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierAttestationParams) error {
		arg.ID = uuid.Nil
		written = append(written, arg)
		return nil
	})
	f.facts.EXPECT().AfterIdentifierFacts(gomock.Any(), row, []resolve.WrittenIdentifierFacts{{Run: row, Tickers: []types.MarketTicker{acmeUS}}})

	to := day(20)
	got, err := f.svc.Start(context.Background(), admin, acmeUS, &acme, day(10), &to)
	if err != nil || got.ID != row.ID {
		t.Fatalf("Start() = %+v, %v, want the run %s", got, err, row.ID)
	}
	held := assertion(row, &acme, day(10))
	held.AssertedBefore = day(21)
	want := []gen.CreateIdentifierAttestationParams{held}
	if diff := cmp.Diff(want, written); diff != "" {
		t.Errorf("assertions mismatch (-want +got):\n%s", diff)
	}
}

// TestStartNothing checks that attesting nothing on one day, given as a one
// day range, writes a single assertion that the ticker names nothing.
func TestStartNothing(t *testing.T) {
	f := newFixture(t)
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier}
	f.starts(t, row)
	f.store.EXPECT().CreateIdentifierAttestation(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierAttestationParams) error {
		arg.ID = uuid.Nil
		if diff := cmp.Diff(assertion(row, nil, day(10)), arg); diff != "" {
			t.Errorf("assertion mismatch (-want +got):\n%s", diff)
		}
		return nil
	})
	f.facts.EXPECT().AfterIdentifierFacts(gomock.Any(), row, gomock.Any())

	to := day(10)
	if _, err := f.svc.Start(context.Background(), admin, acmeUS, nil, day(10), &to); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

// TestStartRefused checks what is refused before a run is created.
func TestStartRefused(t *testing.T) {
	missing := uuid.MustParse("00000000-0000-0000-0000-000000000009")
	tests := []struct {
		name     string
		v        types.MarketTicker
		instr    *uuid.UUID
		from, to time.Time
		wantErr  error
	}{
		{name: "future", v: acmeUS, from: day(31), wantErr: tickervalidity.ErrFuture},
		{name: "future end", v: acmeUS, from: day(10), to: day(31), wantErr: tickervalidity.ErrFuture},
		{name: "ends before it starts", v: acmeUS, from: day(10), to: day(9), wantErr: ErrRange},
		{name: "venue for market", v: types.MarketTicker{Market: "XNAS", Value: "ACME"}, from: day(10), wantErr: tickers.ErrSpelling},
		{name: "unknown market", v: types.MarketTicker{Market: "ZZ", Value: "ACME"}, from: day(10), wantErr: tickers.ErrSpelling},
		{name: "unknown instrument", v: acmeUS, instr: &missing, from: day(10), wantErr: db.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.instr != nil {
				f.store.EXPECT().GetInstr(gomock.Any(), *tc.instr).Return(gen.Instr{}, db.ErrNotFound)
			}
			var to *time.Time
			if !tc.to.IsZero() {
				to = &tc.to
			}
			_, err := f.svc.Start(context.Background(), admin, tc.v, tc.instr, tc.from, to)
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Start(%s) error = %v, want %v", tc.v.Name(), err, tc.wantErr)
			}
		})
	}
}

// TestWithdrawRefused checks what a withdrawal refuses.
func TestWithdrawRefused(t *testing.T) {
	key := db.NewID()
	tests := []struct {
		name    string
		row     gen.GetIdentifierAssertionRow
		err     error
		wantErr error
	}{
		{name: "a datasource's", row: gen.GetIdentifierAssertionRow{IdentifierAssertion: gen.IdentifierAssertion{FetchKeyID: &key}}, wantErr: ErrNotAttested},
		{name: "missing", err: db.ErrNotFound, wantErr: db.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			id := db.NewID()
			f.tx()
			f.store.EXPECT().GetIdentifierAssertion(gomock.Any(), id).Return(tc.row, tc.err)
			if err := f.svc.Withdraw(context.Background(), id); !errors.Is(err, tc.wantErr) {
				t.Errorf("Withdraw() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}

// TestWithdraw checks that a withdrawal deletes the assertion, rebuilds the
// names of its ticker, and replays its ticker at the market, beneath the run
// that wrote it.
func TestWithdraw(t *testing.T) {
	f := newFixture(t)
	id := db.NewID()
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier}
	a := gen.GetIdentifierAssertionRow{
		IdentifierAssertion: gen.IdentifierAssertion{ID: id, RunID: &row.ID, TickerID: acmeRow.ID, Market: "US"},
		Value:               "ACME",
	}
	f.tx()
	f.store.EXPECT().GetIdentifierAssertion(gomock.Any(), id).Return(a, nil)
	f.store.EXPECT().GetRunByID(gomock.Any(), row.ID).Return(row, nil)
	f.locks()
	f.store.EXPECT().DeleteAttestedAssertion(gomock.Any(), id).Return(int64(1), nil)
	f.facts.EXPECT().AfterIdentifierFacts(gomock.Any(), row, []resolve.WrittenIdentifierFacts{{Run: row, Tickers: []types.MarketTicker{acmeUS}}})
	if err := f.svc.Withdraw(context.Background(), id); err != nil {
		t.Fatalf("Withdraw() error = %v", err)
	}
}
