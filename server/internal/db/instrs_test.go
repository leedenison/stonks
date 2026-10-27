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
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
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

func newInstr(t *testing.T, q *gen.Queries, class types.AssetClass) gen.Instr {
	t.Helper()
	row, err := q.CreateInstr(context.Background(), gen.CreateInstrParams{ID: db.NewID(), AssetClass: class})
	require.NoError(t, err)
	return row
}

func newListing(t *testing.T, q *gen.Queries, instr gen.Instr, currency string) gen.Listing {
	t.Helper()
	row, err := q.CreateListing(context.Background(), gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: currency})
	require.NoError(t, err)
	return row
}

// newIdentifier names instrument by an ISIN, so a key can associate through
// it.
func newIdentifier(t *testing.T, q *gen.Queries, instr gen.Instr, isin string) gen.Identifier {
	t.Helper()
	row, err := q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, Type: types.IdentifierTypeIsin, Value: isin})
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
	l, err := q.GetListing(ctx, gen.GetListingParams{InstrID: found.Instr.ID, Currency: currency})
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
	if usd.Instr.AssetClass != types.AssetClassCash {
		t.Errorf("FindIdentifier(currency USD) = %+v, want an instrument of class cash", usd.Instr)
	}
	line, via := cashListing(t, q, "USD")
	if line.InstrID != usd.Instr.ID {
		t.Errorf("USD listing = %+v, want one of instrument %s", line, usd.Instr.ID)
	}
	if via.ID != usd.Identifier.ID {
		t.Errorf("USD identifier = %s, want %s", via.ID, usd.Identifier.ID)
	}
	gbp, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBP"})
	require.NoError(t, err)
	gbx, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "GBX"})
	require.NoError(t, err)
	if gbx.Instr.ID != gbp.Instr.ID {
		t.Errorf("GBX names instrument %s, want GBP's %s: one family is one instrument", gbx.Instr.ID, gbp.Instr.ID)
	}
	if _, err := q.GetListing(ctx, gen.GetListingParams{InstrID: gbp.Instr.ID, Currency: "GBX"}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetListing(GBP, GBX): err = %v, want ErrNotFound: a listing is keyed by the family", err)
	}
	if _, err := q.GetListing(ctx, gen.GetListingParams{InstrID: usd.Instr.ID, Currency: "GBP"}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetListing(USD, GBP): err = %v, want ErrNotFound until a rate is fetched", err)
	}

	var currencies, families, instrs, listings, identifiers int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM currencies").Scan(&currencies))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM currencies WHERE code = family").Scan(&families))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM instrs WHERE asset_class = 'cash'").Scan(&instrs))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM listings").Scan(&listings))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM identifiers WHERE type = 'currency' AND listing_id IS NULL").Scan(&identifiers))
	if families != currencies-1 || instrs != families || listings != families || identifiers != currencies {
		t.Errorf("seeded %d instruments, %d listings and %d identifiers over %d currencies, want one instrument and listing per family (%d) and an identifier per code", instrs, listings, identifiers, currencies, families)
	}
}

// TestMICSeed checks that every MIC normalises to an operating MIC that is its own,
// and shares the market of its operating MIC.
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
	for m, want := range map[string]string{"XNGS": "US", "ARCX": "US", "BATY": "US", "EDGX": "US", "XLON": "XLON", "OTCM": "OTCM"} {
		if got, ok := tbl.Market(m); got != want || !ok {
			t.Errorf("Market(%q) = %q, %v, want %q, true", m, got, ok, want)
		}
	}
	for m, e := range tbl {
		if op := tbl[e.Operating]; op.Operating != e.Operating || op.Market != e.Market {
			t.Errorf("%s: operating MIC %s maps to %+v, want itself in market %s", m, e.Operating, op, e.Market)
		}
	}
}

// TestSetListingPrimary checks that a listing's primary venue is set once.
func TestSetListingPrimary(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	listing := newListing(t, q, newInstr(t, q, types.AssetClassStock), "USD")
	if listing.PrimaryMic != nil {
		t.Fatalf("new listing primary venue = %q, want none", *listing.PrimaryMic)
	}
	for _, mic := range []string{"XNAS", "XNYS"} {
		require.NoError(t, q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: listing.ID, PrimaryMic: ptr.To(mic)}))
		rows, err := q.ListListings(ctx, listing.InstrID)
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
			instr := newInstr(t, q, types.AssetClassSecurity)
			arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, Type: tc.typ, Value: "v"}
			if tc.listing {
				listing := newListing(t, q, instr, "USD")
				arg.ListingID = &listing.ID
			}
			// 23503 is foreign_key_violation.
			if _, err := q.CreateIdentifier(ctx, arg); !sqlstate(err, "23503") {
				t.Errorf("CreateIdentifier(%s, listing=%v): err = %v, want foreign_key_violation", tc.typ, tc.listing, err)
			}
		})
	}
}

// TestIdentifierNoTicker checks that identifiers refuses a ticker of either
// type, since a ticker is never identity.
func TestIdentifierNoTicker(t *testing.T) {
	ctx := context.Background()
	for _, typ := range []types.IdentifierType{types.IdentifierTypeMicTicker, types.IdentifierTypeDatasourceTicker} {
		t.Run(string(typ), func(t *testing.T) {
			q := newTx(t)
			instr := newInstr(t, q, types.AssetClassSecurity)
			listing := newListing(t, q, instr, "USD")
			arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, ListingID: &listing.ID, Type: typ, Domain: "XNAS", Value: "META"}
			if _, err := q.CreateIdentifier(ctx, arg); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateIdentifier(%s): err = %v, want check_violation", typ, err)
			}
		})
	}
}

// TestIdentifierUniqueness checks that one triple identifies one instrument or
// listing.
func TestIdentifierUniqueness(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	one := newInstr(t, q, types.AssetClassSecurity)
	two := newInstr(t, q, types.AssetClassSecurity)
	newIdentifier(t, q, one, "US0378331005")

	// The unique violation aborts the transaction, so it is the last statement.
	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: two.ID, Type: types.IdentifierTypeIsin, Value: "US0378331005"})
	if !db.IsConflict(err) {
		t.Errorf("CreateIdentifier of a triple identifying another instrument: err = %v, want a conflict", err)
	}
}

// TestIdentifierTypes holds the hand written enum equal to the database, in
// value and in order.
func TestIdentifierTypes(t *testing.T) {
	if diff := cmp.Diff(labels[types.IdentifierType](t, "identifier_type"), types.IdentifierTypes); diff != "" {
		t.Errorf("IdentifierTypes mismatch (-database +code):\n%s", diff)
	}
}

// TestAssetClasses holds the hand-written enum equal to the database, in
// value and in order.
func TestAssetClasses(t *testing.T) {
	if diff := cmp.Diff(labels[types.AssetClass](t, "asset_class"), types.AssetClasses); diff != "" {
		t.Errorf("AssetClasses mismatch (-database +code):\n%s", diff)
	}
}

// TestIdentifierDomains holds the hand-written enum equal to the database, in
// value and in order.
func TestIdentifierDomains(t *testing.T) {
	if diff := cmp.Diff(labels[types.IdentifierDomain](t, "identifier_domain"), types.IdentifierDomains); diff != "" {
		t.Errorf("IdentifierDomains mismatch (-database +code):\n%s", diff)
	}
}

// TestIdentifierGrains holds the hand-written enum equal to the database, in
// value and in order.
func TestIdentifierGrains(t *testing.T) {
	if diff := cmp.Diff(labels[types.IdentifierGrain](t, "identifier_grain"), types.IdentifierGrains); diff != "" {
		t.Errorf("IdentifierGrains mismatch (-database +code):\n%s", diff)
	}
}

// TestIdentifierReassignments holds the hand-written enum equal to the
// database, in value and in order.
func TestIdentifierReassignments(t *testing.T) {
	if diff := cmp.Diff(labels[types.IdentifierReassignment](t, "identifier_reassignment"), types.IdentifierReassignments); diff != "" {
		t.Errorf("IdentifierReassignments mismatch (-database +code):\n%s", diff)
	}
}

// TestTraits holds the traits equal to the identifier_type_traits table.
func TestTraits(t *testing.T) {
	rows, err := newTx(t).ListIdentifierTypeTraits(context.Background())
	require.NoError(t, err)
	got := map[types.IdentifierType]types.Trait{}
	for _, r := range rows {
		got[r.Type] = types.Trait{Domain: r.Domain, Grain: r.Grain, Reassignment: r.Reassignment, Exclusive: r.Exclusive, NameRank: r.NameRank}
	}
	want := map[types.IdentifierType]types.Trait{}
	for _, typ := range types.IdentifierTypes {
		want[typ] = typ.Trait()
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("traits mismatch (-code +table):\n%s", diff)
	}
}

// TestTree holds the class tree equal to the asset_class_tree table.
func TestTree(t *testing.T) {
	rows, err := newTx(t).ListAssetClassTree(context.Background())
	require.NoError(t, err)
	got := map[types.AssetClass]types.AssetClass{}
	for _, r := range rows {
		var parent types.AssetClass
		if r.Parent != nil {
			parent = *r.Parent
		}
		got[r.Class] = parent
	}
	want := map[types.AssetClass]types.AssetClass{}
	for _, c := range types.AssetClasses {
		want[c] = c.Parent()
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("asset class tree mismatch (-code +table):\n%s", diff)
	}
}

// labels reads the values of the database enum typ in declared order.
func labels[T ~string](t *testing.T, typ string) []T {
	t.Helper()
	rows, err := begin(t).Query(context.Background(),
		`SELECT enumlabel FROM pg_enum
		 JOIN pg_type ON pg_type.oid = pg_enum.enumtypid
		 WHERE pg_type.typname = $1
		 ORDER BY enumsortorder`, typ)
	require.NoError(t, err)
	defer rows.Close()
	var out []T
	for rows.Next() {
		var label string
		require.NoError(t, rows.Scan(&label))
		out = append(out, T(label))
	}
	require.NoError(t, rows.Err())
	return out
}

// TestListInstrsByIdentifiers checks the lookup over several identifiers
// at either grain, with the instrument each names.
func TestListInstrsByIdentifiers(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	one := newInstr(t, q, types.AssetClassStock)
	two := newInstr(t, q, types.AssetClassStock)
	newIdentifier(t, q, one, "GB00B03MLX29")
	listing := newListing(t, q, two, "USD")
	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: two.ID, ListingID: &listing.ID, Type: types.IdentifierTypeSedol, Value: "B03MLX2"})
	require.NoError(t, err)

	rows, err := q.ListInstrsByIdentifiers(ctx, gen.ListInstrsByIdentifiersParams{
		Types:   []string{"isin", "sedol", "cusip"},
		Domains: []string{"", "", ""},
		Values:  []string{"GB00B03MLX29", "B03MLX2", "037833100"},
	})
	require.NoError(t, err)
	var got []string
	for _, r := range rows {
		got = append(got, fmt.Sprintf("%s %s %s", r.Instr.ID, r.Identifier.Type, r.Identifier.Value))
	}
	want := []string{
		fmt.Sprintf("%s isin GB00B03MLX29", one.ID),
		fmt.Sprintf("%s sedol B03MLX2", two.ID),
	}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListInstrsByIdentifiers mismatch (-want +got):\n%s", diff)
	}
}

// TestListInstrsByIdentifiersIndex checks that the unique index on
// identifiers serves the lookup, which runs under the resolver's locks.
func TestListInstrsByIdentifiersIndex(t *testing.T) {
	q, e := explain(t)
	_, err := q.ListInstrsByIdentifiers(context.Background(), gen.ListInstrsByIdentifiersParams{
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
	require.NoError(t, gen.New(one).LockAdvisory(ctx, []string{a}))
	require.NoError(t, gen.New(two).LockAdvisory(ctx, []string{b}))
	type attempt struct {
		tx  pgx.Tx
		err error
	}
	done := make(chan attempt, 2)
	go func() { done <- attempt{one, gen.New(one).LockAdvisory(ctx, []string{b})} }()
	go func() { done <- attempt{two, gen.New(two).LockAdvisory(ctx, []string{a})} }()
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

// TestListInstrs checks how the listing names and orders every
// instrument, and the listings and identifiers read for one. The tickers that
// name the instruments are rows of resolved_tickers alone, with no MIC_TICKER
// identifier. The seed's cash instruments are in every list, so the test's
// own rows carry names after every currency code, and the assertions place
// rows rather than count them.
func TestListInstrs(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	usd, _ := cashListing(t, q, "USD")
	ident := func(in gen.Instr, listing *gen.Listing, typ types.IdentifierType, domain, value string) {
		t.Helper()
		var listingID *uuid.UUID
		if listing != nil {
			listingID = &listing.ID
		}
		_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: in.ID, ListingID: listingID, Type: typ, Domain: domain, Value: value})
		require.NoError(t, err)
	}

	// A stock with an ISIN and tickers at two venues, whose primary venue
	// ranks after the other in venue_names and sorts after it by MIC.
	stock := newInstr(t, q, types.AssetClassStock)
	stockLine := newListing(t, q, stock, "USD")
	require.NoError(t, q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: stockLine.ID, PrimaryMic: ptr.To("XSWX")}))
	ident(stock, nil, types.IdentifierTypeIsin, "", "ZZ0000000001")
	ident(stock, &stockLine, types.IdentifierTypeSedol, "", "ZZSEDOL")
	resolves(t, q, stock, "US", "ZZSTOCK", "XNYS", "USD", false)
	resolves(t, q, stock, "XSWX", "ZZSTOCKP", "XSWX", "USD", false)
	// A stock with no primary venue, whose tickers differ at a venue ranked
	// first in venue_names and at one sorting first by MIC.
	two := newInstr(t, q, types.AssetClassStock)
	newListing(t, q, two, "USD")
	resolves(t, q, two, "US", "ZZTWO", "XNAS", "USD", false)
	resolves(t, q, two, "US", "ZZTWON", "XNYS", "USD", false)
	// An option with an OCC symbol, a built symbol and a ticker.
	option := newInstr(t, q, types.AssetClassOption)
	newListing(t, q, option, "USD")
	ident(option, nil, types.IdentifierTypeOcc, "", "ZZOPT260116C00150000")
	ident(option, nil, types.IdentifierTypeOption, "", "ZZOPTBUILT")
	resolves(t, q, option, "XCBO", "ZZOPTTICK", "XCBO", "USD", false)
	// An instrument nothing names.
	bare := newInstr(t, q, types.AssetClassSecurity)

	all, err := q.ListInstrs(ctx, gen.ListInstrsParams{Lim: 1000})
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
	want := map[uuid.UUID]string{usd.InstrID: "USD", stock.ID: "ZZSTOCKP", two.ID: "ZZTWON", option.ID: "ZZOPT260116C00150000", bare.ID: ""}
	for id, name := range want {
		if names[id] != name {
			t.Errorf("instrument %s named %q, want %q", id, names[id], name)
		}
	}
	// instr_names holds the same names, with a row only for each named
	// instrument.
	rows, err := q.ListInstrNames(ctx, []uuid.UUID{usd.InstrID, stock.ID, two.ID, option.ID, bare.ID})
	require.NoError(t, err)
	viewed := map[uuid.UUID]string{}
	for _, r := range rows {
		viewed[r.InstrID] = r.Value
	}
	delete(want, bare.ID)
	if diff := cmp.Diff(want, viewed); diff != "" {
		t.Errorf("ListInstrNames mismatch (-want +got):\n%s", diff)
	}
	tail := all[len(all)-3:]
	if tail[0].ID != option.ID || tail[1].ID != stock.ID || tail[2].ID != two.ID {
		t.Errorf("last three = %v, want the option, the stock, then the second stock", tail)
	}
	if all[0].ID != bare.ID || all[0].AssetClass != types.AssetClassSecurity {
		t.Errorf("first = %+v, want the unnamed instrument", all[0])
	}

	first, err := q.ListInstrs(ctx, gen.ListInstrsParams{Lim: 1})
	require.NoError(t, err)
	if len(first) != 1 || first[0].ID != bare.ID {
		t.Errorf("first page = %v, want the unnamed instrument alone", first)
	}
	next, err := q.ListInstrs(ctx, gen.ListInstrsParams{AfterID: &option.ID, AfterName: ptr.To("ZZOPT260116C00150000"), Lim: 1})
	require.NoError(t, err)
	if len(next) != 1 || next[0].ID != stock.ID {
		t.Errorf("page after the option = %v, want the stock alone", next)
	}
	// A page continues past a cursor whose row has gone, by its name.
	gone, err := q.ListInstrs(ctx, gen.ListInstrsParams{AfterID: ptr.To(db.NewID()), AfterName: ptr.To("ZZOPTZ"), Lim: 1})
	require.NoError(t, err)
	if len(gone) != 1 || gone[0].ID != stock.ID {
		t.Errorf("page after a row that has gone = %v, want the stock alone", gone)
	}
	// The cursor applies after the filters, so a filtered page continues
	// from the cursor too.
	filtered, err := q.ListInstrs(ctx, gen.ListInstrsParams{AfterID: &option.ID, AfterName: ptr.To("ZZOPT260116C00150000"), Q: ptr.To("zztwo"), Lim: 10})
	require.NoError(t, err)
	if len(filtered) != 1 || filtered[0].ID != two.ID {
		t.Errorf("filtered page after the option = %v, want the second stock alone", filtered)
	}

	// The query matches any identifier, ignoring case, and the class keeps
	// the classes below it.
	found, err := q.ListInstrs(ctx, gen.ListInstrsParams{Q: ptr.To("zz0000"), Lim: 10})
	require.NoError(t, err)
	if len(found) != 1 || found[0].ID != stock.ID {
		t.Errorf("ListInstrs(q=zz0000) = %v, want the stock alone", found)
	}
	equities, err := q.ListInstrs(ctx, gen.ListInstrsParams{Q: ptr.To("ZZ"), AssetClass: ptr.To(types.AssetClassEquity), Lim: 10})
	require.NoError(t, err)
	if len(equities) != 2 || equities[0].ID != stock.ID || equities[1].ID != two.ID {
		t.Errorf("ListInstrs(q=ZZ, equity) = %v, want the two stocks", equities)
	}
	listings, err := q.ListListingsOf(ctx, []uuid.UUID{stock.ID})
	require.NoError(t, err)
	if len(listings) != 1 || listings[0].ID != stockLine.ID {
		t.Errorf("ListListingsOf = %+v, want the stock's listing", listings)
	}
	idents, err := q.ListIdentifiersOf(ctx, []uuid.UUID{stock.ID})
	require.NoError(t, err)
	if len(idents) != 2 || idents[0].Type != types.IdentifierTypeIsin || idents[1].Type != types.IdentifierTypeSedol || idents[1].ListingID == nil {
		t.Errorf("ListIdentifiersOf = %+v, want the ISIN then the SEDOL on the listing, by the rank of the type as a name", idents)
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
