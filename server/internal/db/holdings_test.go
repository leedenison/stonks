//go:build dbtest

package db_test

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// holder is one user with a statement that holds the transactions.
type holder struct {
	user gen.User
	stmt gen.Stmt
}

func newHolder(t *testing.T, q *gen.Queries, email string) holder {
	t.Helper()
	user := newUser(t, q, email)
	return holder{user: user, stmt: newStmt(t, q, user)}
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
	k := upsertKey(t, q, h.stmt, gen.UpsertStatedKeyParams{Currency: currency, Identifiers: []types.Identifier{described}})
	if listing == nil {
		return k
	}
	arg := gen.SetStatedKeyAssociationParams{
		ID: k.ID, UserID: h.user.ID,
		InstrID: &listing.InstrID, ListingID: &listing.ID,
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
	k := upsertKey(t, q, h.stmt, gen.UpsertStatedKeyParams{Currency: &currency, Identifiers: []types.Identifier{stated}})
	arg := gen.SetStatedKeyDatedAssociationParams{ID: k.ID, UserID: h.user.ID, ViaDomain: venue, ViaValue: ticker, Arbiter: gen.ArbiterDatasource}
	_, err := q.SetStatedKeyDatedAssociation(context.Background(), arg)
	require.NoError(t, err)
	return k
}

// record writes one transaction of quantity against key, stated as at
// 2026-03-01.
func (h holder) record(t *testing.T, q *gen.Queries, key gen.StatedKey, quantity string) {
	t.Helper()
	h.recordOn(t, q, key, date(2026, 3, 1), quantity)
}

// recordOn writes one transaction of quantity against key, stated as at
// day and ordered a week before it.
func (h holder) recordOn(t *testing.T, q *gen.Queries, key gen.StatedKey, day time.Time, quantity string) {
	t.Helper()
	_, err := q.CreateTxn(context.Background(), gen.CreateTxnParams{
		ID: db.NewID(), UserID: h.user.ID, Broker: gen.BrokerIbkr, StmtID: h.stmt.ID, StatedKeyID: key.ID,
		OrderDate: day.AddDate(0, 0, -7), SettlementDate: day, AsAt: day,
		Quantity: decimal.RequireFromString(quantity),
	})
	require.NoError(t, err)
}

var decimalEqual = cmp.Comparer(func(a, b decimal.Decimal) bool { return a.Equal(b) })

// venueTicker gives listing a ticker at venue.
func venueTicker(t *testing.T, q *gen.Queries, listing gen.Listing, venue, value string) {
	t.Helper()
	_, err := q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrID: listing.InstrID, ListingID: &listing.ID, Type: types.IdentifierTypeMicTicker, Domain: venue, Value: value})
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
		{ListingID: bare.ID, InstrID: instrument.ID, Currency: "CHF"},
		{ListingID: ranked.ID, InstrID: instrument.ID, Currency: "EUR", TickerDomain: ptr.To("XLON"), TickerValue: ptr.To("ACMX"), Venue: "LSE"},
		{ListingID: primary.ID, InstrID: instrument.ID, Currency: "GBP", TickerDomain: ptr.To("XLON"), TickerValue: ptr.To("ACME"), Venue: "LSE"},
		{ListingID: unranked.ID, InstrID: instrument.ID, Currency: "USD", TickerDomain: ptr.To("XXXA"), TickerValue: ptr.To("ACMU"), Venue: "XXXA"},
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListListingNames mismatch (-want +got):\n%s", diff)
	}
}

// TestListHoldingKeys checks that the user's keys with a transaction are
// listed earliest key first, resolved or grouped alike. It checks that sums
// are exact and trimmed of trailing zeros, and that another user's keys are
// omitted.
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
	later := holder{user: h.user, stmt: newStmt(t, q, h.user)}
	newer := later.key(t, q, "ACME CORP LATER", &usd, &via)
	later.record(t, q, newer, "1")
	other := newHolder(t, q, "other@example.com")
	other.record(t, q, other.key(t, q, "ACME CORP", &usd, &via), "7")
	halves := later.key(t, q, "HALVES", &usd, &via)
	later.record(t, q, halves, "1.50")
	later.record(t, q, halves, "2.50")

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
	want := []row{{resolved.ID, "7.5"}, {grouped.ID, "3"}, {newer.ID, "1"}, {halves.ID, "4"}}
	if diff := cmp.Diff(want, rows); diff != "" {
		t.Errorf("ListHoldingKeys mismatch (-want +got):\n%s", diff)
	}
}

// TestListDatedTxns checks that the transactions of the user's dated
// keys are listed by key then date, and that fixed keys, unresolved keys and
// another user's are left out.
func TestListDatedTxns(t *testing.T) {
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

	got, err := q.ListDatedTxns(ctx, h.user.ID)
	require.NoError(t, err)
	want := []gen.ListDatedTxnsRow{
		{StatedKeyID: acme.ID, AsAt: date(2026, 3, 1), Quantity: decimal.RequireFromString("-1.5")},
		{StatedKeyID: acme.ID, AsAt: date(2026, 3, 2), Quantity: decimal.RequireFromString("4")},
		{StatedKeyID: zeta.ID, AsAt: date(2026, 3, 1), Quantity: decimal.RequireFromString("7")},
	}
	if diff := cmp.Diff(want, got, decimalEqual); diff != "" {
		t.Errorf("ListDatedTxns mismatch (-want +got):\n%s", diff)
	}
}
