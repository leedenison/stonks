//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// sqlstate reports whether err is a Postgres error with the given SQLSTATE.
func sqlstate(err error, code string) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == code
}

func newUser(t *testing.T, q *gen.Queries, email string) gen.User {
	t.Helper()
	u, err := q.CreateUser(context.Background(), gen.CreateUserParams{ID: db.NewID(), Email: email, Role: gen.UserRoleUser})
	require.NoError(t, err)
	return u
}

func newInstrument(t *testing.T, q *gen.Queries, class gen.AssetClass) gen.Instrument {
	t.Helper()
	row, err := q.CreateInstrument(context.Background(), gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: class})
	require.NoError(t, err)
	return row
}

func newListing(t *testing.T, q *gen.Queries, instrument gen.Instrument, currency string) gen.Listing {
	t.Helper()
	row, err := q.CreateListing(context.Background(), gen.CreateListingParams{ID: db.NewID(), InstrumentID: instrument.ID, Currency: currency})
	require.NoError(t, err)
	return row
}

// newIdentifier names instrument by an ISIN, so a key can associate through
// it.
func newIdentifier(t *testing.T, q *gen.Queries, instrument gen.Instrument, isin string) gen.Identifier {
	t.Helper()
	row, err := q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, Type: types.IdentifierTypeIsin, Value: isin})
	require.NoError(t, err)
	return row
}

// cashListing returns the seed listing holding money in currency, with the
// identifier naming it.
func cashListing(t *testing.T, q *gen.Queries, currency string) (gen.Listing, gen.Identifier) {
	t.Helper()
	ctx := context.Background()
	found, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: currency})
	require.NoError(t, err)
	l, err := q.GetListing(ctx, gen.GetListingParams{InstrumentID: found.Instrument.ID, Currency: currency})
	require.NoError(t, err)
	return l, found.Identifier
}

// TestCurrencySeed checks the currency instruments the migration seeds: one
// per family, named by each code of the family, listed in the family.
func TestCurrencySeed(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()

	usd, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "USD"})
	require.NoError(t, err)
	if usd.Instrument.AssetClass != gen.AssetClassCash {
		t.Errorf("FindIdentifier(currency USD) = %+v, want an instrument of class cash", usd.Instrument)
	}
	line, via := cashListing(t, q, "USD")
	if line.InstrumentID != usd.Instrument.ID {
		t.Errorf("USD listing = %+v, want one of instrument %s", line, usd.Instrument.ID)
	}
	if via.ID != usd.Identifier.ID {
		t.Errorf("USD identifier = %s, want %s", via.ID, usd.Identifier.ID)
	}
	gbp, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBP"})
	require.NoError(t, err)
	gbx, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBX"})
	require.NoError(t, err)
	if gbx.Instrument.ID != gbp.Instrument.ID {
		t.Errorf("GBX names instrument %s, want GBP's %s: one family is one instrument", gbx.Instrument.ID, gbp.Instrument.ID)
	}
	if _, err := q.GetListing(ctx, gen.GetListingParams{InstrumentID: gbp.Instrument.ID, Currency: "GBX"}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetListing(GBP, GBX): err = %v, want ErrNotFound: a listing is keyed by the family", err)
	}
	if _, err := q.GetListing(ctx, gen.GetListingParams{InstrumentID: usd.Instrument.ID, Currency: "GBP"}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetListing(USD, GBP): err = %v, want ErrNotFound until a rate is fetched", err)
	}

	var currencies, families, instruments, listings, identifiers int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM currencies").Scan(&currencies))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM currencies WHERE code = family").Scan(&families))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM instruments WHERE asset_class = 'cash'").Scan(&instruments))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM listings").Scan(&listings))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM identifiers WHERE type = 'currency' AND listing_id IS NULL").Scan(&identifiers))
	if families != currencies-1 || instruments != families || listings != families || identifiers != currencies {
		t.Errorf("seeded %d instruments, %d listings and %d identifiers over %d currencies, want one instrument and listing per family (%d) and an identifier per code", instruments, listings, identifiers, currencies, families)
	}
}

// TestMICSeed checks that every MIC normalises to an operating MIC that is its own.
func TestMICSeed(t *testing.T) {
	tbl, err := mic.Load(context.Background(), newTx(t))
	require.NoError(t, err)
	if len(tbl) != 1078 {
		t.Errorf("seeded %d MICs, want 1078", len(tbl))
	}
	for m, want := range map[string]string{"XNGS": "XNAS", "ARCX": "XNYS", "XLON": "XLON"} {
		if got, ok := tbl.Operating(m); got != want || !ok {
			t.Errorf("Operating(%q) = %q, %v, want %q, true", m, got, ok, want)
		}
	}
	for m, op := range tbl {
		if tbl[op] != op {
			t.Errorf("%s: operating MIC %s has operating MIC %q, want itself", m, op, tbl[op])
		}
	}
}

// TestSetListingPrimary checks that a listing's primary venue is set once.
func TestSetListingPrimary(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	listing := newListing(t, q, newInstrument(t, q, gen.AssetClassStock), "USD")
	if listing.PrimaryMic != nil {
		t.Fatalf("new listing primary venue = %q, want none", *listing.PrimaryMic)
	}
	for _, mic := range []string{"XNAS", "XNYS"} {
		require.NoError(t, q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: listing.ID, PrimaryMic: ptr.To(mic)}))
		rows, err := q.ListListings(ctx, listing.InstrumentID)
		require.NoError(t, err)
		if len(rows) != 1 || rows[0].PrimaryMic == nil || *rows[0].PrimaryMic != "XNAS" {
			t.Errorf("after setting %s: listings = %+v, want the primary venue XNAS", mic, rows)
		}
	}
}

// TestIdentifierGrain checks that a row attaches at its type's grain.
func TestIdentifierGrain(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		typ     types.IdentifierType
		listing bool
	}{
		{name: "instrument type on a listing", typ: types.IdentifierTypeIsin, listing: true},
		{name: "listing type on an instrument", typ: types.IdentifierTypeSedol, listing: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := newTx(t)
			instrument := newInstrument(t, q, gen.AssetClassSecurity)
			arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, Type: tc.typ, Value: "v"}
			if tc.listing {
				listing := newListing(t, q, instrument, "USD")
				arg.ListingID = &listing.ID
			}
			// 23503 is foreign_key_violation.
			if _, err := q.CreateIdentifier(ctx, arg); !sqlstate(err, "23503") {
				t.Errorf("CreateIdentifier(%s, listing=%v): err = %v, want foreign_key_violation", tc.typ, tc.listing, err)
			}
		})
	}
}

// TestIdentifierUniqueness checks that one triple names one subject.
func TestIdentifierUniqueness(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	one := newInstrument(t, q, gen.AssetClassSecurity)
	two := newInstrument(t, q, gen.AssetClassSecurity)
	newIdentifier(t, q, one, "US0378331005")

	// The unique violation aborts the transaction, so it is the last statement.
	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: two.ID, Type: types.IdentifierTypeIsin, Value: "US0378331005"})
	if !db.IsConflict(err) {
		t.Errorf("CreateIdentifier of a triple identifying another instrument: err = %v, want a conflict", err)
	}
}

// TestIdentifierTypes holds the hand written enum equal to the database, in
// value and in order.
func TestIdentifierTypes(t *testing.T) {
	tx := begin(t)
	rows, err := tx.Query(context.Background(),
		`SELECT enumlabel FROM pg_enum
		 JOIN pg_type ON pg_type.oid = pg_enum.enumtypid
		 WHERE pg_type.typname = 'identifier_type'
		 ORDER BY enumsortorder`)
	require.NoError(t, err)
	defer rows.Close()
	var want []types.IdentifierType
	for rows.Next() {
		var label types.IdentifierType
		require.NoError(t, rows.Scan(&label))
		want = append(want, label)
	}
	require.NoError(t, rows.Err())
	if diff := cmp.Diff(want, types.IdentifierTypes); diff != "" {
		t.Errorf("IdentifierTypes mismatch (-database +code):\n%s", diff)
	}
}

// TestListInstrumentsByIdentifiers checks the lookup over several identifiers
// at either grain, with the instrument each names.
func TestListInstrumentsByIdentifiers(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	one := newInstrument(t, q, gen.AssetClassStock)
	two := newInstrument(t, q, gen.AssetClassStock)
	newIdentifier(t, q, one, "GB00B03MLX29")
	listing := newListing(t, q, two, "USD")
	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: two.ID, ListingID: &listing.ID, Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "ACME"})
	require.NoError(t, err)

	rows, err := q.ListInstrumentsByIdentifiers(ctx, gen.ListInstrumentsByIdentifiersParams{
		Types:   []string{"isin", "mic_ticker", "cusip"},
		Domains: []string{"", "XNYS", ""},
		Values:  []string{"GB00B03MLX29", "ACME", "037833100"},
	})
	require.NoError(t, err)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %s %s", r.Instrument.ID, r.Identifier.Type, r.Identifier.Value))
	}
	want := []string{
		fmt.Sprintf("%s isin GB00B03MLX29", one.ID),
		fmt.Sprintf("%s mic_ticker ACME", two.ID),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListInstrumentsByIdentifiers mismatch (-want +got):\n%s", diff)
	}
}

// TestListInstrumentsByIdentifiersIndex checks that the unique index on
// identifiers serves the lookup, which runs under the resolver's locks.
func TestListInstrumentsByIdentifiersIndex(t *testing.T) {
	q, e := explain(t)
	_, err := q.ListInstrumentsByIdentifiers(context.Background(), gen.ListInstrumentsByIdentifiersParams{
		Types: []string{"isin"}, Domains: []string{""}, Values: []string{"GB00B03MLX29"},
	})
	require.NoError(t, err)
	conds := indexConds(t, e.plan, "identifiers_type_domain_value_key")
	if !slices.ContainsFunc(conds, func(c string) bool { return strings.Contains(c, "(type = ") }) {
		t.Errorf("the identifiers unique index is not searched on type:\n%s", e.plan)
	}
}

// TestIsRetryable checks the predicate against the error Postgres raises when
// two transactions take advisory locks in opposite orders. Postgres refuses
// one of them and releases its locks. The other transaction then proceeds and
// can finish before the refusal reaches its client.
func TestIsRetryable(t *testing.T) {
	ctx := context.Background()
	a, b := uuid.NewString(), uuid.NewString()
	open := func() pgx.Tx {
		tx, err := pool.Begin(ctx)
		require.NoError(t, err)
		t.Cleanup(func() {
			if err := tx.Rollback(ctx); err != nil && !errors.Is(err, pgx.ErrTxClosed) {
				t.Errorf("rollback: %v", err)
			}
		})
		return tx
	}
	one, two := open(), open()
	require.NoError(t, gen.New(one).LockIdentifiers(ctx, []string{a}))
	require.NoError(t, gen.New(two).LockIdentifiers(ctx, []string{b}))
	type attempt struct {
		tx  pgx.Tx
		err error
	}
	done := make(chan attempt, 2)
	go func() { done <- attempt{one, gen.New(one).LockIdentifiers(ctx, []string{b})} }()
	go func() { done <- attempt{two, gen.New(two).LockIdentifiers(ctx, []string{a})} }()
	refused, survived := <-done, <-done
	if refused.err == nil {
		refused, survived = survived, refused
	}
	if !db.IsRetryable(refused.err) {
		t.Errorf("IsRetryable(%v) = false, want true", refused.err)
	}
	if survived.err != nil {
		t.Errorf("the other transaction failed: %v", survived.err)
	}
}

// TestListInstruments checks how the listing names and orders every
// instrument, and the listings and identifiers read for one. The seed's cash
// instruments are in every list, so the test's own rows carry names after
// every currency code, and the assertions place rows rather than count them.
func TestListInstruments(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	usd, _ := cashListing(t, q, "USD")
	ident := func(in gen.Instrument, listing *gen.Listing, typ types.IdentifierType, domain, value string) {
		t.Helper()
		var listingID *uuid.UUID
		if listing != nil {
			listingID = &listing.ID
		}
		_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: in.ID, ListingID: listingID, Type: typ, Domain: domain, Value: value})
		require.NoError(t, err)
	}

	// A stock with an ISIN and tickers at two venues, whose primary venue
	// ranks after the other in venue_names and sorts after it by MIC.
	stock := newInstrument(t, q, gen.AssetClassStock)
	stockLine := newListing(t, q, stock, "USD")
	require.NoError(t, q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: stockLine.ID, PrimaryMic: ptr.To("XSWX")}))
	ident(stock, nil, types.IdentifierTypeIsin, "", "ZZ0000000001")
	ident(stock, &stockLine, types.IdentifierTypeMicTicker, "XNYS", "ZZSTOCK")
	ident(stock, &stockLine, types.IdentifierTypeMicTicker, "XSWX", "ZZSTOCKP")
	// A stock with no primary venue, whose tickers differ at a venue ranked
	// first in venue_names and at one sorting first by MIC.
	two := newInstrument(t, q, gen.AssetClassStock)
	twoLine := newListing(t, q, two, "USD")
	ident(two, &twoLine, types.IdentifierTypeMicTicker, "XNAS", "ZZTWO")
	ident(two, &twoLine, types.IdentifierTypeMicTicker, "XNYS", "ZZTWON")
	// An option with an OCC symbol, a built symbol and a ticker.
	option := newInstrument(t, q, gen.AssetClassOption)
	optionLine := newListing(t, q, option, "USD")
	ident(option, nil, types.IdentifierTypeOcc, "", "ZZOPT260116C00150000")
	ident(option, nil, types.IdentifierTypeOption, "", "ZZOPTBUILT")
	ident(option, &optionLine, types.IdentifierTypeMicTicker, "XCBO", "ZZOPTTICK")
	// An instrument nothing names.
	bare := newInstrument(t, q, gen.AssetClassSecurity)

	all, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{Lim: 1000})
	require.NoError(t, err)
	names := map[uuid.UUID]string{}
	for _, r := range all {
		names[r.ID] = r.Name
		if r.ID == stock.ID && (r.NameType != string(types.IdentifierTypeMicTicker) || r.NameDomain != "XSWX") {
			t.Errorf("stock named by %q at %q, want the ticker at XSWX", r.NameType, r.NameDomain)
		}
		if r.ID == bare.ID && r.NameType != "" {
			t.Errorf("unnamed instrument named by %q, want none", r.NameType)
		}
	}
	want := map[uuid.UUID]string{usd.InstrumentID: "USD", stock.ID: "ZZSTOCKP", two.ID: "ZZTWON", option.ID: "ZZOPT260116C00150000", bare.ID: ""}
	for id, name := range want {
		if names[id] != name {
			t.Errorf("instrument %s named %q, want %q", id, names[id], name)
		}
	}
	tail := all[len(all)-3:]
	if tail[0].ID != option.ID || tail[1].ID != stock.ID || tail[2].ID != two.ID {
		t.Errorf("last three = %v, want the option, the stock, then the second stock", tail)
	}
	if all[0].ID != bare.ID || all[0].AssetClass != gen.AssetClassSecurity {
		t.Errorf("first = %+v, want the unnamed instrument", all[0])
	}

	first, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{Lim: 1})
	require.NoError(t, err)
	if len(first) != 1 || first[0].ID != bare.ID {
		t.Errorf("first page = %v, want the unnamed instrument alone", first)
	}
	next, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{AfterID: &option.ID, AfterName: ptr.To("ZZOPT260116C00150000"), Lim: 1})
	require.NoError(t, err)
	if len(next) != 1 || next[0].ID != stock.ID {
		t.Errorf("page after the option = %v, want the stock alone", next)
	}
	// A page continues past a cursor whose row has gone, by its name.
	gone, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{AfterID: ptr.To(db.NewID()), AfterName: ptr.To("ZZOPTZ"), Lim: 1})
	require.NoError(t, err)
	if len(gone) != 1 || gone[0].ID != stock.ID {
		t.Errorf("page after a row that has gone = %v, want the stock alone", gone)
	}
	// The cursor applies after the filters, so a filtered page continues
	// from the cursor too.
	filtered, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{AfterID: &option.ID, AfterName: ptr.To("ZZOPT260116C00150000"), Q: ptr.To("zztwo"), Lim: 10})
	require.NoError(t, err)
	if len(filtered) != 1 || filtered[0].ID != two.ID {
		t.Errorf("filtered page after the option = %v, want the second stock alone", filtered)
	}

	// The query matches any identifier, ignoring case, and the class keeps
	// the classes below it.
	found, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{Q: ptr.To("zz0000"), Lim: 10})
	require.NoError(t, err)
	if len(found) != 1 || found[0].ID != stock.ID {
		t.Errorf("ListInstruments(q=zz0000) = %v, want the stock alone", found)
	}
	equities, err := q.ListInstruments(ctx, gen.ListInstrumentsParams{Q: ptr.To("ZZ"), AssetClass: ptr.To(gen.AssetClassEquity), Lim: 10})
	require.NoError(t, err)
	if len(equities) != 2 || equities[0].ID != stock.ID || equities[1].ID != two.ID {
		t.Errorf("ListInstruments(q=ZZ, equity) = %v, want the two stocks", equities)
	}
	listings, err := q.ListListingsOf(ctx, []uuid.UUID{stock.ID})
	require.NoError(t, err)
	if len(listings) != 1 || listings[0].ID != stockLine.ID {
		t.Errorf("ListListingsOf = %+v, want the stock's listing", listings)
	}
	idents, err := q.ListIdentifiersOf(ctx, []uuid.UUID{stock.ID})
	require.NoError(t, err)
	if len(idents) != 3 || idents[0].ListingID != nil || idents[1].ListingID == nil {
		t.Errorf("ListIdentifiersOf = %+v, want the ISIN then the two tickers", idents)
	}

	// Two keys share the ISIN as their via identifier, so it is returned
	// once. The option has no key, so it contributes nothing.
	h := newHolder(t, q, "via@example.com")
	h.key(t, q, "ZZ stock", &stockLine, &idents[0])
	h.key(t, q, "ZZ stock again", &stockLine, &idents[0])
	via, err := q.ListViaOf(ctx, []uuid.UUID{stock.ID, option.ID})
	require.NoError(t, err)
	if len(via) != 1 || via[0] != idents[0].ID {
		t.Errorf("ListViaOf = %v, want the ISIN alone", via)
	}
}
