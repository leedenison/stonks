//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// holder is one user with a statement and a stated key to record
// transactions under.
type holder struct {
	user      gen.User
	statement gen.Run
	key       gen.StatedKey
}

func newHolder(t *testing.T, q *gen.Queries, email string) holder {
	t.Helper()
	user := newUser(t, q, email)
	statement := newStatement(t, q, user)
	key, err := q.CreateStatedKey(context.Background(), gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: statement.ID, UserID: user.ID, Identifiers: []types.StatedIdentifier{{Type: "system", Value: "cash"}}})
	require.NoError(t, err)
	return holder{user: user, statement: statement, key: key}
}

// record writes one transaction of quantity against listing.
func (h holder) record(t *testing.T, q *gen.Queries, listing gen.Listing, quantity string) {
	t.Helper()
	day := date(2026, 3, 1)
	_, err := q.CreateTransaction(context.Background(), gen.CreateTransactionParams{
		ID: db.NewID(), UserID: h.user.ID, Broker: gen.BrokerIbkr, StatementID: h.statement.ID, StatedKeyID: h.key.ID,
		InstrumentID: listing.InstrumentID, ListingID: &listing.ID,
		OrderDate: day, SettlementDate: day, AsAt: day,
		Quantity: decimal.RequireFromString(quantity), Currency: &listing.Currency,
	})
	require.NoError(t, err)
}

var decimalEqual = cmp.Comparer(func(a, b decimal.Decimal) bool { return a.Equal(b) })

func holding(instrument gen.Instrument, quantity string) gen.ListHoldingsRow {
	return gen.ListHoldingsRow{InstrumentID: instrument.ID, AssetClass: instrument.AssetClass, Quantity: decimal.RequireFromString(quantity)}
}

// TestListHoldings checks that a holding sums every listing of one
// instrument for one user, that a zero sum is not a holding, and that the
// identifiers read for it are the system's and the caller's own.
func TestListHoldings(t *testing.T) {
	ctx := context.Background()
	ignoreRowIDs := cmpopts.IgnoreFields(gen.Identifier{}, "ID", "CreatedAt")

	t.Run("sums the listings of one instrument", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "sum@example.com")
		instrument := newInstrument(t, q, gen.AssetClassEquity, &h.user.ID)
		h.record(t, q, newListing(t, q, instrument, "USD", &h.user.ID), "10")
		h.record(t, q, newListing(t, q, instrument, "GBP", &h.user.ID), "2.5")

		got, err := q.ListHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListHoldingsRow{holding(instrument, "12.5")}, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("cash", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "cash@example.com")
		gbp := cashListing(t, q, "GBP")
		h.record(t, q, gbp, "100")
		h.record(t, q, gbp, "-25.5")

		got, err := q.ListHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListHoldingsRow{{InstrumentID: gbp.InstrumentID, AssetClass: gen.AssetClassCash, Quantity: decimal.RequireFromString("74.5")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings mismatch (-want +got):\n%s", diff)
		}
		idents, err := q.ListHeldIdentifiers(ctx, h.user.ID)
		require.NoError(t, err)
		wantIdents := []gen.Identifier{{InstrumentID: gbp.InstrumentID, Type: gen.IdentifierTypeCurrency, Value: "GBP", Grain: gen.IdentifierGrainInstrument}}
		if diff := cmp.Diff(wantIdents, idents, ignoreRowIDs); diff != "" {
			t.Errorf("ListHeldIdentifiers mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a zero sum is not a holding", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "zero@example.com")
		usd := cashListing(t, q, "USD")
		h.record(t, q, usd, "10")
		h.record(t, q, usd, "-10")

		got, err := q.ListHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if len(got) != 0 {
			t.Errorf("ListHoldings = %+v, want none", got)
		}
		idents, err := q.ListHeldIdentifiers(ctx, h.user.ID)
		require.NoError(t, err)
		if len(idents) != 0 {
			t.Errorf("ListHeldIdentifiers = %+v, want none", idents)
		}
	})

	t.Run("another user is excluded", func(t *testing.T) {
		q := newTx(t)
		a := newHolder(t, q, "a@example.com")
		b := newHolder(t, q, "b@example.com")
		shared := newInstrument(t, q, gen.AssetClassSecurity, nil)
		_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: shared.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005"})
		require.NoError(t, err)
		describe := func(h holder, listing gen.Listing, description string) {
			t.Helper()
			_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: shared.ID, ListingID: &listing.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/upload"), Value: description, OwnerID: &h.user.ID})
			require.NoError(t, err)
		}
		aLine := newListing(t, q, shared, "USD", &a.user.ID)
		bLine := newListing(t, q, shared, "GBP", &b.user.ID)
		describe(a, aLine, "ACME CORP")
		describe(b, bLine, "ACME CORPORATION")
		a.record(t, q, aLine, "10")
		b.record(t, q, bLine, "7")
		b.record(t, q, cashListing(t, q, "GBP"), "50")

		got, err := q.ListHoldings(ctx, a.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListHoldingsRow{holding(shared, "10")}, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings for a mismatch (-want +got):\n%s", diff)
		}
		idents, err := q.ListHeldIdentifiers(ctx, a.user.ID)
		require.NoError(t, err)
		want := []gen.Identifier{
			{InstrumentID: shared.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005", Grain: gen.IdentifierGrainInstrument},
			{InstrumentID: shared.ID, ListingID: &aLine.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/upload"), Value: "ACME CORP", OwnerID: &a.user.ID, Grain: gen.IdentifierGrainListing},
		}
		if diff := cmp.Diff(want, idents, ignoreRowIDs); diff != "" {
			t.Errorf("ListHeldIdentifiers for a mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("exact decimal", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "exact@example.com")
		tenths := newInstrument(t, q, gen.AssetClassSecurity, &h.user.ID)
		halves := newInstrument(t, q, gen.AssetClassSecurity, &h.user.ID)
		tenthsLine := newListing(t, q, tenths, "USD", &h.user.ID)
		halvesLine := newListing(t, q, halves, "USD", &h.user.ID)
		h.record(t, q, tenthsLine, "0.1")
		h.record(t, q, tenthsLine, "0.2")
		h.record(t, q, halvesLine, "1.50")
		h.record(t, q, halvesLine, "2.50")

		got, err := q.ListHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListHoldingsRow{holding(tenths, "0.3"), holding(halves, "4")}, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings mismatch (-want +got):\n%s", diff)
		}
		if len(got) == 2 && (got[0].Quantity.String() != "0.3" || got[1].Quantity.String() != "4") {
			t.Errorf("ListHoldings quantities = %s, %s, want 0.3 and 4 with no trailing zeros", got[0].Quantity, got[1].Quantity)
		}
	})
}
