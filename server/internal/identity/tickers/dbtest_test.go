//go:build dbtest

package tickers

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/types"
)

var pool *pgxpool.Pool

func TestMain(m *testing.M) { dbtest.Main(m, &pool) }

func setup(t *testing.T) (*gen.Queries, mic.MICMap) {
	t.Helper()
	q := gen.New(dbtest.Begin(t, pool))
	mics, err := mic.Load(context.Background(), q)
	require.NoError(t, err)
	return q, mics
}

// fresh returns a ticker at market that no other test writes.
func fresh(market string) types.MarketTicker {
	return types.MarketTicker{Market: market, Value: "T" + uuid.NewString()[:8]}
}

// TestGet checks that Get gives one ticker one row, however it is asked.
func TestGet(t *testing.T) {
	ctx := context.Background()
	q, mics := setup(t)
	a, b := fresh("US"), fresh("XLON")

	first, err := Get(ctx, q, mics, []types.MarketTicker{a, b, a})
	require.NoError(t, err)
	require.Len(t, first, 2)
	second, err := Get(ctx, q, mics, []types.MarketTicker{a})
	require.NoError(t, err)
	if second[a].ID != first[a].ID {
		t.Errorf("Get of %s again = %s, want %s", a.Name(), second[a].ID, first[a].ID)
	}
	if got := Of(first[b]); got != b {
		t.Errorf("Get row of %s spells %s", b.Name(), got.Name())
	}
	found, err := ByID(ctx, q, []uuid.UUID{first[b].ID})
	require.NoError(t, err)
	if Of(found[first[b].ID]) != b {
		t.Errorf("ByID(%s) = %+v, want %s", first[b].ID, found, b.Name())
	}
}

// TestGetSpelling checks that Get refuses a call holding a ticker the system
// cannot spell, and leaves the table unchanged.
func TestGetSpelling(t *testing.T) {
	ctx := context.Background()
	for _, tc := range []struct {
		name string
		bad  types.MarketTicker
	}{
		{name: "a class separator other than a dot", bad: types.MarketTicker{Market: "US", Value: "BRK/B"}},
		{name: "a venue in place of its market", bad: types.MarketTicker{Market: "XNAS", Value: "ABC"}},
		{name: "an unknown market", bad: types.MarketTicker{Market: "ZZZZ", Value: "ABC"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			q, mics := setup(t)
			good := fresh("US")
			if _, err := Get(ctx, q, mics, []types.MarketTicker{good, tc.bad}); !errors.Is(err, ErrSpelling) {
				t.Fatalf("Get() error = %v, want ErrSpelling", err)
			}
			found, err := Find(ctx, q, []types.MarketTicker{good, tc.bad})
			require.NoError(t, err)
			if len(found) != 0 {
				t.Errorf("Find after a refused Get = %+v, want no rows", found)
			}
		})
	}
}
