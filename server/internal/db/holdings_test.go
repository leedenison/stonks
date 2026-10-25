//go:build dbtest

package db_test

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// holder is one user with a statement that holds the transactions.
type holder struct {
	user      gen.User
	statement gen.Statement
}

func newHolder(t *testing.T, q *gen.Queries, email string) holder {
	t.Helper()
	user := newUser(t, q, email)
	return holder{user: user, statement: newStatement(t, q, user)}
}

// key makes a stated key, resolved where a listing is given.
func (h holder) key(t *testing.T, q *gen.Queries, description string, listing *gen.Listing, via *gen.Identifier) gen.StatedKey {
	t.Helper()
	return h.keyIn(t, q, description, nil, listing, via)
}

// keyIn makes a stated key that states currency, or none where currency is
// nil, resolved where a listing is given.
func (h holder) keyIn(t *testing.T, q *gen.Queries, description string, currency *string, listing *gen.Listing, via *gen.Identifier) gen.StatedKey {
	t.Helper()
	ctx := context.Background()
	described := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: description}
	k := upsertKey(t, q, h.statement, gen.UpsertStatedKeyParams{Currency: currency, Identifiers: []types.Identifier{described}})
	if listing == nil {
		return k
	}
	arg := gen.SetStatedKeyAssociationParams{
		ID: k.ID, UserID: h.user.ID,
		InstrumentID: &listing.InstrumentID, ListingID: &listing.ID,
		ViaID: &via.ID, Arbiter: gen.ArbiterDatasource,
	}
	_, err := q.SetStatedKeyAssociation(ctx, arg)
	require.NoError(t, err)
	return k
}

// datedKey makes a stated key that states a ticker at venue and currency,
// dated through the ticker.
func (h holder) datedKey(t *testing.T, q *gen.Queries, venue, ticker, currency string) gen.StatedKey {
	t.Helper()
	stated := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: venue, Value: ticker}
	k := upsertKey(t, q, h.statement, gen.UpsertStatedKeyParams{Currency: &currency, Identifiers: []types.Identifier{stated}})
	arg := gen.SetStatedKeyDatedAssociationParams{ID: k.ID, UserID: h.user.ID, ViaDomain: venue, ViaValue: ticker, Arbiter: gen.ArbiterDatasource}
	_, err := q.SetStatedKeyDatedAssociation(context.Background(), arg)
	require.NoError(t, err)
	return k
}

// record writes one transaction of quantity against key on 2026-03-01.
func (h holder) record(t *testing.T, q *gen.Queries, key gen.StatedKey, quantity string) {
	t.Helper()
	h.recordOn(t, q, key, date(2026, 3, 1), quantity)
}

// recordOn writes one transaction of quantity against key on day.
func (h holder) recordOn(t *testing.T, q *gen.Queries, key gen.StatedKey, day time.Time, quantity string) {
	t.Helper()
	_, err := q.CreateTransaction(context.Background(), gen.CreateTransactionParams{
		ID: db.NewID(), UserID: h.user.ID, Broker: gen.BrokerIbkr, StatementID: h.statement.ID, StatedKeyID: key.ID,
		OrderDate: day, SettlementDate: day, AsAt: day,
		Quantity: decimal.RequireFromString(quantity),
	})
	require.NoError(t, err)
}

var decimalEqual = cmp.Comparer(func(a, b decimal.Decimal) bool { return a.Equal(b) })

// venueTicker gives listing a ticker at venue.
func venueTicker(t *testing.T, q *gen.Queries, listing gen.Listing, venue, value string) {
	t.Helper()
	_, err := q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: listing.InstrumentID, ListingID: &listing.ID, Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value})
	require.NoError(t, err)
}

// TestListListingNames checks that each listing is named by one ticker,
// chosen by primary venue, then rank, then MIC, and has none where it has
// no tickers.
func TestListListingNames(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	instrument := newInstrument(t, q, types.AssetClassEquity)
	primary := newListing(t, q, instrument, "GBP")
	require.NoError(t, q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: primary.ID, PrimaryMic: ptr.To("XLON")}))
	venueTicker(t, q, primary, "XNYS", "ACME")
	venueTicker(t, q, primary, "XLON", "ACME")
	ranked := newListing(t, q, instrument, "EUR")
	venueTicker(t, q, ranked, "AQSE", "ACMX")
	venueTicker(t, q, ranked, "XLON", "ACMX")
	unranked := newListing(t, q, instrument, "USD")
	venueTicker(t, q, unranked, "XXXB", "ACMU")
	venueTicker(t, q, unranked, "XXXA", "ACMU")
	bare := newListing(t, q, instrument, "CHF")

	got, err := q.ListListingNames(ctx, []uuid.UUID{instrument.ID})
	require.NoError(t, err)
	want := []gen.ListListingNamesRow{
		{ListingID: bare.ID, InstrumentID: instrument.ID, Currency: "CHF"},
		{ListingID: ranked.ID, InstrumentID: instrument.ID, Currency: "EUR", TickerDomain: ptr.To("XLON"), TickerValue: ptr.To("ACMX"), Venue: "LSE"},
		{ListingID: primary.ID, InstrumentID: instrument.ID, Currency: "GBP", TickerDomain: ptr.To("XLON"), TickerValue: ptr.To("ACME"), Venue: "LSE"},
		{ListingID: unranked.ID, InstrumentID: instrument.ID, Currency: "USD", TickerDomain: ptr.To("XXXA"), TickerValue: ptr.To("ACMU"), Venue: "XXXA"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListListingNames mismatch (-want +got):\n%s", diff)
	}
}

// TestListHoldingKeys checks that the user's keys with a transaction are
// listed with their transactions summed, resolved or grouped alike, earliest
// key first, and that another user's keys are omitted.
func TestListHoldingKeys(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	h := newHolder(t, q, "keys@example.com")
	instrument := newInstrument(t, q, types.AssetClassEquity)
	via := newIdentifier(t, q, instrument, "US0378331005")
	usd := newListing(t, q, instrument, "USD")
	resolved := h.key(t, q, "ACME CORP", &usd, &via)
	h.record(t, q, resolved, "10")
	h.record(t, q, resolved, "-2.5")
	grouped := h.key(t, q, "ACME CORPORATION", nil, nil)
	require.NoError(t, q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: h.user.ID, Ids: []uuid.UUID{grouped.ID}, GroupIds: []uuid.UUID{grouped.ID}}))
	h.record(t, q, grouped, "3")
	h.key(t, q, "NEVER TRADED", nil, nil)
	later := holder{user: h.user, statement: newStatement(t, q, h.user)}
	newer := later.key(t, q, "ACME CORP LATER", &usd, &via)
	later.record(t, q, newer, "1")
	other := newHolder(t, q, "other@example.com")
	other.record(t, q, other.key(t, q, "ACME CORP", &usd, &via), "7")

	got, err := q.ListHoldingKeys(ctx, h.user.ID)
	require.NoError(t, err)
	type row struct {
		Key      uuid.UUID
		Quantity string
	}
	var rows []row
	for _, r := range got {
		rows = append(rows, row{r.StatedKey.ID, r.Quantity.String()})
	}
	want := []row{{resolved.ID, "7.5"}, {grouped.ID, "3"}, {newer.ID, "1"}}
	if diff := cmp.Diff(want, rows); diff != "" {
		t.Errorf("ListHoldingKeys mismatch (-want +got):\n%s", diff)
	}
}

// pence returns the seed identifier naming the pound instrument by GBX.
func pence(t *testing.T, q *gen.Queries) gen.Identifier {
	t.Helper()
	found, err := q.FindIdentifier(context.Background(), gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBX"})
	require.NoError(t, err)
	return found.Identifier
}

func holding(instrument gen.Instrument, quantity string) gen.ListInstrumentHoldingsRow {
	return gen.ListInstrumentHoldingsRow{InstrumentID: instrument.ID, Quantity: decimal.RequireFromString(quantity)}
}

// TestListInstrumentHoldings checks that a holding sums every key resolved
// to one instrument for one user, that cash sums in its family's unit, that a
// zero sum is returned, and that an unresolved key is no instrument holding.
func TestListInstrumentHoldings(t *testing.T) {
	ctx := context.Background()
	ignoreRowIDs := cmpopts.IgnoreFields(gen.Identifier{}, "ID", "CreatedAt")

	t.Run("sums the keys resolved to one instrument", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "sum@example.com")
		instrument := newInstrument(t, q, types.AssetClassEquity)
		via := newIdentifier(t, q, instrument, "US0378331005")
		usd := newListing(t, q, instrument, "USD")
		gbp := newListing(t, q, instrument, "GBP")
		h.record(t, q, h.key(t, q, "ACME CORP", &usd, &via), "10")
		h.record(t, q, h.key(t, q, "ACME CORPORATION", &gbp, &via), "2.5")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListInstrumentHoldingsRow{holding(instrument, "12.5")}, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("cash", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "cash@example.com")
		gbp, via := cashListing(t, q, "GBP")
		key := h.key(t, q, "GBP", &gbp, &via)
		h.record(t, q, key, "100")
		h.record(t, q, key, "-25.5")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListInstrumentHoldingsRow{{InstrumentID: gbp.InstrumentID, Quantity: decimal.RequireFromString("74.5")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
		idents, err := q.ListIdentifiersOf(ctx, []uuid.UUID{gbp.InstrumentID})
		require.NoError(t, err)
		// The pound instrument is named by every code of its family.
		wantIdents := []gen.Identifier{
			{InstrumentID: gbp.InstrumentID, Type: types.IdentifierTypeCurrency, Value: "GBP", Grain: types.IdentifierGrainInstrument},
			{InstrumentID: gbp.InstrumentID, Type: types.IdentifierTypeCurrency, Value: "GBX", Grain: types.IdentifierGrainInstrument},
		}
		if diff := cmp.Diff(wantIdents, idents, ignoreRowIDs); diff != "" {
			t.Errorf("ListIdentifiersOf mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("cash sums its family in the family's unit", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "family@example.com")
		gbp, gbpVia := cashListing(t, q, "GBP")
		gbxVia := pence(t, q)
		h.record(t, q, h.keyIn(t, q, "GBP", ptr.To("GBP"), &gbp, &gbpVia), "5")
		penceKey := h.keyIn(t, q, "GBX", ptr.To("GBX"), &gbp, &gbxVia)
		h.record(t, q, penceKey, "1000")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListInstrumentHoldingsRow{{InstrumentID: gbp.InstrumentID, Quantity: decimal.RequireFromString("15")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
		// A key's own sum stays in the currency it states.
		keys, err := q.ListHoldingKeys(ctx, h.user.ID)
		require.NoError(t, err)
		i := slices.IndexFunc(keys, func(k gen.ListHoldingKeysRow) bool { return k.StatedKey.ID == penceKey.ID })
		require.GreaterOrEqual(t, i, 0)
		if got := keys[i].Quantity.String(); got != "1000" {
			t.Errorf("pence key quantity = %s, want 1000", got)
		}
	})

	t.Run("pence and pounds that cancel sum to zero", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "cancel@example.com")
		gbp, gbpVia := cashListing(t, q, "GBP")
		gbxVia := pence(t, q)
		h.record(t, q, h.keyIn(t, q, "GBP", ptr.To("GBP"), &gbp, &gbpVia), "-5")
		h.record(t, q, h.keyIn(t, q, "GBX", ptr.To("GBX"), &gbp, &gbxVia), "500")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListInstrumentHoldingsRow{{InstrumentID: gbp.InstrumentID, Quantity: decimal.Zero}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a security quoted in pence counts shares", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "shares@example.com")
		instrument := newInstrument(t, q, types.AssetClassStock)
		via := newIdentifier(t, q, instrument, "GB00BH4HKS39")
		gbp := newListing(t, q, instrument, "GBP")
		h.record(t, q, h.keyIn(t, q, "VODAFONE GRP", ptr.To("GBX"), &gbp, &via), "100")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListInstrumentHoldingsRow{holding(instrument, "100")}, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("an unresolved key is no instrument holding", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "unresolved@example.com")
		usd, via := cashListing(t, q, "USD")
		h.record(t, q, h.key(t, q, "MYSTERY FUND", nil, nil), "40")
		h.record(t, q, h.key(t, q, "USD", &usd, &via), "5")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListInstrumentHoldingsRow{{InstrumentID: usd.InstrumentID, Quantity: decimal.RequireFromString("5")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("another user is excluded", func(t *testing.T) {
		q := newTx(t)
		a := newHolder(t, q, "a@example.com")
		b := newHolder(t, q, "b@example.com")
		shared := newInstrument(t, q, types.AssetClassSecurity)
		via := newIdentifier(t, q, shared, "US0378331005")
		aLine := newListing(t, q, shared, "USD")
		bLine := newListing(t, q, shared, "GBP")
		a.record(t, q, a.key(t, q, "ACME CORP", &aLine, &via), "10")
		b.record(t, q, b.key(t, q, "ACME CORPORATION", &bLine, &via), "7")
		gbp, gbpVia := cashListing(t, q, "GBP")
		b.record(t, q, b.key(t, q, "GBP", &gbp, &gbpVia), "50")

		got, err := q.ListInstrumentHoldings(ctx, a.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListInstrumentHoldingsRow{holding(shared, "10")}, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings for a mismatch (-want +got):\n%s", diff)
		}

	})

	t.Run("exact decimal", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "exact@example.com")
		tenths := newInstrument(t, q, types.AssetClassSecurity)
		halves := newInstrument(t, q, types.AssetClassSecurity)
		tenthsVia := newIdentifier(t, q, tenths, "US0378331005")
		halvesVia := newIdentifier(t, q, halves, "US5949181045")
		tenthsLine := newListing(t, q, tenths, "USD")
		halvesLine := newListing(t, q, halves, "USD")
		tenthsKey := h.key(t, q, "TENTHS", &tenthsLine, &tenthsVia)
		halvesKey := h.key(t, q, "HALVES", &halvesLine, &halvesVia)
		h.record(t, q, tenthsKey, "0.1")
		h.record(t, q, tenthsKey, "0.2")
		h.record(t, q, halvesKey, "1.50")
		h.record(t, q, halvesKey, "2.50")

		got, err := q.ListInstrumentHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		if diff := cmp.Diff([]gen.ListInstrumentHoldingsRow{holding(tenths, "0.3"), holding(halves, "4")}, got, decimalEqual); diff != "" {
			t.Errorf("ListInstrumentHoldings mismatch (-want +got):\n%s", diff)
		}
		if len(got) == 2 && (got[0].Quantity.String() != "0.3" || got[1].Quantity.String() != "4") {
			t.Errorf("ListInstrumentHoldings quantities = %s, %s, want 0.3 and 4 with no trailing zeros", got[0].Quantity, got[1].Quantity)
		}
	})
}

// group puts every key in one group, named by the first, as the ingest does.
func (h holder) group(t *testing.T, q *gen.Queries, keys ...gen.StatedKey) uuid.UUID {
	t.Helper()
	ids := make([]uuid.UUID, 0, len(keys))
	groups := make([]uuid.UUID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
		groups = append(groups, keys[0].ID)
	}
	arg := gen.SetStatedKeyGroupsParams{UserID: h.user.ID, Ids: ids, GroupIds: groups}
	require.NoError(t, q.SetStatedKeyGroups(context.Background(), arg))
	return keys[0].ID
}

// TestListGroupHoldings checks that a group sums its unresolved keys and
// none of a dated key's transactions, that a zero sum is returned, and that
// another user's group is excluded.
func TestListGroupHoldings(t *testing.T) {
	ctx := context.Background()

	t.Run("sums the keys of one group", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "group@example.com")
		one := h.key(t, q, "ACME CORP", nil, nil)
		two := h.key(t, q, "ACME CORPORATION", nil, nil)
		id := h.group(t, q, one, two)
		h.record(t, q, one, "10")
		h.record(t, q, two, "2.5")

		got, err := q.ListGroupHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListGroupHoldingsRow{{GroupID: id, Quantity: decimal.RequireFromString("12.5")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListGroupHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a zero sum is returned", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "groupzero@example.com")
		one := h.key(t, q, "ACME CORP", nil, nil)
		id := h.group(t, q, one)
		h.record(t, q, one, "10")
		h.record(t, q, one, "-10")

		got, err := q.ListGroupHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListGroupHoldingsRow{{GroupID: id, Quantity: decimal.Zero}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListGroupHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("a dated key's transactions are left out", func(t *testing.T) {
		q := newTx(t)
		h := newHolder(t, q, "groupdated@example.com")
		one := h.key(t, q, "ACME CORP", nil, nil)
		dated := h.datedKey(t, q, "XNAS", "ACME", "USD")
		id := h.group(t, q, one, dated)
		h.record(t, q, one, "10")
		h.record(t, q, dated, "4")

		got, err := q.ListGroupHoldings(ctx, h.user.ID)
		require.NoError(t, err)
		want := []gen.ListGroupHoldingsRow{{GroupID: id, Quantity: decimal.RequireFromString("10")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListGroupHoldings mismatch (-want +got):\n%s", diff)
		}
	})

	t.Run("another user's group is excluded", func(t *testing.T) {
		q := newTx(t)
		a := newHolder(t, q, "groupa@example.com")
		b := newHolder(t, q, "groupb@example.com")
		mine := a.key(t, q, "ACME CORP", nil, nil)
		theirs := b.key(t, q, "ACME CORP", nil, nil)
		id := a.group(t, q, mine)
		b.group(t, q, theirs)
		a.record(t, q, mine, "10")
		b.record(t, q, theirs, "7")

		got, err := q.ListGroupHoldings(ctx, a.user.ID)
		require.NoError(t, err)
		want := []gen.ListGroupHoldingsRow{{GroupID: id, Quantity: decimal.RequireFromString("10")}}
		if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
			t.Errorf("ListGroupHoldings for a mismatch (-want +got):\n%s", diff)
		}
	})
}

// TestListDatedTransactions checks that the transactions of the user's dated
// keys are listed by key then date, and that fixed keys, unresolved keys and
// another user's are left out.
func TestListDatedTransactions(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	h := newHolder(t, q, "dated@example.com")
	instrument := newInstrument(t, q, types.AssetClassEquity)
	via := newIdentifier(t, q, instrument, "US0378331005")
	usd := newListing(t, q, instrument, "USD")
	h.record(t, q, h.key(t, q, "ACME CORP", &usd, &via), "10")
	h.record(t, q, h.key(t, q, "MYSTERY FUND", nil, nil), "3")
	acme := h.datedKey(t, q, "XNAS", "ACME", "USD")
	h.recordOn(t, q, acme, date(2026, 3, 2), "4")
	h.recordOn(t, q, acme, date(2026, 3, 1), "-1.5")
	zeta := h.datedKey(t, q, "XNAS", "ZETA", "USD")
	h.record(t, q, zeta, "7")
	other := newHolder(t, q, "otherdated@example.com")
	other.record(t, q, other.datedKey(t, q, "XNAS", "ACME", "USD"), "9")

	got, err := q.ListDatedTransactions(ctx, h.user.ID)
	require.NoError(t, err)
	want := []gen.ListDatedTransactionsRow{
		{StatedKeyID: acme.ID, OrderDate: date(2026, 3, 1), Quantity: decimal.RequireFromString("-1.5")},
		{StatedKeyID: acme.ID, OrderDate: date(2026, 3, 2), Quantity: decimal.RequireFromString("4")},
		{StatedKeyID: zeta.ID, OrderDate: date(2026, 3, 1), Quantity: decimal.RequireFromString("7")},
	}
	if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
		t.Errorf("ListDatedTransactions mismatch (-want +got):\n%s", diff)
	}
}
