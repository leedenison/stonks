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
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

var (
	admin = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	acme  = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	mics  = mic.Of(map[string]string{"XNAS": "XNAS", "XNGS": "XNAS"})
	today = time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	// nasdaq is the ticker at its operating MIC, the venue every assertion
	// names.
	nasdaq = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "ACME"}
)

// day is the nth day of March 2026.
func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

type fixture struct {
	store   *MockStore
	runs    *MockRunner
	replays *MockReplays
	svc     *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl), replays: NewMockReplays(ctrl)}
	f.svc = New(f.store, f.runs, f.replays, mics, func() time.Time { return today })
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
		RunID: row.ID, AttestedBy: admin, Type: nasdaq.Type, Domain: nasdaq.Domain, Value: nasdaq.Value,
		InstrumentID: instrument, AssertedOn: on,
	}
}

// TestStart checks that a range attested at a venue asserts at the operating
// MIC on both ends.
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
	f.replays.EXPECT().ReplayNewIdentifierFacts(gomock.Any(), row, []resolve.WrittenIdentifierFacts{{Run: row, Identifiers: []types.Identifier{nasdaq}}})

	segment := types.Identifier{Type: nasdaq.Type, Domain: "XNGS", Value: "ACME"}
	to := day(20)
	got, err := f.svc.Start(context.Background(), admin, segment, &acme, day(10), &to)
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
	f.replays.EXPECT().ReplayNewIdentifierFacts(gomock.Any(), row, gomock.Any())

	to := day(10)
	if _, err := f.svc.Start(context.Background(), admin, nasdaq, nil, day(10), &to); err != nil {
		t.Fatalf("Start() error = %v", err)
	}
}

// TestStartRefused checks what is refused before a run is created.
func TestStartRefused(t *testing.T) {
	missing := uuid.MustParse("00000000-0000-0000-0000-000000000009")
	tests := []struct {
		name       string
		v          types.Identifier
		instrument *uuid.UUID
		from, to   time.Time
		wantErr    error
	}{
		{name: "future", v: nasdaq, from: day(31), wantErr: validity.ErrFuture},
		{name: "future end", v: nasdaq, from: day(10), to: day(31), wantErr: validity.ErrFuture},
		{name: "ends before it starts", v: nasdaq, from: day(10), to: day(9), wantErr: ErrRange},
		{name: "domain", v: types.Identifier{Type: nasdaq.Type, Domain: "XNAS"}, from: day(10), wantErr: validity.ErrSpelling},
		{name: "unknown MIC", v: types.Identifier{Type: nasdaq.Type, Domain: "ZZZZ", Value: "ACME"}, from: day(10), wantErr: validity.ErrSpelling},
		{name: "isin", v: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}, from: day(10), wantErr: validity.ErrNotTicker},
		{name: "unknown instrument", v: nasdaq, instrument: &missing, from: day(10), wantErr: db.ErrNotFound},
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
