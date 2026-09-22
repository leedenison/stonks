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
	"github.com/leedenison/stonks/server/internal/ptr"
)

// holder is one user with a statement to record transactions under.
type holder struct {
	user      gen.User
	statement gen.Statement
}

func newHolder(t *testing.T, q *gen.Queries, email string) holder {
	t.Helper()
	user := newUser(t, q, email)
	return holder{user: user, statement: newStatement(t, q, user)}
}

// key makes a stated key stating description, resolved to listing through
// via. A key given no listing is one nothing answered for.
func (h holder) key(t *testing.T, q *gen.Queries, description string, listing *gen.Listing, via *gen.Identifier) gen.StatedKey {
	t.Helper()
	ctx := context.Background()
	k, err := q.CreateStatedKey(ctx, gen.CreateStatedKeyParams{ID: db.NewID(), StatementID: h.statement.ID, UserID: h.user.ID, Description: &description, Identifiers: []types.StatedIdentifier{}})
	require.NoError(t, err)
	if listing == nil {
		return k
	}
	arg := gen.SetStatedKeyAssociationParams{
		ID: k.ID, UserID: h.user.ID,
		InstrumentID: &listing.InstrumentID, ListingID: &listing.ID,
		ViaID: &via.ID, Validity: ptr.To(gen.ValidityConfirmed),
	}
	require.NoError(t, q.SetStatedKeyAssociation(ctx, arg))
	return k
}

// record writes one transaction of quantity against key.
func (h holder) record(t *testing.T, q *gen.Queries, key gen.StatedKey, currency string, quantity string) {
	t.Helper()
	day := date(2026, 3, 1)
	_, err := q.CreateTransaction(context.Background(), gen.CreateTransactionParams{
		ID: db.NewID(), UserID: h.user.ID, Broker: gen.BrokerIbkr, StatementID: h.statement.ID, StatedKeyID: key.ID,
		OrderDate: day, SettlementDate: day, AsAt: day,
		Quantity: decimal.RequireFromString(quantity), Currency: &currency,
	})
	require.NoError(t, err)
}

var decimalEqual = cmp.Comparer(func(a, b decimal.Decimal) bool { return a.Equal(b) })

func holding(instrument gen.Instrument, quantity string) gen.ListHoldingsRow {
	return gen.ListHoldingsRow{InstrumentID: instrument.ID, AssetClass: instrument.AssetClass, Quantity: decimal.RequireFromString(quantity)}
}

// TestListHoldings checks that a holding sums every key resolved to one
// instrument for one user, that a zero sum is not a holding, and that a key
// nothing answered for is no instrument holding.
func TestListHoldings(t *testing.T) {
	ctx := context.Background()
	ignoreRowIDs := cmpopts.IgnoreFields(gen.Identifier{}, "ID", "CreatedAt")

	t.Run("sums the keys resolved to one instrument", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "sum@example.com")
		instrument := newInstrument(t, q, gen.AssetClassEquity)
		via := newIdentifier(t, q, instrument, "US0378331005")
		usd := newListing(t, q, instrument, "USD")
		gbp := newListing(t, q, instrument, "GBP")
		h.record(t, q, h.key(t, q, "ACME CORP", &usd, &via), "USD", "10")
		h.record(t, q, h.key(t, q, "ACME CORPORATION", &gbp, &via), "GBP", "2.5")

		got, err := q.ListHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListHoldingsRow{holding(instrument, "12.5")}, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("cash", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "cash@example.com")
		gbp, via := cashListing(t, q, "GBP")
		key := h.key(t, q, "GBP", &gbp, &via)
		h.record(t, q, key, "GBP", "100")
		h.record(t, q, key, "GBP", "-25.5")

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
		usd, via := cashListing(t, q, "USD")
		key := h.key(t, q, "USD", &usd, &via)
		h.record(t, q, key, "USD", "10")
		h.record(t, q, key, "USD", "-10")

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

	t.Run("a key nothing answered for is no instrument holding", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "unresolved@example.com")
		usd, via := cashListing(t, q, "USD")
		h.record(t, q, h.key(t, q, "MYSTERY FUND", nil, nil), "USD", "40")
		h.record(t, q, h.key(t, q, "USD", &usd, &via), "USD", "5")

		got, err := q.ListHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListHoldingsRow{{InstrumentID: usd.InstrumentID, AssetClass: gen.AssetClassCash, Quantity: decimal.RequireFromString("5")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("another user is excluded", func(t *testing.T) {
		q := newTx(t)
		a := newHolder(t, q, "a@example.com")
		b := newHolder(t, q, "b@example.com")
		shared := newInstrument(t, q, gen.AssetClassSecurity)
		via := newIdentifier(t, q, shared, "US0378331005")
		aLine := newListing(t, q, shared, "USD")
		bLine := newListing(t, q, shared, "GBP")
		a.record(t, q, a.key(t, q, "ACME CORP", &aLine, &via), "USD", "10")
		b.record(t, q, b.key(t, q, "ACME CORPORATION", &bLine, &via), "GBP", "7")
		gbp, gbpVia := cashListing(t, q, "GBP")
		b.record(t, q, b.key(t, q, "GBP", &gbp, &gbpVia), "GBP", "50")

		got, err := q.ListHoldings(ctx, a.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListHoldingsRow{holding(shared, "10")}, got, decimalEqual); diff != "" {
			t.Errorf("ListHoldings for a mismatch (-want +got):\n%s", diff)
		}
		idents, err := q.ListHeldIdentifiers(ctx, a.user.ID)
		require.NoError(t, err)
		want := []gen.Identifier{{InstrumentID: shared.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005", Grain: gen.IdentifierGrainInstrument}}
		if diff := cmp.Diff(want, idents, ignoreRowIDs); diff != "" {
			t.Errorf("ListHeldIdentifiers for a mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("exact decimal", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "exact@example.com")
		tenths := newInstrument(t, q, gen.AssetClassSecurity)
		halves := newInstrument(t, q, gen.AssetClassSecurity)
		tenthsVia := newIdentifier(t, q, tenths, "US0378331005")
		halvesVia := newIdentifier(t, q, halves, "US5949181045")
		tenthsLine := newListing(t, q, tenths, "USD")
		halvesLine := newListing(t, q, halves, "USD")
		tenthsKey := h.key(t, q, "TENTHS", &tenthsLine, &tenthsVia)
		halvesKey := h.key(t, q, "HALVES", &halvesLine, &halvesVia)
		h.record(t, q, tenthsKey, "USD", "0.1")
		h.record(t, q, tenthsKey, "USD", "0.2")
		h.record(t, q, halvesKey, "USD", "1.50")
		h.record(t, q, halvesKey, "USD", "2.50")

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
