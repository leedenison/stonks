//go:build dbtest

package db_test

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/group"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

func date(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// upsertKey states a key in statement the way an ingestion does and returns
// the row, which may be an earlier statement's.
func upsertKey(t *testing.T, q *gen.Queries, statement gen.Statement, arg gen.UpsertStatedKeyParams) gen.StatedKey {
	t.Helper()
	arg.ID, arg.UserID = db.NewID(), statement.UserID
	key, err := q.UpsertStatedKey(context.Background(), arg)
	require.NoError(t, err)
	link(t, q, statement, key)
	return key
}

// link records that statement states key, as an ingestion does.
func link(t *testing.T, q *gen.Queries, statement gen.Statement, key gen.StatedKey) {
	t.Helper()
	_, err := q.CreateStatementKeys(context.Background(), []gen.CreateStatementKeysParams{{StatementID: statement.ID, UserID: statement.UserID, StatedKeyID: key.ID}})
	require.NoError(t, err)
}

// TestStatedKeys checks that a user holds one row per distinct key, whatever
// statements state it, that each statement lists the keys it links to, that
// an associated key upserts to its own row, and that the identifiers column
// round-trips through its Go type.
func TestStatedKeys(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	user, other := newUser(t, q, "keys@example.com"), newUser(t, q, "keys-other@example.com")
	statement, later := newStatement(t, q, user), newStatement(t, q, user)
	cash := types.AssetClassCash
	key := func(currency *string) gen.UpsertStatedKeyParams {
		return gen.UpsertStatedKeyParams{AssetClass: &cash, Currency: currency, Identifiers: []types.Identifier{{Type: "system", Value: "cash"}}}
	}

	created := upsertKey(t, q, statement, key(ptr.To("USD")))
	if created.AssetClass == nil || *created.AssetClass != types.AssetClassCash {
		t.Errorf("UpsertStatedKey = %+v, want asset class cash", created)
	}
	listed, err := q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	if diff := cmp.Diff([]gen.StatedKey{created}, listed); diff != "" {
		t.Errorf("ListStatedKeys mismatch (-want +got):\n%s", diff)
	}

	if again := upsertKey(t, q, later, key(ptr.To("USD"))); again.ID != created.ID {
		t.Errorf("the same key under another statement = %s, want the row %s", again.ID, created.ID)
	}
	for _, st := range []gen.Statement{statement, later} {
		listed, err := q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: st.ID, UserID: user.ID})
		require.NoError(t, err)
		if len(listed) != 1 || listed[0].ID != created.ID {
			t.Errorf("ListStatedKeys(%s) = %+v, want the one key", st.ID, listed)
		}
	}
	if theirs := upsertKey(t, q, newStatement(t, q, other), key(ptr.To("USD"))); theirs.ID == created.ID {
		t.Errorf("the same key of another user = %s, want a row of their own", theirs.ID)
	}
	// A null currency is one value, so a restatement shares the row.
	bare := upsertKey(t, q, statement, key(nil))
	if bare.ID == created.ID || upsertKey(t, q, later, key(nil)).ID != bare.ID {
		t.Errorf("a key with no currency = %s, want a row apart from %s that a restatement shares", bare.ID, created.ID)
	}

	usd, via := cashListing(t, q, "USD")
	arg := gen.SetStatedKeyAssociationParams{ID: created.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterUser}
	_, err = q.SetStatedKeyAssociation(ctx, arg)
	require.NoError(t, err)
	restated := upsertKey(t, q, newStatement(t, q, user), key(ptr.To("USD")))
	if restated.ID != created.ID || restated.InstrumentID == nil || *restated.InstrumentID != usd.InstrumentID || restated.Arbiter == nil || *restated.Arbiter != gen.ArbiterUser {
		t.Errorf("an associated key upserted again = %+v, want its row with the association", restated)
	}
}

// TestSweepStatedKeys checks that the sweep deletes a key that lacks both a
// transaction and a statement link, and only such a key. The key's
// resolution rows and findings go with it. The fetch key that asked for it,
// its assertions and the instrument it sourced stay. A member survives the
// sweep of its root after a regroup.
func TestSweepStatedKeys(t *testing.T) {
	ctx := context.Background()
	t.Run("deletes only a key with no transaction and no link", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		user := newUser(t, q, "sweep@example.com")
		statement, other := newStatement(t, q, user), newStatement(t, q, user)
		run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: &user.ID})
		require.NoError(t, err)
		linked := newStatedKey(t, q, user, statement)
		named := newStatedKey(t, q, user, statement)
		names(t, q, statement, named)
		orphan := newStatedKey(t, q, user, other)
		resolution := newResolution(t, q, user, statement)
		resolved(t, q, resolution, orphan, gen.ResolutionOutcomeUnavailable)
		require.NoError(t, q.CreateFinding(ctx, gen.CreateFindingParams{ID: db.NewID(), RunID: resolution.ID, Kind: gen.FindingKindContradiction, StatedKeyID: &orphan.ID, Detail: ptr.To("two")}))
		ds := newDatasource(t, q, "sweep", 10)
		fetchKey := servedKey(t, q, newFetch(t, q, user, run, ds), orphan, "GB00B03MLX29")
		require.NoError(t, q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{ID: db.NewID(), FetchKeyID: &fetchKey, Type: types.IdentifierTypeIsin, Value: "GB00B03MLX29", AssertedOn: date(2026, 3, 2)}))
		instrument, err := q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock, FetchKeyID: &fetchKey})
		require.NoError(t, err)

		// Deleting the other statement cascades the orphan's only link.
		_, err = tx.Exec(ctx, "DELETE FROM statements WHERE id = $1", other.ID)
		require.NoError(t, err)
		n, err := q.SweepStatedKeys(ctx, user.ID)
		require.NoError(t, err)
		if n != 1 {
			t.Errorf("SweepStatedKeys = %d rows, want 1: the orphan", n)
		}
		for _, k := range []gen.StatedKey{linked, named} {
			if _, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: k.ID, UserID: user.ID}); err != nil {
				t.Errorf("key %s after the sweep: err = %v, want it kept", k.ID, err)
			}
		}
		if _, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: orphan.ID, UserID: user.ID}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("the orphan after the sweep: err = %v, want ErrNotFound", err)
		}
		rows, err := q.ListResolutionKeys(ctx, gen.ListResolutionKeysParams{RunID: resolution.ID, UserID: user.ID})
		require.NoError(t, err)
		findings, err := q.ListRunFindings(ctx, []uuid.UUID{resolution.ID})
		require.NoError(t, err)
		if len(rows) != 0 || len(findings) != 0 {
			t.Errorf("resolution rows %+v and findings %+v after the sweep, want none", rows, findings)
		}
		var statedKey, provenance *uuid.UUID
		var assertions int
		require.NoError(t, tx.QueryRow(ctx, "SELECT stated_key_id FROM fetch_keys WHERE id = $1", fetchKey).Scan(&statedKey))
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM identifier_assertions WHERE fetch_key_id = $1", fetchKey).Scan(&assertions))
		require.NoError(t, tx.QueryRow(ctx, "SELECT fetch_key_id FROM instruments WHERE id = $1", instrument.ID).Scan(&provenance))
		if statedKey != nil || assertions != 1 || provenance == nil || *provenance != fetchKey {
			t.Errorf("fetch key after the sweep: stated key %v, %d assertions, instrument provenance %v; want no stated key, 1 assertion and the fetch key as provenance", statedKey, assertions, provenance)
		}
	})

	t.Run("a member survives the sweep of its root after a regroup", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		user := newUser(t, q, "sweep-group@example.com")
		statement, other := newStatement(t, q, user), newStatement(t, q, user)
		shared := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}
		root := upsertKey(t, q, other, gen.UpsertStatedKeyParams{Identifiers: []types.Identifier{shared}})
		member := upsertKey(t, q, statement, gen.UpsertStatedKeyParams{Currency: ptr.To("USD"), Identifiers: []types.Identifier{shared}})
		names(t, q, other, root)
		names(t, q, statement, member)
		require.NoError(t, q.LockUserKeys(ctx, user.ID))
		require.NoError(t, group.Regroup(ctx, q, user.ID))
		m, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: member.ID, UserID: user.ID})
		require.NoError(t, err)
		if m.GroupID == nil || *m.GroupID != root.ID {
			t.Fatalf("member = %+v, want it in the root's group %s", m, root.ID)
		}

		// The root's statement goes, with its transaction and link.
		_, err = tx.Exec(ctx, "DELETE FROM statements WHERE id = $1", other.ID)
		require.NoError(t, err)
		require.NoError(t, group.Regroup(ctx, q, user.ID))
		n, err := q.SweepStatedKeys(ctx, user.ID)
		require.NoError(t, err)
		if n != 1 {
			t.Errorf("SweepStatedKeys = %d rows, want 1: the root", n)
		}
		m, err = q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: member.ID, UserID: user.ID})
		require.NoError(t, err)
		if m.GroupID == nil || *m.GroupID != member.ID {
			t.Errorf("member after the sweep = %+v, want it kept in a group of its own", m)
		}
	})
}

// TestTransactions checks the decimal and date round trip, and the half-open
// period that keys the delete.
func TestTransactions(t *testing.T) {
	q := newTx(t)
	ctx := context.Background()
	user := newUser(t, q, "txs@example.com")
	statement := newStatement(t, q, user)
	key := upsertKey(t, q, statement, gen.UpsertStatedKeyParams{Identifiers: []types.Identifier{{Type: "system", Value: "cash"}}})
	create := func(broker gen.Broker, order time.Time, quantity string) gen.Transaction {
		t.Helper()
		row, err := q.CreateTransaction(ctx, gen.CreateTransactionParams{
			ID: db.NewID(), UserID: user.ID, Broker: broker, StatementID: statement.ID, StatedKeyID: key.ID,
			OrderDate: order, SettlementDate: order.AddDate(0, 0, 2), AsAt: order,
			Quantity: decimal.RequireFromString(quantity),
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

// TestStatedKeyAssociation checks that a key names an instrument and the
// identifier carrying that name together, and that a listing it names is one
// of that instrument.
func TestStatedKeyAssociation(t *testing.T) {
	ctx := context.Background()
	t.Run("clears the group of a key that becomes associated", func(t *testing.T) {
		q := newTx(t)
		user := newUser(t, q, "grouped@example.com")
		statement := newStatement(t, q, user)
		one, two := newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement)
		require.NoError(t, q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: user.ID, Ids: []uuid.UUID{one.ID, two.ID}, GroupIds: []uuid.UUID{one.ID, one.ID}}))
		usd, via := cashListing(t, q, "USD")
		arg := gen.SetStatedKeyAssociationParams{ID: two.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterDatasource}
		n, err := q.SetStatedKeyAssociation(ctx, arg)
		require.NoError(t, err)
		if n != 1 {
			t.Errorf("SetStatedKeyAssociation changed %d rows, want 1", n)
		}
		keys, err := q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement.ID, UserID: user.ID})
		require.NoError(t, err)
		if len(keys) != 2 || keys[0].GroupID == nil || keys[1].GroupID != nil || keys[1].InstrumentID == nil {
			t.Errorf("keys after association = %+v, want the first still grouped and the second associated with no group", keys)
		}
	})

	t.Run("leaves an associated key out of a group", func(t *testing.T) {
		q := newTx(t)
		user := newUser(t, q, "regrouped@example.com")
		statement := newStatement(t, q, user)
		one, two := newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement)
		usd, via := cashListing(t, q, "USD")
		arg := gen.SetStatedKeyAssociationParams{ID: two.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterDatasource}
		_, err := q.SetStatedKeyAssociation(ctx, arg)
		require.NoError(t, err)
		require.NoError(t, q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: user.ID, Ids: []uuid.UUID{one.ID, two.ID}, GroupIds: []uuid.UUID{one.ID, one.ID}}))
		keys, err := q.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement.ID, UserID: user.ID})
		require.NoError(t, err)
		if len(keys) != 2 || keys[0].GroupID == nil || keys[1].GroupID != nil || keys[1].InstrumentID == nil {
			t.Errorf("keys after regrouping = %+v, want the first grouped and the second associated with no group", keys)
		}
	})

	base := newTx(t)
	usd, via := cashListing(t, base, "USD")
	other := newInstrument(t, base, types.AssetClassEquity)
	otherLine := newListing(t, base, other, "USD")

	tests := []struct {
		name string
		arg  gen.SetStatedKeyAssociationParams
	}{
		{name: "an instrument without the identifier it is named through", arg: gen.SetStatedKeyAssociationParams{InstrumentID: &usd.InstrumentID, Arbiter: gen.ArbiterStated}},
		{name: "an identifier without an instrument", arg: gen.SetStatedKeyAssociationParams{ViaID: &via.ID, Arbiter: gen.ArbiterStated}},
		{name: "a listing without an instrument", arg: gen.SetStatedKeyAssociationParams{ListingID: &usd.ID, Arbiter: gen.ArbiterStated}},
		{name: "a listing of another instrument", arg: gen.SetStatedKeyAssociationParams{InstrumentID: &usd.InstrumentID, ListingID: &otherLine.ID, ViaID: &via.ID, Arbiter: gen.ArbiterStated}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			user := newUser(t, q, "assoc-"+uuid.NewString()+"@example.com")
			statement := newStatement(t, q, user)
			key := upsertKey(t, q, statement, gen.UpsertStatedKeyParams{Identifiers: []types.Identifier{}})
			arg := tc.arg
			arg.ID, arg.UserID = key.ID, user.ID
			if _, err := q.SetStatedKeyAssociation(ctx, arg); err == nil {
				t.Errorf("SetStatedKeyAssociation with %s: err = nil, want a constraint violation", tc.name)
			}
		})
	}

	t.Run("an association the user arbitrated stands", func(t *testing.T) {
		q := newTx(t)
		user := newUser(t, q, "arbitrated@example.com")
		statement := newStatement(t, q, user)
		chosen := newStatedKey(t, q, user, statement)
		usd, via := cashListing(t, q, "USD")
		byUser := gen.SetStatedKeyAssociationParams{ID: chosen.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterUser}
		n, err := q.SetStatedKeyAssociation(ctx, byUser)
		require.NoError(t, err)
		if n != 1 {
			t.Fatalf("the user's association changed %d rows, want 1", n)
		}
		other := newInstrument(t, q, types.AssetClassEquity)
		otherVia := newIdentifier(t, q, other, "US0000000009")
		byDatasource := gen.SetStatedKeyAssociationParams{ID: chosen.ID, UserID: user.ID, InstrumentID: &other.ID, ViaID: &otherVia.ID, Arbiter: gen.ArbiterDatasource}
		n, err = q.SetStatedKeyAssociation(ctx, byDatasource)
		require.NoError(t, err)
		if n != 0 {
			t.Errorf("a later write changed %d rows, want 0: the user's association stands", n)
		}
		got, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: chosen.ID, UserID: user.ID})
		require.NoError(t, err)
		if got.InstrumentID == nil || *got.InstrumentID != usd.InstrumentID || got.Arbiter == nil || *got.Arbiter != gen.ArbiterUser {
			t.Errorf("key = %+v, want the user's association", got)
		}
		if _, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: chosen.ID, UserID: db.NewID()}); !errors.Is(err, db.ErrNotFound) {
			t.Errorf("GetStatedKey as another user: err = %v, want ErrNotFound", err)
		}
	})

	t.Run("an instrument without an arbiter", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		user := newUser(t, q, "no-arbiter@example.com")
		statement := newStatement(t, q, user)
		key := newStatedKey(t, q, user, statement)
		usd, via := cashListing(t, q, "USD")
		_, err := tx.Exec(ctx, "UPDATE stated_keys SET instrument_id = $1, via_id = $2 WHERE id = $3", usd.InstrumentID, via.ID, key.ID)
		if !sqlstate(err, pgerrcode.CheckViolation) {
			t.Errorf("an association with no arbiter: err = %v, want a check violation", err)
		}
	})
}

// TestDatedAssociation checks that a dated association names a MIC_TICKER
// ticker and no instrument, that it replaces a fixed one and keeps the key's
// group, that a fixed association replaces it, that a user's choice stands
// against it, and that the checks refuse every half-written shape.
func TestDatedAssociation(t *testing.T) {
	ctx := context.Background()
	dated := func(key gen.StatedKey, arbiter gen.Arbiter) gen.SetStatedKeyDatedAssociationParams {
		return gen.SetStatedKeyDatedAssociationParams{ID: key.ID, UserID: key.UserID, ViaDomain: "XNAS", ViaValue: "META", Arbiter: arbiter}
	}
	t.Run("replaces a fixed association and keeps the group", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		user := newUser(t, q, "dated@example.com")
		statement := newStatement(t, q, user)
		key := newStatedKey(t, q, user, statement)
		usd, via := cashListing(t, q, "USD")
		_, err := q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: key.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterStated})
		require.NoError(t, err)
		n, err := q.SetStatedKeyDatedAssociation(ctx, dated(key, gen.ArbiterDatasource))
		require.NoError(t, err)
		if n != 1 {
			t.Fatalf("SetStatedKeyDatedAssociation changed %d rows, want 1", n)
		}
		_, err = tx.Exec(ctx, "UPDATE stated_keys SET group_id = id WHERE id = $1", key.ID)
		require.NoError(t, err)
		_, err = q.SetStatedKeyDatedAssociation(ctx, dated(key, gen.ArbiterStated))
		require.NoError(t, err)
		got, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: key.ID, UserID: user.ID})
		require.NoError(t, err)
		if got.InstrumentID != nil || got.ListingID != nil || got.ViaID != nil || got.ViaType == nil || *got.ViaType != types.IdentifierTypeMicTicker ||
			got.ViaDomain == nil || *got.ViaDomain != "XNAS" || got.ViaValue == nil || *got.ViaValue != "META" || got.Arbiter == nil || *got.Arbiter != gen.ArbiterStated {
			t.Errorf("key = %+v, want dated through XNAS:META with arbiter stated and no instrument", got)
		}
		if got.GroupID == nil || *got.GroupID != key.ID {
			t.Errorf("group = %v, want the key keeps its group %s", got.GroupID, key.ID)
		}
		_, err = q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: key.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterUser})
		require.NoError(t, err)
		got, err = q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: key.ID, UserID: user.ID})
		require.NoError(t, err)
		if got.InstrumentID == nil || got.ViaType != nil || got.ViaDomain != nil || got.ViaValue != nil {
			t.Errorf("key = %+v, want a fixed association with no via value", got)
		}
		if n, err := q.SetStatedKeyDatedAssociation(ctx, dated(key, gen.ArbiterDatasource)); err != nil || n != 0 {
			t.Errorf("dating a key the user arbitrated changed %d rows, %v, want 0: the choice stands", n, err)
		}
	})

	tests := []struct {
		name string
		sql  string
	}{
		{name: "a via type without a value", sql: "UPDATE stated_keys SET via_type = 'mic_ticker', via_domain = 'XNAS', arbiter = 'stated' WHERE id = $1"},
		{name: "a via type other than a venue ticker", sql: "UPDATE stated_keys SET via_type = 'isin', via_domain = '', via_value = 'US0378331005', arbiter = 'stated' WHERE id = $1"},
		{name: "a ticker without its venue", sql: "UPDATE stated_keys SET via_type = 'mic_ticker', via_domain = '', via_value = 'META', arbiter = 'stated' WHERE id = $1"},
		{name: "a dated association without an arbiter", sql: "UPDATE stated_keys SET via_type = 'mic_ticker', via_domain = 'XNAS', via_value = 'META' WHERE id = $1"},
		{name: "an arbiter without an association", sql: "UPDATE stated_keys SET arbiter = 'stated' WHERE id = $1"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			tx := begin(t)
			q := gen.New(tx)
			user := newUser(t, q, "dated-"+uuid.NewString()+"@example.com")
			key := newStatedKey(t, q, user, newStatement(t, q, user))
			if _, err := tx.Exec(ctx, tc.sql, key.ID); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("%s: err = %v, want a check violation", tc.name, err)
			}
		})
	}

	t.Run("an instrument and a via value", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		user := newUser(t, q, "dated-fixed@example.com")
		key := newStatedKey(t, q, user, newStatement(t, q, user))
		usd, via := cashListing(t, q, "USD")
		_, err := tx.Exec(ctx, "UPDATE stated_keys SET instrument_id = $1, via_id = $2, via_type = 'mic_ticker', via_domain = 'XNAS', via_value = 'META', arbiter = 'stated' WHERE id = $3", usd.InstrumentID, via.ID, key.ID)
		if !sqlstate(err, pgerrcode.CheckViolation) {
			t.Errorf("an association both fixed and dated: err = %v, want a check violation", err)
		}
	})
}

// TestListKeyOrderDates checks that each key lists the distinct order dates of
// its transactions, and only for the keys and user asked.
func TestListKeyOrderDates(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "dates@example.com")
	statement := newStatement(t, q, user)
	one, two, other := newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement)
	trade := func(key gen.StatedKey, on time.Time) {
		t.Helper()
		_, err := q.CreateTransaction(ctx, gen.CreateTransactionParams{
			ID: db.NewID(), UserID: user.ID, Broker: gen.BrokerIbkr, StatementID: statement.ID, StatedKeyID: key.ID,
			OrderDate: on, SettlementDate: on, AsAt: on, Quantity: decimal.NewFromInt(1),
		})
		require.NoError(t, err)
	}
	trade(one, date(2024, 1, 16))
	trade(one, date(2021, 6, 1))
	trade(one, date(2024, 1, 16))
	trade(two, date(2023, 3, 1))
	trade(other, date(2020, 1, 2))
	rows, err := q.ListKeyOrderDates(ctx, gen.ListKeyOrderDatesParams{UserID: user.ID, Ids: []uuid.UUID{one.ID, two.ID}})
	require.NoError(t, err)
	got := map[uuid.UUID][]time.Time{}
	for _, r := range rows {
		got[r.StatedKeyID] = append(got[r.StatedKeyID], r.OrderDate)
	}
	want := map[uuid.UUID][]time.Time{one.ID: {date(2021, 6, 1), date(2024, 1, 16)}, two.ID: {date(2023, 3, 1)}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListKeyOrderDates mismatch (-want +got):\n%s", diff)
	}
	if rows, err := q.ListKeyOrderDates(ctx, gen.ListKeyOrderDatesParams{UserID: db.NewID(), Ids: []uuid.UUID{one.ID}}); err != nil || len(rows) != 0 {
		t.Errorf("ListKeyOrderDates as another user = %+v, %v, want none", rows, err)
	}
}

// TestListStatementTransactions checks the order of a statement's
// transactions, the via type of a fixed key and of a dated key, and that
// another statement's transactions and another user's are left out.
func TestListStatementTransactions(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "statement-transactions@example.com")
	statement, later := newStatement(t, q, user), newStatement(t, q, user)
	fixed, dated, bare := newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement), newStatedKey(t, q, user, later)
	usd, via := cashListing(t, q, "USD")
	_, err := q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: fixed.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterStated})
	require.NoError(t, err)
	_, err = q.SetStatedKeyDatedAssociation(ctx, gen.SetStatedKeyDatedAssociationParams{ID: dated.ID, UserID: user.ID, ViaDomain: "XNAS", ViaValue: "META", Arbiter: gen.ArbiterDatasource})
	require.NoError(t, err)
	trade := func(st gen.Statement, key gen.StatedKey, on time.Time) gen.Transaction {
		t.Helper()
		row, err := q.CreateTransaction(ctx, gen.CreateTransactionParams{
			ID: db.NewID(), UserID: user.ID, Broker: gen.BrokerIbkr, StatementID: st.ID, StatedKeyID: key.ID,
			OrderDate: on, SettlementDate: on, AsAt: on, Quantity: decimal.NewFromInt(1),
		})
		require.NoError(t, err)
		return row
	}
	second := trade(statement, dated, date(2024, 1, 16))
	first := trade(statement, fixed, date(2021, 6, 1))
	trade(later, bare, date(2020, 1, 2))

	rows, err := q.ListStatementTransactions(ctx, gen.ListStatementTransactionsParams{StatementID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	if len(rows) != 2 || rows[0].Transaction.ID != first.ID || rows[1].Transaction.ID != second.ID {
		t.Fatalf("ListStatementTransactions = %+v, want the fixed key's trade then the dated key's", rows)
	}
	if rows[0].StatedKey.ID != fixed.ID || rows[0].FixedViaType == nil || *rows[0].FixedViaType != types.IdentifierTypeCurrency {
		t.Errorf("fixed row = key %s via %v, want key %s via currency", rows[0].StatedKey.ID, rows[0].FixedViaType, fixed.ID)
	}
	if rows[1].StatedKey.ID != dated.ID || rows[1].FixedViaType != nil || rows[1].StatedKey.ViaValue == nil || *rows[1].StatedKey.ViaValue != "META" {
		t.Errorf("dated row = key %s via %v, want key %s dated through META with no fixed via type", rows[1].StatedKey.ID, rows[1].FixedViaType, dated.ID)
	}
	if rows, err := q.ListStatementTransactions(ctx, gen.ListStatementTransactionsParams{StatementID: statement.ID, UserID: db.NewID()}); err != nil || len(rows) != 0 {
		t.Errorf("ListStatementTransactions as another user = %+v, %v, want none", rows, err)
	}
}

// TestListUnassociatedKeysStating checks that a probe of a ticker finds every
// user's unassociated keys stating it, whatever else they state, that a probe
// of a domain finds a key stating any ticker of the domain, and that an
// associated key, a dated key, a key without a transaction and a key the run
// resolved are left out.
func TestListUnassociatedKeysStating(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user, other := newUser(t, q, "stating@example.com"), newUser(t, q, "stating-other@example.com")
	statement, theirs := newStatement(t, q, user), newStatement(t, q, other)
	stating := func(st gen.Statement, ids ...types.Identifier) gen.StatedKey {
		t.Helper()
		key := upsertKey(t, q, st, gen.UpsertStatedKeyParams{Identifiers: ids})
		names(t, q, st, key)
		return key
	}
	meta := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "META"}
	descr := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "META PLATFORMS " + uuid.NewString()}
	mine := stating(statement, descr, meta)
	theirsKey := stating(theirs, meta)
	aapl := stating(statement, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "AAPL"})
	stating(statement, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "IBM"})
	upsertKey(t, q, statement, gen.UpsertStatedKeyParams{Identifiers: []types.Identifier{meta, {Type: types.IdentifierTypeMicTicker, Value: "META"}}})
	usd, via := cashListing(t, q, "USD")
	fixed := stating(statement, meta, types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "fixed " + uuid.NewString()})
	_, err := q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: fixed.ID, UserID: user.ID, InstrumentID: &usd.InstrumentID, ListingID: &usd.ID, ViaID: &via.ID, Arbiter: gen.ArbiterStated})
	require.NoError(t, err)
	datedKey := stating(statement, meta, types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "dated " + uuid.NewString()})
	_, err = q.SetStatedKeyDatedAssociation(ctx, gen.SetStatedKeyDatedAssociationParams{ID: datedKey.ID, UserID: user.ID, ViaDomain: "XNAS", ViaValue: "META", Arbiter: gen.ArbiterStated})
	require.NoError(t, err)
	resolution := newResolution(t, q, user, statement)
	just := stating(statement, meta, types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "just " + uuid.NewString()})
	resolved(t, q, resolution, just, gen.ResolutionOutcomeUnrecognised)

	// A domain probe leaves the value out, so it matches any ticker.
	probe := func(id types.Identifier) []byte {
		t.Helper()
		obj := map[string]string{"type": string(id.Type), "domain": id.Domain}
		if !id.IsDomain() {
			obj["value"] = id.Value
		}
		b, err := json.Marshal([]map[string]string{obj})
		require.NoError(t, err)
		return b
	}
	list := func(probes ...[]byte) []uuid.UUID {
		t.Helper()
		rows, err := q.ListUnassociatedKeysStating(ctx, gen.ListUnassociatedKeysStatingParams{Probes: probes, RunID: resolution.ID})
		require.NoError(t, err)
		var out []uuid.UUID
		for _, r := range rows {
			if r.UserID == user.ID || r.UserID == other.ID {
				out = append(out, r.ID)
			}
		}
		return out
	}
	sorted := func(ids ...uuid.UUID) []uuid.UUID {
		slices.SortFunc(ids, func(a, b uuid.UUID) int { return strings.Compare(a.String(), b.String()) })
		return ids
	}
	if diff := cmp.Diff(sorted(mine.ID, theirsKey.ID), sorted(list(probe(meta))...)); diff != "" {
		t.Errorf("keys stating XNAS:META mismatch (-want +got):\n%s", diff)
	}
	domain := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS"}
	if diff := cmp.Diff(sorted(mine.ID, theirsKey.ID, aapl.ID), sorted(list(probe(domain))...)); diff != "" {
		t.Errorf("keys stating a ticker of XNAS mismatch (-want +got):\n%s", diff)
	}
}

// TestPlaceTicker checks that a ticker written onto an instrument that lacks
// it moves the row from the instrument that carried it.
func TestPlaceTicker(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	old, present := newInstrument(t, q, types.AssetClassStock), newInstrument(t, q, types.AssetClassStock)
	oldLine, presentLine := newListing(t, q, old, "USD"), newListing(t, q, present, "USD")
	venueTicker(t, q, oldLine, "XNAS", "META")
	row, err := q.PlaceTicker(ctx, gen.PlaceTickerParams{ID: db.NewID(), InstrumentID: present.ID, ListingID: &presentLine.ID, Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "META"})
	require.NoError(t, err)
	if row.InstrumentID != present.ID || row.ListingID == nil || *row.ListingID != presentLine.ID {
		t.Errorf("PlaceTicker = %+v, want the row on the present holder's listing", row)
	}
	found, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "META"})
	require.NoError(t, err)
	if found.Instrument.ID != present.ID {
		t.Errorf("XNAS:META names %s, want the present holder %s", found.Instrument.ID, present.ID)
	}
}
