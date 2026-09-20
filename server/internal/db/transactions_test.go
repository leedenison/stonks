//go:build dbtest

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// TestStatedKeys checks that one statement holds one row per distinct key, and
// that the identifiers column round-trips through its Go type.
func TestStatedKeys(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	user := newUser(t, q, "keys@example.com")
	statement, other := newStatement(t, q, user), newStatement(t, q, user)
	cash := gen.AssetClassCash
	key := func(statement gen.Statement) gen.CreateStatedKeyParams {
		return gen.CreateStatedKeyParams{
			ID: db.NewID(), StatementID: statement.ID, UserID: user.ID, AssetClass: &cash, Currency: ptr.To("USD"),
			Identifiers: []types.StatedIdentifier{{Type: "system", Value: "cash"}},
		}
	}

	created, err := q.CreateStatedKey(ctx, key(statement))
	require.NoError(t, err)
	if created.Description != nil || created.AssetClass == nil || *created.AssetClass != gen.AssetClassCash {
		t.Errorf("CreateStatedKey = %+v, want asset class cash and no description", created)
	}
	listed, err := q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	if diff := cmp.Diff([]gen.StatedKey{created}, listed); diff != "" {
		t.Errorf("ListStatedKeys mismatch (-want +got):\n%s", diff)
	}

	if _, err := q.CreateStatedKey(ctx, key(other)); err != nil {
		t.Errorf("CreateStatedKey of the same key in another statement: err = %v, want nil", err)
	}
	// The unique violation aborts the transaction, so it is the last statement.
	if _, err := q.CreateStatedKey(ctx, key(statement)); !db.IsConflict(err) {
		t.Errorf("CreateStatedKey of a key the statement already holds: err = %v, want a conflict", err)
	}
}

// TestTransactions checks the decimal and date round trip, and the half-open
// period the delete is keyed on.
func TestTransactions(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	user := newUser(t, q, "txs@example.com")
	statement := newStatement(t, q, user)
	usd := cashListing(t, q, "USD")
	key, err := q.CreateStatedKey(ctx, gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: statement.ID, UserID: user.ID, Identifiers: []types.StatedIdentifier{{Type: "system", Value: "cash"}}})
	require.NoError(t, err)
	create := func(broker gen.Broker, order time.Time, quantity string) gen.Transaction {
		t.Helper()
		row, err := q.CreateTransaction(ctx, gen.CreateTransactionParams{
			ID: db.NewID(), UserID: user.ID, Broker: broker, StatementID: statement.ID, StatedKeyID: key.ID,
			InstrumentID: usd.InstrumentID, ListingID: &usd.ID,
			OrderDate: order, SettlementDate: order.AddDate(0, 0, 2), AsAt: order,
			Quantity: decimal.RequireFromString(quantity), Currency: ptr.To("USD"),
		})
		require.NoError(t, err)
		return row
	}

	got := create(gen.BrokerIbkr, date(2026, time.March, 1), "-0.0001")
	if !got.Quantity.Equal(decimal.RequireFromString("-0.0001")) || got.Quantity.String() != "-0.0001" {
		t.Errorf("CreateTransaction quantity = %s, want -0.0001", got.Quantity)
	}
	if !got.OrderDate.Equal(date(2026, time.March, 1)) || !got.SettlementDate.Equal(date(2026, time.March, 3)) {
		t.Errorf("CreateTransaction dates = %s, %s, want 2026-03-01 and 2026-03-03", got.OrderDate, got.SettlementDate)
	}

	from, before := date(2026, time.March, 1), date(2026, time.April, 1)
	create(gen.BrokerIbkr, from.AddDate(0, 0, -1), "1")
	create(gen.BrokerIbkr, before.AddDate(0, 0, -1), "1")
	create(gen.BrokerIbkr, before, "1")
	create(gen.BrokerSchwab, from, "1")
	n, err := q.DeleteTransactions(ctx, gen.DeleteTransactionsParams{UserID: user.ID, Broker: gen.BrokerIbkr, OrderFrom: from, OrderBefore: before})
	require.NoError(t, err)
	if n != 2 {
		t.Errorf("DeleteTransactions over [%s, %s) = %d rows, want 2: the one at from and the one the day before before", from.Format(time.DateOnly), before.Format(time.DateOnly), n)
	}
}
