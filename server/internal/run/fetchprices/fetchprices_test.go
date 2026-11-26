package fetchprices

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/price"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	userID   = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	stmtRun  = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000010"), UserID: &userID, Kind: gen.RunKindIngestStmt}
	sysRun   = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000011"), Kind: gen.RunKindFetchPrices}
	alpha    = &market.Entry{Name: "alpha", Precedence: 1}
	beta     = &market.Entry{Name: "beta", Precedence: 2}
	gamma    = &market.Entry{Name: "gamma", Precedence: 3}
	listingA = uuid.MustParse("00000000-0000-0000-0000-0000000000a1")
	listingB = uuid.MustParse("00000000-0000-0000-0000-0000000000b1")
)

// gap is a gap of listing with one ticker and no period, which is all these
// tests compare.
func gap(listing uuid.UUID) price.Gap {
	return price.Gap{Listing: listing, Identifier: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: listing.String()[30:]}}
}

type fixture struct {
	demand   *MockDemand
	runs     *MockRunner
	recorder *MockRecorder
	sources  *MockSources
	svc      *Service
	starter  *Starter
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{demand: NewMockDemand(ctrl), runs: NewMockRunner(ctrl), recorder: NewMockRecorder(ctrl), sources: NewMockSources(ctrl)}
	log := slog.New(slog.DiscardHandler)
	f.svc = New(f.demand, f.runs, f.recorder, f.sources, log)
	f.starter = NewStarter(f.demand, f.runs, f.recorder, f.sources, log)
	return f
}

// reads scripts one read of the gaps of user, answering gaps.
func (f *fixture) reads(user *uuid.UUID, gaps ...price.Gap) []any {
	return []any{
		f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(nil),
		f.demand.EXPECT().Gaps(gomock.Any(), user).Return(gaps, nil, nil),
	}
}

// TestFill checks the order of asking: each enabled datasource in precedence
// order, each for the gaps read just before it, until no gap remains.
func TestFill(t *testing.T) {
	a, b := gap(listingA), gap(listingB)
	tests := []struct {
		name    string
		expect  func(f *fixture) []any
		wantErr bool
	}{
		{
			name: "the second datasource is asked for what the first left",
			expect: func(f *fixture) []any {
				var calls []any
				calls = append(calls, f.reads(nil, a, b)...)
				calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, alpha, []price.Gap{a, b}).Return(1, nil))
				calls = append(calls, f.reads(nil, b)...)
				calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, beta, []price.Gap{b}).Return(0, nil))
				calls = append(calls, f.reads(nil, b)...)
				calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, gamma, []price.Gap{b}).Return(0, nil))
				return calls
			},
		},
		{
			name: "the loop stops once no gap remains",
			expect: func(f *fixture) []any {
				var calls []any
				calls = append(calls, f.reads(nil, a)...)
				calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, alpha, []price.Gap{a}).Return(1, nil))
				calls = append(calls, f.reads(nil)...)
				return calls
			},
		},
		{
			name: "an error of the recorder ends the loop",
			expect: func(f *fixture) []any {
				var calls []any
				calls = append(calls, f.reads(nil, a)...)
				calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, alpha, []price.Gap{a}).Return(0, errors.New("boom")))
				return calls
			},
			wantErr: true,
		},
		{
			name: "an error of the demand ends the loop",
			expect: func(f *fixture) []any {
				return []any{f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(errors.New("boom"))}
			},
			wantErr: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.sources.EXPECT().Enabled().Return([]*market.Entry{alpha, beta, gamma})
			gomock.InOrder(tt.expect(f)...)
			err := f.svc.fill(context.Background(), sysRun, nil)
			if (err != nil) != tt.wantErr {
				t.Errorf("fill() error = %v, wantErr %t", err, tt.wantErr)
			}
		})
	}
}

// TestStart checks that Start refuses when nothing is a gap, and otherwise
// starts a system run in its lane that fills every gap.
func TestStart(t *testing.T) {
	t.Run("nothing is a gap", func(t *testing.T) {
		f := newFixture(t)
		gomock.InOrder(f.reads(nil)...)
		_, err := f.starter.Start(context.Background())
		if !errors.Is(err, ErrEmpty) || !errors.Is(err, run.ErrEmpty) {
			t.Errorf("Start() error = %v, want %v", err, ErrEmpty)
		}
	})
	t.Run("a gap", func(t *testing.T) {
		f := newFixture(t)
		a := gap(listingA)
		var calls []any
		calls = append(calls, f.reads(nil, a)...)
		spec := run.Spec{Kind: gen.RunKindFetchPrices, Trigger: gen.RunTriggerAdministrator, Lane: "fetch_prices"}
		calls = append(calls, f.runs.EXPECT().Start(gomock.Any(), spec, gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ run.Spec, work run.Work) (gen.Run, error) {
				return sysRun, work(ctx, sysRun)
			}))
		calls = append(calls, f.sources.EXPECT().Enabled().Return([]*market.Entry{alpha}))
		calls = append(calls, f.reads(nil, a)...)
		calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, alpha, []price.Gap{a}).Return(1, nil))
		gomock.InOrder(calls...)
		got, err := f.starter.Start(context.Background())
		if err != nil || got.ID != sysRun.ID {
			t.Errorf("Start() = %v, %v, want run %v", got.ID, err, sysRun.ID)
		}
	})
}

// TestAfterStmt checks that a statement starts one system child for the gaps
// of its user, starts nothing where the user lacks a gap, and never returns
// a failure to the statement.
func TestAfterStmt(t *testing.T) {
	a := gap(listingA)
	child := func(f *fixture, err error) any {
		return f.runs.EXPECT().ChildAs(gomock.Any(), stmtRun, nil, gen.RunKindFetchPrices, gomock.Any()).
			DoAndReturn(func(ctx context.Context, _ gen.Run, _ *uuid.UUID, _ gen.RunKind, work run.Work) (gen.Run, error) {
				if err != nil {
					return gen.Run{}, err
				}
				return sysRun, work(ctx, sysRun)
			})
	}
	tests := []struct {
		name   string
		expect func(f *fixture) []any
	}{
		{
			name:   "nothing is a gap",
			expect: func(f *fixture) []any { return f.reads(&userID) },
		},
		{
			name: "a gap",
			expect: func(f *fixture) []any {
				var calls []any
				calls = append(calls, f.reads(&userID, a)...)
				calls = append(calls, child(f, nil))
				calls = append(calls, f.sources.EXPECT().Enabled().Return([]*market.Entry{alpha}))
				calls = append(calls, f.reads(&userID, a)...)
				calls = append(calls, f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, alpha, []price.Gap{a}).Return(1, nil))
				return calls
			},
		},
		{
			name: "the demand fails",
			expect: func(f *fixture) []any {
				return []any{f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(errors.New("boom"))}
			},
		},
		{
			name: "the child fails",
			expect: func(f *fixture) []any {
				return append(f.reads(&userID, a), child(f, errors.New("boom")))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			gomock.InOrder(tt.expect(f)...)
			f.svc.AfterStmt(context.Background(), stmtRun)
		})
	}
}
