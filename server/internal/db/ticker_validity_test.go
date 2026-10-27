//go:build dbtest

package db_test

import (
	"context"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// TestIdentifierAssertions checks that an assertion has one source, a fetch
// key or an administrator's attest identifier run, and goes with that source.
func TestIdentifierAssertions(t *testing.T) {
	ctx := context.Background()
	on := date(2026, 3, 2)
	assertion := func() gen.CreateIdentifierAssertionParams {
		return gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "SHEL", AssertedOn: on,
		}
	}

	t.Run("one per fetch key, identifier and day", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)
		key := servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
		arg := assertion()
		arg.FetchKeyID = &key
		require.NoError(t, q.CreateIdentifierAssertion(ctx, arg))
		listed, err := q.ListFetchKeyAssertions(ctx, &key)
		require.NoError(t, err)
		if len(listed) != 1 || listed[0].InstrumentID != nil || !listed[0].AssertedOn.Equal(on) {
			t.Errorf("ListFetchKeyAssertions = %+v, want the negative assertion on %s", listed, on.Format("2006-01-02"))
		}
		arg.ID = db.NewID()
		if err := q.CreateIdentifierAssertion(ctx, arg); !sqlstate(err, pgerrcode.UniqueViolation) {
			t.Errorf("CreateIdentifierAssertion of the same day twice: err = %v, want a unique violation", err)
		}
	})

	t.Run("an identifier attestation goes with its run", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		f := newFetching(t, q)
		admin := newUser(t, q, "attest-"+f.user.Email)
		run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
		require.NoError(t, err)
		arg := assertion()
		arg.RunID, arg.AttestedBy = &run.ID, &admin.ID
		require.NoError(t, q.CreateIdentifierAssertion(ctx, arg))
		_, err = tx.Exec(ctx, "DELETE FROM runs WHERE id = $1", run.ID)
		require.NoError(t, err)
		var n int
		require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM identifier_assertions WHERE id = $1", arg.ID).Scan(&n))
		if n != 0 {
			t.Errorf("assertion survived its attest identifier run")
		}
	})

	refusals := []struct {
		name string
		arg  func(q *gen.Queries, f fetching) gen.CreateIdentifierAssertionParams
	}{
		{name: "no source", arg: func(*gen.Queries, fetching) gen.CreateIdentifierAssertionParams { return assertion() }},
		{name: "a fetch key and a run", arg: func(q *gen.Queries, f fetching) gen.CreateIdentifierAssertionParams {
			key := servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
			arg := assertion()
			arg.FetchKeyID, arg.RunID, arg.AttestedBy = &key, &f.run.ID, &f.user.ID
			return arg
		}},
		{name: "a run with no administrator", arg: func(_ *gen.Queries, f fetching) gen.CreateIdentifierAssertionParams {
			arg := assertion()
			arg.RunID = &f.run.ID
			return arg
		}},
	}
	for _, tc := range refusals {
		t.Run(tc.name, func(t *testing.T) {
			// The violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			f := newFetching(t, q)
			if err := q.CreateIdentifierAssertion(ctx, tc.arg(q, f)); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateIdentifierAssertion(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}

// TestIdentifierEventChecks checks the shape the schema demands of an event
// and of coverage.
func TestIdentifierEventChecks(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name string
		sql  string
	}{
		{name: "a rename with no new value", sql: "INSERT INTO identifier_events (id, fetch_key_id, market, value, action, effective_on) VALUES ($1, $2, 'US', 'FB', 'renamed', '2022-06-09')"},
		{name: "a retirement with a new value", sql: "INSERT INTO identifier_events (id, fetch_key_id, market, value, action, to_value, effective_on) VALUES ($1, $2, 'US', 'FB', 'retired', 'META', '2022-06-09')"},
		{name: "an event with no market", sql: "INSERT INTO identifier_events (id, fetch_key_id, market, value, action, effective_on) VALUES ($1, $2, '', 'FB', 'retired', '2022-06-09')"},
		{name: "coverage that ends as it starts", sql: "INSERT INTO identifier_event_coverage (id, fetch_key_id, market, covered_from, covered_before) VALUES ($1, $2, 'US', '2022-06-09', '2022-06-09')"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			tx := begin(t)
			q := gen.New(tx)
			f := newFetching(t, q)
			key := servedKey(t, q, f.fetch, f.key, "US30303M1027")
			if _, err := tx.Exec(ctx, tc.sql, db.NewID(), key); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("%s: err = %v, want a check violation", tc.name, err)
			}
		})
	}
}

// TestListHeldTickers checks that the query lists a ticker once, with the
// class and currency of the oldest key dated through it. It also checks that
// the query leaves out a key without a transaction and a fixed key.
func TestListHeldTickers(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "held@example.com")
	stmt := newStmt(t, q, user)
	held := "HLD" + uuid.NewString()[:8]
	idle := "IDL" + uuid.NewString()[:8]
	stock, etf := types.AssetClassStock, types.AssetClassEtf
	usd, gbp := "USD", "GBP"
	dated := func(value string, class *types.AssetClass, currency *string, traded bool) gen.StatedKey {
		t.Helper()
		key := upsertKey(t, q, stmt, gen.UpsertStatedKeyParams{AssetClass: class, Currency: currency, Identifiers: []types.Identifier{
			{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: uuid.NewString()},
		}})
		if traded {
			names(t, q, stmt, key)
		}
		_, err := q.SetStatedKeyDatedAssociation(ctx, gen.SetStatedKeyDatedAssociationParams{ID: key.ID, UserID: user.ID, ViaDomain: "XNAS", ViaValue: value, Arbiter: gen.ArbiterDatasource})
		require.NoError(t, err)
		return key
	}
	dated(held, &stock, &usd, true)
	dated(held, &etf, &gbp, true)
	dated(idle, &stock, &usd, false)
	fixed := newStatedKey(t, q, user, stmt)
	names(t, q, stmt, fixed)
	listing, via := cashListing(t, q, "USD")
	_, err := q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: fixed.ID, UserID: user.ID, InstrumentID: &listing.InstrumentID, ListingID: &listing.ID, ViaID: &via.ID, Arbiter: gen.ArbiterStated})
	require.NoError(t, err)

	rows, err := q.ListHeldTickers(ctx)
	require.NoError(t, err)
	var got []gen.ListHeldTickersRow
	for _, r := range rows {
		if r.Value == held || r.Value == idle {
			got = append(got, r)
		}
	}
	want := []gen.ListHeldTickersRow{{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: held, AssetClass: &stock, Currency: &usd}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("ListHeldTickers mismatch (-want +got):\n%s", diff)
	}
}

// TestListRunAssertions checks that a run lists the assertions of its fetch
// runs, each with its datasource. It also lists the assertions the run
// attested, each with the administrator's email. The listing pages and
// leaves out another run's assertions.
func TestListRunAssertions(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	f := newFetching(t, q)
	on := date(2026, 3, 2)
	assert := func(arg gen.CreateIdentifierAssertionParams, value string) uuid.UUID {
		t.Helper()
		arg.ID, arg.Type, arg.Domain, arg.Value, arg.AssertedOn = db.NewID(), types.IdentifierTypeMicTicker, "XLON", value, on
		require.NoError(t, q.CreateIdentifierAssertion(ctx, arg))
		return arg.ID
	}
	reassertion, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindReassertTickers, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	child, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, ParentID: &reassertion.ID})
	require.NoError(t, err)
	fetch, err := q.CreateFetch(ctx, gen.CreateFetchParams{ID: child.ID, Datasource: f.ds.Name, Endpoint: "https://" + f.ds.Name + ".test", Kind: gen.FetchKindIdentity})
	require.NoError(t, err)
	key := db.NewID()
	require.NoError(t, q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: fetch.ID, Outcome: gen.FetchOutcomeServed, Attempts: 1, AnsweredAt: ptr.To(answered),
		SentType: ptr.To(types.IdentifierTypeMicTicker), SentDomain: "XLON", SentValue: ptr.To("SHEL"),
	}))
	first := assert(gen.CreateIdentifierAssertionParams{FetchKeyID: &key}, "SHEL")
	second := assert(gen.CreateIdentifierAssertionParams{FetchKeyID: &key}, "BP")
	other := servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
	assert(gen.CreateIdentifierAssertionParams{FetchKeyID: &other}, "HSBA")
	admin := newUser(t, q, "listed-"+f.user.Email)
	attestation, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	attested := assert(gen.CreateIdentifierAssertionParams{RunID: &attestation.ID, AttestedBy: &admin.ID}, "SHEL")

	list := func(run uuid.UUID, after *uuid.UUID, lim *int32) []gen.ListRunAssertionsRow {
		t.Helper()
		rows, err := q.ListRunAssertions(ctx, gen.ListRunAssertionsParams{RunID: &run, After: after, Lim: lim})
		require.NoError(t, err)
		return rows
	}
	one := int32(1)
	if got := list(reassertion.ID, nil, &one); len(got) != 1 || got[0].IdentifierAssertion.ID != first || got[0].Datasource == nil || *got[0].Datasource != f.ds.Name {
		t.Errorf("first page = %+v, want the first assertion from %s", got, f.ds.Name)
	}
	if got := list(reassertion.ID, &first, nil); len(got) != 1 || got[0].IdentifierAssertion.ID != second {
		t.Errorf("page after the first = %+v, want only the second assertion", got)
	}
	if got := list(attestation.ID, nil, nil); len(got) != 1 || got[0].IdentifierAssertion.ID != attested || got[0].AttestedBy == nil || *got[0].AttestedBy != admin.Email || got[0].Datasource != nil {
		t.Errorf("identifier attestation's assertions = %+v, want the attested one with %s", got, admin.Email)
	}
}
