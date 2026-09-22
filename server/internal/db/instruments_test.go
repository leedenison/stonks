//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
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

// newIdentifier names instrument by an ISIN, so a key has something to
// associate through.
func newIdentifier(t *testing.T, q *gen.Queries, instrument gen.Instrument, isin string) gen.Identifier {
	t.Helper()
	row, err := q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, Type: gen.IdentifierTypeIsin, Value: isin})
	require.NoError(t, err)
	return row
}

// cashListing returns the listing money in currency is held against, the
// currency instrument's listing in itself, and the identifier naming it.
func cashListing(t *testing.T, q *gen.Queries, currency string) (gen.Listing, gen.Identifier) {
	t.Helper()
	ctx := context.Background()
	found, err := q.GetInstrumentByIdentifier(ctx, gen.GetInstrumentByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: currency})
	require.NoError(t, err)
	l, err := q.GetListing(ctx, gen.GetListingParams{InstrumentID: found.Instrument.ID, Currency: currency})
	require.NoError(t, err)
	return l, found.Identifier
}

// TestCurrencySeed checks the currency instruments the migration seeds: one
// per currency, named by its code, listed in itself.
func TestCurrencySeed(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()

	usd, err := q.GetInstrumentByIdentifier(ctx, gen.GetInstrumentByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: "USD"})
	require.NoError(t, err)
	if usd.Instrument.AssetClass != gen.AssetClassCash {
		t.Errorf("GetInstrumentByIdentifier(currency USD) = %+v, want an instrument of class cash", usd.Instrument)
	}
	line, via := cashListing(t, q, "USD")
	if line.InstrumentID != usd.Instrument.ID {
		t.Errorf("USD listing = %+v, want one of instrument %s", line, usd.Instrument.ID)
	}
	if via.ID != usd.Identifier.ID {
		t.Errorf("USD identifier = %s, want %s", via.ID, usd.Identifier.ID)
	}
	if gbx, _ := cashListing(t, q, "GBX"); gbx.InstrumentID == usd.Instrument.ID {
		t.Errorf("GBX listing = %+v, want one of an instrument other than USD's", gbx)
	}
	if _, err := q.GetListing(ctx, gen.GetListingParams{InstrumentID: usd.Instrument.ID, Currency: "GBP"}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetListing(USD, GBP): err = %v, want ErrNotFound until a rate is fetched", err)
	}

	var currencies, instruments, listings, identifiers int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM currencies").Scan(&currencies))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM instruments WHERE asset_class = 'cash'").Scan(&instruments))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM listings").Scan(&listings))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM identifiers WHERE type = 'currency' AND listing_id IS NULL").Scan(&identifiers))
	if instruments != currencies || listings != currencies || identifiers != currencies {
		t.Errorf("seeded %d instruments, %d listings and %d identifiers, want %d of each", instruments, listings, identifiers, currencies)
	}
}

// TestIdentifierGrain checks that a row attaches at its type's grain.
func TestIdentifierGrain(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name    string
		typ     gen.IdentifierType
		listing bool
	}{
		{name: "instrument type on a listing", typ: gen.IdentifierTypeIsin, listing: true},
		{name: "listing type on an instrument", typ: gen.IdentifierTypeSedol, listing: false},
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
	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: two.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005"})
	if !db.IsConflict(err) {
		t.Errorf("CreateIdentifier of a triple another instrument holds: err = %v, want a conflict", err)
	}
}
