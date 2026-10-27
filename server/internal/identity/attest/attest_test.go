package attest

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
	"github.com/leedenison/stonks/server/internal/identity/validity"
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
)

// day is the nth day of March 2026.
func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

type fixture struct {
	store *MockStore
	runs  *MockRunner
	facts *MockFacts
	svc   *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl), facts: NewMockFacts(ctrl)}
	f.svc = New(f.store, f.runs, f.facts, mics, func() time.Time { return today })
	return f
}

// starts stands in for Runner.Start and runs Prepare and the work in the
// caller.
func (f *fixture) starts(t *testing.T, row gen.Run) {
	t.Helper()
	f.runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
		if spec.Kind != gen.RunKindAttestation || spec.Trigger != gen.RunTriggerAdministrator || spec.UserID != nil || spec.Lane != lane {
			t.Errorf("spec = %+v, want a system attestation run an administrator started", spec)
		}
		if err := spec.Prepare(ctx, row); err != nil {
			return row, err
		}
		return row, work(ctx, row)
	})
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(Queries) error) error {
		return fn(f.store)
	})
}

func assertion(row gen.Run, instrument *uuid.UUID, on time.Time) gen.CreateIdentifierAttestationParams {
	return gen.CreateIdentifierAttestationParams{
		RunID: row.ID, AttestedBy: admin, Type: types.IdentifierTypeMicTicker, Domain: "US", Value: "ACME",
		InstrumentID: instrument, AssertedOn: on,
	}
}

// TestStart checks that a range asserts at the market on both ends, and
// replays the market ticker.
func TestStart(t *testing.T) {
	f := newFixture(t)
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindAttestation, Trigger: gen.RunTriggerAdministrator}
	f.store.EXPECT().GetInstrument(gomock.Any(), acme).Return(gen.Instrument{ID: acme}, nil)
	f.starts(t, row)
	var written []gen.CreateIdentifierAttestationParams
	f.store.EXPECT().CreateIdentifierAttestation(gomock.Any(), gomock.Any()).Times(2).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierAttestationParams) error {
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
	want := []gen.CreateIdentifierAttestationParams{assertion(row, &acme, day(10)), assertion(row, &acme, day(20))}
	if diff := cmp.Diff(want, written); diff != "" {
		t.Errorf("assertions mismatch (-want +got):\n%s", diff)
	}
}

// TestStartNothing checks that attesting nothing on one day, given as a one
// day range, writes a single assertion that the ticker names nothing.
func TestStartNothing(t *testing.T) {
	f := newFixture(t)
	row := gen.Run{ID: db.NewID(), Kind: gen.RunKindAttestation}
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
		name       string
		v          types.MarketTicker
		instrument *uuid.UUID
		from, to   time.Time
		wantErr    error
	}{
		{name: "future", v: acmeUS, from: day(31), wantErr: validity.ErrFuture},
		{name: "future end", v: acmeUS, from: day(10), to: day(31), wantErr: validity.ErrFuture},
		{name: "ends before it starts", v: acmeUS, from: day(10), to: day(9), wantErr: ErrRange},
		{name: "venue for market", v: types.MarketTicker{Market: "XNAS", Value: "ACME"}, from: day(10), wantErr: validity.ErrSpelling},
		{name: "unknown market", v: types.MarketTicker{Market: "ZZ", Value: "ACME"}, from: day(10), wantErr: validity.ErrSpelling},
		{name: "unknown instrument", v: acmeUS, instrument: &missing, from: day(10), wantErr: db.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.instrument != nil {
				f.store.EXPECT().GetInstrument(gomock.Any(), *tc.instrument).Return(gen.Instrument{}, db.ErrNotFound)
			}
			var to *time.Time
			if !tc.to.IsZero() {
				to = &tc.to
			}
			_, err := f.svc.Start(context.Background(), admin, tc.v, tc.instrument, tc.from, to)
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
		row     gen.IdentifierAssertion
		err     error
		wantErr error
	}{
		{name: "a datasource's", row: gen.IdentifierAssertion{FetchKeyID: &key}, wantErr: ErrNotAttested},
		{name: "missing", err: db.ErrNotFound, wantErr: db.ErrNotFound},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			id := db.NewID()
			f.store.EXPECT().GetIdentifierAssertion(gomock.Any(), id).Return(tc.row, tc.err)
			if err := f.svc.Withdraw(context.Background(), id); !errors.Is(err, tc.wantErr) {
				t.Errorf("Withdraw() error = %v, want %v", err, tc.wantErr)
			}
		})
	}
}
