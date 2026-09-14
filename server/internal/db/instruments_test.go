//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
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

func newInstrument(t *testing.T, q *gen.Queries, class gen.AssetClass, owner *uuid.UUID) gen.Instrument {
	t.Helper()
	row, err := q.CreateInstrument(context.Background(), gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: class, OwnerID: owner})
	require.NoError(t, err)
	return row
}

func newListing(t *testing.T, q *gen.Queries, instrument gen.Instrument, currency string, owner *uuid.UUID) gen.Listing {
	t.Helper()
	row, err := q.CreateListing(context.Background(), gen.CreateListingParams{ID: db.NewID(), InstrumentID: instrument.ID, Currency: currency, OwnerID: owner})
	require.NoError(t, err)
	return row
}

func ptr[T any](v T) *T { return &v }

// TestCashSeed checks the system owned cash instrument the migration seeds:
// one listing per currency, each named by a currency identifier.
func TestCashSeed(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()

	usd, err := q.GetListingByIdentifier(ctx, gen.GetListingByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: "USD"})
	require.NoError(t, err)
	if usd.AssetClass != gen.AssetClassCash || usd.Listing.Currency != "USD" || usd.Listing.OwnerID != nil {
		t.Errorf("GetListingByIdentifier(currency USD) = %+v, want a system owned USD listing of a cash instrument", usd)
	}
	gbx, err := q.GetListingByIdentifier(ctx, gen.GetListingByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: "GBX"})
	require.NoError(t, err)
	if gbx.Listing.InstrumentID != usd.Listing.InstrumentID || gbx.Listing.ID == usd.Listing.ID {
		t.Errorf("GBX listing = %+v, want a second listing of instrument %s", gbx.Listing, usd.Listing.InstrumentID)
	}

	var currencies, listings, identifiers int
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM currencies").Scan(&currencies))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM listings WHERE instrument_id = $1", usd.Listing.InstrumentID).Scan(&listings))
	require.NoError(t, pool.QueryRow(ctx, "SELECT count(*) FROM identifiers WHERE instrument_id = $1 AND type = 'currency' AND listing_id IS NOT NULL", usd.Listing.InstrumentID).Scan(&identifiers))
	if listings != currencies || identifiers != currencies {
		t.Errorf("cash has %d listings and %d currency identifiers, want %d of each", listings, identifiers, currencies)
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
		{name: "listing type on an instrument", typ: gen.IdentifierTypeBrokerDescription, listing: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			q := newTx(t)
			owner := newUser(t, q, "grain@example.com")
			instrument := newInstrument(t, q, gen.AssetClassSecurity, &owner.ID)
			arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, Type: tc.typ, Domain: ptr("d"), Value: "v", OwnerID: &owner.ID}
			if tc.listing {
				listing := newListing(t, q, instrument, "USD", &owner.ID)
				arg.ListingID = &listing.ID
			}
			// 23503 is foreign_key_violation.
			if _, err := q.CreateIdentifier(ctx, arg); !sqlstate(err, "23503") {
				t.Errorf("CreateIdentifier(%s, listing=%v): err = %v, want foreign_key_violation", tc.typ, tc.listing, err)
			}
		})
	}
}

// TestOwnerChain checks the trigger that keeps a user owned parent's children
// with that user, and the one that keeps an owner where it was set.
func TestOwnerChain(t *testing.T) {
	ctx := context.Background()

	t.Run("system parent takes a user child", func(t *testing.T) {
		q := newTx(t)
		owner := newUser(t, q, "chain@example.com")
		instrument := newInstrument(t, q, gen.AssetClassSecurity, nil)
		listing := newListing(t, q, instrument, "USD", &owner.ID)
		_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, ListingID: &listing.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/qfx"), Value: "ACME CORP", OwnerID: &owner.ID})
		require.NoError(t, err)
	})

	t.Run("user parent refuses a system child", func(t *testing.T) {
		q := newTx(t)
		owner := newUser(t, q, "chain@example.com")
		instrument := newInstrument(t, q, gen.AssetClassSecurity, &owner.ID)
		// 23514 is check_violation.
		if _, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: instrument.ID, Currency: "USD"}); !sqlstate(err, "23514") {
			t.Errorf("CreateListing owned by the system under a user's instrument: err = %v, want check_violation", err)
		}
	})

	t.Run("user parent refuses another user's child", func(t *testing.T) {
		q := newTx(t)
		owner := newUser(t, q, "chain@example.com")
		other := newUser(t, q, "chain-other@example.com")
		instrument := newInstrument(t, q, gen.AssetClassSecurity, &owner.ID)
		if _, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: instrument.ID, Currency: "USD", OwnerID: &other.ID}); !sqlstate(err, "23514") {
			t.Errorf("CreateListing owned by another user under a user's instrument: err = %v, want check_violation", err)
		}
	})

	t.Run("user listing refuses a system identifier", func(t *testing.T) {
		q := newTx(t)
		owner := newUser(t, q, "chain@example.com")
		instrument := newInstrument(t, q, gen.AssetClassSecurity, nil)
		listing := newListing(t, q, instrument, "USD", &owner.ID)
		if _, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, ListingID: &listing.ID, Type: gen.IdentifierTypeSedol, Value: "B0YQ5W0"}); !sqlstate(err, "23514") {
			t.Errorf("CreateIdentifier owned by the system under a user's listing: err = %v, want check_violation", err)
		}
	})

	t.Run("owner cannot change", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		owner := newUser(t, q, "chain@example.com")
		instrument := newInstrument(t, q, gen.AssetClassSecurity, &owner.ID)
		if _, err := tx.Exec(ctx, "UPDATE instruments SET owner_id = NULL WHERE id = $1", instrument.ID); !sqlstate(err, "23514") {
			t.Errorf("UPDATE of owner_id: err = %v, want check_violation", err)
		}
	})
}

// TestIdentifierUniqueness checks that one owner holds one triple once, and
// that the system and a user may each hold it.
func TestIdentifierUniqueness(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	owner := newUser(t, q, "unique@example.com")
	system := newInstrument(t, q, gen.AssetClassSecurity, nil)
	mine := newInstrument(t, q, gen.AssetClassSecurity, &owner.ID)

	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: system.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005"})
	require.NoError(t, err)
	_, err = q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: mine.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005", OwnerID: &owner.ID})
	require.NoError(t, err)

	// The unique violation aborts the transaction, so it is the last statement.
	_, err = q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: mine.ID, Type: gen.IdentifierTypeIsin, Value: "US0378331005", OwnerID: &owner.ID})
	if !db.IsConflict(err) {
		t.Errorf("CreateIdentifier of a triple the owner already holds: err = %v, want a conflict", err)
	}
}

// TestGetListingByIdentifier checks the broker description match is scoped to
// the owner and the domain.
func TestGetListingByIdentifier(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	owner := newUser(t, q, "match@example.com")
	other := newUser(t, q, "match-other@example.com")
	instrument := newInstrument(t, q, gen.AssetClassEquity, &owner.ID)
	listing := newListing(t, q, instrument, "USD", &owner.ID)
	_, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, ListingID: &listing.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/qfx"), Value: "ACME CORP", OwnerID: &owner.ID})
	require.NoError(t, err)

	got, err := q.GetListingByIdentifier(ctx, gen.GetListingByIdentifierParams{OwnerID: &owner.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/qfx"), Value: "ACME CORP"})
	require.NoError(t, err)
	if got.Listing.ID != listing.ID || got.AssetClass != gen.AssetClassEquity {
		t.Errorf("GetListingByIdentifier = %+v, want listing %s of class equity", got, listing.ID)
	}
	for name, arg := range map[string]gen.GetListingByIdentifierParams{
		"another owner": {OwnerID: &other.ID, Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/qfx"), Value: "ACME CORP"},
		"the system":    {Type: gen.IdentifierTypeBrokerDescription, Domain: ptr("ibkr/qfx"), Value: "ACME CORP"},
		"no domain":     {OwnerID: &owner.ID, Type: gen.IdentifierTypeBrokerDescription, Value: "ACME CORP"},
	} {
		if _, err := q.GetListingByIdentifier(ctx, arg); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("GetListingByIdentifier for %s: err = %v, want ErrNotFound", name, err)
		}
	}
}
