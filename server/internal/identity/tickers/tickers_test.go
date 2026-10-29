package tickers

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestGetSorts checks that Get creates each ticker once, in order of market
// and value, so two creators of one set insert in one order.
func TestGetSorts(t *testing.T) {
	ctx := context.Background()
	mics := mic.Of(map[string]string{"XNAS": "US", "XLON": "XLON"})
	ts := []types.MarketTicker{
		{Market: "XLON", Value: "B"}, {Market: "US", Value: "Z"}, {Market: "US", Value: "A"}, {Market: "US", Value: "Z"},
	}
	q := NewMockQueries(gomock.NewController(t))
	var created gen.CreateTickersParams
	q.EXPECT().CreateTickers(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateTickersParams) error {
		created = arg
		return nil
	})
	q.EXPECT().ListTickers(ctx, gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.ListTickersParams) ([]gen.Ticker, error) {
		var out []gen.Ticker
		for i := range arg.Markets {
			out = append(out, gen.Ticker{Market: arg.Markets[i], Value: arg.Values[i]})
		}
		return out, nil
	})
	if _, err := Get(ctx, q, mics, ts); err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if diff := cmp.Diff([]string{"US", "US", "XLON"}, created.Markets); diff != "" {
		t.Errorf("CreateTickers markets (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]string{"A", "Z", "B"}, created.Values); diff != "" {
		t.Errorf("CreateTickers values (-want +got):\n%s", diff)
	}
	if len(created.Ids) != 3 {
		t.Errorf("CreateTickers ids = %d, want 3", len(created.Ids))
	}
}

// TestDated checks that a dated key gives its ticker at its market and the
// MIC_TICKER at its venue, and that a fixed key gives neither.
func TestDated(t *testing.T) {
	foo := gen.Ticker{ID: uuid.MustParse("00000000-0000-0000-0000-000000000001"), Market: "US", Value: "FOO"}
	rows := map[uuid.UUID]gen.Ticker{foo.ID: foo}
	instr := uuid.MustParse("00000000-0000-0000-0000-000000000002")
	venue, market := "XNAS", "US"
	tests := []struct {
		name   string
		key    gen.StatedKey
		want   types.MarketTicker
		wantAt types.Identifier
		ok     bool
	}{
		{
			name:   "dated",
			key:    gen.StatedKey{ViaTickerID: &foo.ID, ViaMarket: &market, ViaVenue: &venue},
			want:   types.MarketTicker{Market: "US", Value: "FOO"},
			wantAt: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "FOO"},
			ok:     true,
		},
		{name: "fixed", key: gen.StatedKey{InstrID: &instr}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, at, ok := Dated(tc.key, rows)
			if got != tc.want || at != tc.wantAt || ok != tc.ok {
				t.Errorf("Dated() = %v, %v, %v, want %v, %v, %v", got, at, ok, tc.want, tc.wantAt, tc.ok)
			}
		})
	}
}

// TestLock checks that Lock's token holds the rows the query locked, and that
// an empty set of ids skips the query.
func TestLock(t *testing.T) {
	ctx := context.Background()
	a, b := uuid.New(), uuid.New()
	q := NewMockLocker(gomock.NewController(t))
	q.EXPECT().LockTickers(ctx, []uuid.UUID{b, a}).Return([]uuid.UUID{a}, nil)
	locked, err := Lock(ctx, q, []uuid.UUID{b, a})
	if err != nil {
		t.Fatalf("Lock() error = %v", err)
	}
	if diff := cmp.Diff([]uuid.UUID{a}, locked.IDs()); diff != "" {
		t.Errorf("IDs() mismatch (-want +got):\n%s", diff)
	}
	empty, err := Lock(ctx, q, nil)
	if err != nil || len(empty.IDs()) != 0 {
		t.Errorf("Lock(nil) = %v, %v, want an empty token", empty.IDs(), err)
	}
}

// TestLockBatch checks that LockBatch passes its bounds to the query and holds
// the rows the query locked.
func TestLockBatch(t *testing.T) {
	ctx := context.Background()
	after, a := uuid.New(), uuid.New()
	q := NewMockBatchLocker(gomock.NewController(t))
	q.EXPECT().LockTickerBatch(ctx, gen.LockTickerBatchParams{After: &after, Lim: 2}).Return([]uuid.UUID{a}, nil)
	locked, err := LockBatch(ctx, q, &after, 2)
	if err != nil {
		t.Fatalf("LockBatch() error = %v", err)
	}
	if diff := cmp.Diff([]uuid.UUID{a}, locked.IDs()); diff != "" {
		t.Errorf("IDs() mismatch (-want +got):\n%s", diff)
	}
}
