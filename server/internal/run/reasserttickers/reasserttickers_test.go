package reasserttickers

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"
)

type fixture struct {
	queries    *MockQueries
	runs       *MockRunner
	reasserter *MockReasserter
	facts      *MockFacts
	svc        *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{queries: NewMockQueries(ctrl), runs: NewMockRunner(ctrl), reasserter: NewMockReasserter(ctrl), facts: NewMockFacts(ctrl)}
	f.svc = New(f.queries, f.runs, f.reasserter, f.facts)
	return f
}

// TestStartEmpty checks that a reassert tickers run with no held ticker is
// refused before a run is created.
func TestStartEmpty(t *testing.T) {
	f := newFixture(t)
	f.queries.EXPECT().ListHeldTickers(gomock.Any()).Return(nil, nil)
	if _, err := f.svc.Start(context.Background()); !errors.Is(err, ErrEmpty) {
		t.Errorf("Start() error = %v, want %v", err, ErrEmpty)
	}
}
