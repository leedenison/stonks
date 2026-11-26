package price

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/testutil/scripted"
	"github.com/leedenison/stonks/server/internal/types"
)

// inline has runs execute each run's work in the caller and return its error.
func inline(runs *MockRunner) {
	runs.EXPECT().Sync(gomock.Any(), (*uuid.UUID)(nil), gen.RunKindUploadPrices, gen.RunTriggerAdministrator, gomock.Any()).DoAndReturn(
		func(ctx context.Context, _ *uuid.UUID, kind gen.RunKind, trigger gen.RunTrigger, work run.Work) (gen.Run, error) {
			row := gen.Run{ID: db.NewID(), Kind: kind, Trigger: trigger}
			return row, work(ctx, row)
		})
	runs.EXPECT().ChildAs(gomock.Any(), gomock.Any(), (*uuid.UUID)(nil), gen.RunKindDatasourceFetch, gomock.Any()).DoAndReturn(
		func(ctx context.Context, parent gen.Run, _ *uuid.UUID, kind gen.RunKind, work run.Work) (gen.Run, error) {
			row := gen.Run{ID: db.NewID(), Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID}
			return row, work(ctx, row)
		})
}

// TestUploadFails checks that an error other than a refusal fails the run,
// by reaching the runner as the error of its work.
func TestUploadFails(t *testing.T) {
	boom := errors.New("boom")
	listing := db.NewID()
	series := []Series{{Listing: listing, Symbol: "NASDAQ:ABC", Currency: "USD", Coverage: []types.Period{{From: day(2026, 3, 2), Before: day(2026, 3, 7)}}}}
	tests := []struct {
		name   string
		expect func(s *MockUploadStore)
	}{
		{name: "the fetch cannot be recorded", expect: func(s *MockUploadStore) {
			s.EXPECT().CreateFetch(gomock.Any(), gomock.Any()).Return(gen.Fetch{}, boom)
		}},
		{name: "the rate listings cannot be read", expect: func(s *MockUploadStore) {
			s.EXPECT().CreateFetch(gomock.Any(), gomock.Any()).Return(gen.Fetch{}, nil)
			s.EXPECT().ListRateListings(gomock.Any()).Return(nil, boom)
		}},
		{name: "a series cannot be written", expect: func(s *MockUploadStore) {
			s.EXPECT().CreateFetch(gomock.Any(), gomock.Any()).Return(gen.Fetch{}, nil)
			s.EXPECT().ListRateListings(gomock.Any()).Return(nil, nil)
			s.EXPECT().Tx(gomock.Any(), gomock.Any()).Return(boom)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			store, runs := NewMockUploadStore(ctrl), NewMockRunner(ctrl)
			inline(runs)
			tc.expect(store)
			u := NewUploader(store, runs, nil, nil)
			_, refused, err := u.Upload(context.Background(), &market.Entry{Name: "sheets", Integration: scripted.NewPrices()}, day(2026, 3, 10), series)
			if !errors.Is(err, boom) {
				t.Errorf("Upload() error = %v, want boom", err)
			}
			if len(refused) != 0 {
				t.Errorf("Upload() refused %v, want nothing beside the error", refused)
			}
		})
	}
}
