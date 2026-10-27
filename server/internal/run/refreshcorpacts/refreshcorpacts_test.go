package refreshcorpacts

import (
	"context"
	"errors"
	"log/slog"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	userID  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	otherID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	today   = time.Date(2026, time.March, 2, 0, 0, 0, 0, time.UTC)
	userRun = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000010"), UserID: &userID, Kind: gen.RunKindIngestStmt}
	sysRun  = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000011"), Kind: gen.RunKindReassertTickers}
	sysKid  = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000012"), Kind: gen.RunKindRefreshCorpActs}
)

type fixture struct {
	queries  *MockQueries
	runs     *MockRunner
	recorder *MockRecorder
	svc      *Service
	starter  *Starter
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{queries: NewMockQueries(ctrl), runs: NewMockRunner(ctrl), recorder: NewMockRecorder(ctrl)}
	clock := func() time.Time { return today }
	log := slog.New(slog.DiscardHandler)
	mics := mic.Of(map[string]string{"XNAS": "XNAS"})
	f.svc = New(f.queries, f.runs, f.recorder, mics, clock, log)
	f.starter = NewStarter(f.queries, f.runs, f.recorder, mics, clock, log)
	return f
}

// req is a request for value at XNAS over the period from from to the day
// after today.
func req(value string, from time.Time) market.CorpActRequest {
	return market.CorpActRequest{
		Ticker: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: value},
		Period: types.Period{From: from, Before: today.AddDate(0, 0, 1)},
	}
}

// system expects one system child of parent, which runs its work as sysKid,
// and returns err.
func (f *fixture) system(parent gen.Run, err error) {
	f.runs.EXPECT().ChildAs(gomock.Any(), parent, nil, gen.RunKindRefreshCorpActs, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _ gen.Run, _ *uuid.UUID, _ gen.RunKind, work run.Work) (gen.Run, error) {
			if err != nil {
				return gen.Run{}, err
			}
			return sysKid, work(ctx, sysKid)
		})
}

// TestBeneath checks where each ticker is fetched: beneath a user's run when
// only that user holds its instruments, beneath one system child when
// another user holds one, and beneath a system run in every case.
func TestBeneath(t *testing.T) {
	from := time.Date(2020, time.January, 2, 0, 0, 0, 0, time.UTC)
	own := ask{req: req("AAPL", from), users: []uuid.UUID{userID}}
	shared := ask{req: req("MSFT", from), users: []uuid.UUID{userID, otherID}}
	theirs := ask{req: req("NVDA", from), users: []uuid.UUID{otherID}}
	tests := []struct {
		name   string
		parent gen.Run
		expect func(f *fixture)
	}{
		{
			name:   "a user's run",
			parent: userRun,
			expect: func(f *fixture) {
				f.recorder.EXPECT().Fetch(gomock.Any(), userRun, []market.CorpActRequest{own.req}).Return(nil)
				f.system(userRun, nil)
				f.recorder.EXPECT().Fetch(gomock.Any(), sysKid, []market.CorpActRequest{shared.req, theirs.req}).Return(nil)
			},
		},
		{
			name:   "a system run",
			parent: sysRun,
			expect: func(f *fixture) {
				f.recorder.EXPECT().Fetch(gomock.Any(), sysRun, []market.CorpActRequest{own.req, shared.req, theirs.req}).Return(nil)
			},
		},
		{
			name:   "the user's fetch fails",
			parent: userRun,
			expect: func(f *fixture) {
				f.recorder.EXPECT().Fetch(gomock.Any(), userRun, gomock.Any()).Return(errors.New("boom"))
				f.system(userRun, nil)
				f.recorder.EXPECT().Fetch(gomock.Any(), sysKid, gomock.Any()).Return(nil)
			},
		},
		{
			name:   "the system child fails",
			parent: userRun,
			expect: func(f *fixture) {
				f.recorder.EXPECT().Fetch(gomock.Any(), userRun, []market.CorpActRequest{own.req}).Return(nil)
				f.system(userRun, errors.New("boom"))
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			tt.expect(f)
			f.svc.beneath(context.Background(), tt.parent, []ask{own, shared, theirs})
		})
	}
}

// TestBeneathOwnOnly checks that a user's run fetches every ticker beneath
// itself when its user alone holds each one.
func TestBeneathOwnOnly(t *testing.T) {
	f := newFixture(t)
	a := ask{req: req("AAPL", today), users: []uuid.UUID{userID}}
	f.recorder.EXPECT().Fetch(gomock.Any(), userRun, []market.CorpActRequest{a.req}).Return(nil)
	f.svc.beneath(context.Background(), userRun, []ask{a})
}

// TestFetchAfterNoTicker checks that an empty list of facts fetches nothing.
func TestFetchAfterNoTicker(t *testing.T) {
	f := newFixture(t)
	f.svc.FetchAfterIdentifierFacts(context.Background(), userRun, nil)
}

// TestStartEmpty checks that a refresh over an empty holding is refused
// before a run is created.
func TestStartEmpty(t *testing.T) {
	f := newFixture(t)
	f.queries.EXPECT().ListHeldInstruments(gomock.Any()).Return(nil, nil)
	f.queries.EXPECT().ListHeldTickerDates(gomock.Any()).Return(nil, nil)
	if _, err := f.starter.Start(context.Background()); !errors.Is(err, ErrEmpty) {
		t.Errorf("Start() error = %v, want %v", err, ErrEmpty)
	}
}

// TestOverlaps checks the bounds of an interval against a holding.
func TestOverlaps(t *testing.T) {
	d := func(m time.Month, day int) *time.Time {
		v := time.Date(2024, m, day, 0, 0, 0, 0, time.UTC)
		return &v
	}
	from, before := *d(3, 1), *d(6, 1)
	tests := []struct {
		name string
		from *time.Time
		end  *time.Time
		want bool
	}{
		{name: "open on both sides", want: true},
		{name: "ends on the first day", end: d(3, 1), want: false},
		{name: "ends the day after the first", end: d(3, 2), want: true},
		{name: "starts on the day after the last", from: d(6, 1), want: false},
		{name: "starts on the last day", from: d(5, 31), want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := overlaps(tickervalidity.Interval{From: tt.from, Before: tt.end}, from, before)
			if diff := cmp.Diff(tt.want, got); diff != "" {
				t.Errorf("overlaps() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
