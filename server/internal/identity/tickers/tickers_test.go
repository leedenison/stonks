package tickers

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
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
