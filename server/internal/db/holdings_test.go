//go:build dbtest

package db_test

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
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
		ViaIdentifierID: &via.ID, Arbiter: gen.ArbiterDatasource,
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
	mics, err := q.ListMICs(context.Background())
	require.NoError(t, err)
	at := mics[slices.IndexFunc(mics, func(m gen.MicMarket) bool { return m.Mic == venue })]
	arg := gen.SetStatedKeyDatedAssociationParams{
		ID: k.ID, UserID: h.user.ID, ViaTickerID: newTicker(t, q, at.Market, ticker).ID, ViaMarket: at.Market, ViaVenue: at.OperatingMic,
		Arbiter: gen.ArbiterDatasource,
	}
	_, err = q.SetStatedKeyDatedAssociation(context.Background(), arg)
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

// resolves writes a row of resolved_tickers that names instr by value at
// market, at venue and in currency. An empty venue or currency is NULL. The
// row is open unless closed is set.
func resolves(t *testing.T, q *gen.Queries, instr gen.Instr, market, value, venue, currency string, closed bool) {
	t.Helper()
	row := dbtest.Ticker(t, q, types.MarketTicker{Market: market, Value: value})
	before := ""
	if closed {
		before = "2026-03-05"
	}
	require.NoError(t, q.CreateResolvedTickers(context.Background(), gen.CreateResolvedTickersParams{
		TickerIds: []uuid.UUID{row.ID}, Markets: []string{market}, InstrIds: []uuid.UUID{instr.ID},
		Venues: []string{venue}, ValidFroms: []string{""}, ValidBefores: []string{before}, Currencies: []string{currency},
	}))
}

// TestListListingNames checks that each listing is reached by every row of
// resolved_tickers in its family, ranked by primary venue, then venue rank,
// then venue. A row without a venue names the listing at its market, a row
// without a currency stays apart from every listing, and a listing that a
// later write creates is reached at once.
func TestListListingNames(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	instr := newInstr(t, q, types.AssetClassEquity)
	gbp := newListing(t, q, instr, "GBP")
	require.NoError(t, q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: gbp.ID, PrimaryMic: ptr.To("XLON")}))
	newListing(t, q, instr, "USD")
	newListing(t, q, instr, "EUR")
	suffix := uuid.NewString()[:6]
	resolves(t, q, instr, "XLON", "ACME"+suffix, "XLON", "GBX", false)
	resolves(t, q, instr, "US", "ACMN"+suffix, "XNAS", "USD", false)
	resolves(t, q, instr, "US", "ACMN"+suffix, "XNYS", "USD", false)
	resolves(t, q, instr, "XSWX", "ACMC"+suffix, "", "CHF", false)
	resolves(t, q, instr, "XAMS", "ACMZ"+suffix, "", "", false)
	newListing(t, q, instr, "CHF")

	rows, err := q.ListListingNames(ctx, []uuid.UUID{instr.ID})
	require.NoError(t, err)
	var got []string
	for _, r := range rows {
		got = append(got, r.Currency+": "+ptr.Deref(r.Market, "-")+":"+strings.TrimSuffix(ptr.Deref(r.TickerValue, "-"), suffix)+
			" at "+ptr.Deref(r.Venue, "-")+" ("+ptr.Deref(r.VenueName, "-")+") "+string(ptr.Deref(r.Relation, ""))+fmt.Sprintf(" %d", ptr.Deref(r.Rank, 0)))
	}
	want := []string{
		"CHF: XSWX:ACMC at - (-) current 1",
		"EUR: -:- at - (-)  0",
		"GBP: XLON:ACME at XLON (LSE) current 1",
		"USD: US:ACMN at XNYS (NYSE) current 1",
		"USD: US:ACMN at XNAS (Nasdaq) current 2",
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListListingNames mismatch (-want +got):\n%s", diff)
	}
}

// TestTickerRelations checks the relation of each ticker to each instrument
// it named, and the names that instr_names takes from them. A ticker is
// defunct where another instrument has an open row of it, and former where
// none has. A ticker that only an assertion names is former. A defunct
// ticker and a ticker without a venue name no instrument in a list.
func TestTickerRelations(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	gone, kept, moved, bare := newInstr(t, q, types.AssetClassEquity), newInstr(t, q, types.AssetClassEquity), newInstr(t, q, types.AssetClassEquity), newInstr(t, q, types.AssetClassEquity)
	suffix := uuid.NewString()[:6]
	def, fmr, asr := "DEF"+suffix, "FMR"+suffix, "ASR"+suffix
	resolves(t, q, gone, "XPAR", def, "XPAR", "EUR", true)
	resolves(t, q, gone, "XBRU", fmr, "XBRU", "EUR", true)
	resolves(t, q, kept, "XPAR", def, "XPAR", "EUR", false)
	resolves(t, q, moved, "XPAR", def, "XPAR", "EUR", true)
	resolves(t, q, bare, "XAMS", "BAR"+suffix, "", "EUR", false)
	admin := newUser(t, q, "relations@example.com")
	run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	attested := dbtest.Ticker(t, q, types.MarketTicker{Market: "US", Value: asr})
	require.NoError(t, q.CreateIdentifierAttestation(ctx, gen.CreateIdentifierAttestationParams{
		ID: db.NewID(), RunID: run.ID, AttestedBy: admin.ID, TickerID: attested.ID, Market: "US", InstrID: &gone.ID, AssertedOn: time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC),
	}))

	tickers, err := q.ListInstrTickers(ctx, gone.ID)
	require.NoError(t, err)
	var got []string
	for _, r := range tickers {
		got = append(got, strings.TrimSuffix(r.Value, suffix)+" "+string(r.Relation))
	}
	if diff := cmp.Diff([]string{"ASR former", "FMR former", "DEF defunct"}, got); diff != "" {
		t.Errorf("relations of the instrument the tickers named before mismatch (-want +got):\n%s", diff)
	}

	rows, err := q.ListInstrNames(ctx, []uuid.UUID{gone.ID, kept.ID, moved.ID, bare.ID})
	require.NoError(t, err)
	named := map[uuid.UUID]string{}
	for _, r := range rows {
		named[r.InstrID] = r.Domain + ":" + strings.TrimSuffix(r.Value, suffix)
	}
	want := map[uuid.UUID]string{gone.ID: "XBRU:FMR", kept.ID: "XPAR:DEF"}
	if diff := cmp.Diff(want, named); diff != "" {
		t.Errorf("instr_names mismatch (-want +got):\n%s", diff)
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
	instr := newInstr(t, q, types.AssetClassEquity)
	via := newIdentifier(t, q, instr, "US0378331005")
	usd := newListing(t, q, instr, "USD")
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
	instr := newInstr(t, q, types.AssetClassEquity)
	via := newIdentifier(t, q, instr, "US0378331005")
	usd := newListing(t, q, instr, "USD")
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
