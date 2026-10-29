package attestidentifier

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	admin = uuid.MustParse("00000000-0000-0000-0000-000000000002")
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
	svc   *Service
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{store: NewMockStore(ctrl)}
	f.svc = New(f.store, NewMockRunner(ctrl), NewMockFacts(ctrl), mics, func() time.Time { return today })
	return f
}

// tx runs one transaction in the caller.
func (f *fixture) tx() {
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, fn func(Queries) error) error {
		return fn(f.store)
	})
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
