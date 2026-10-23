//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// newDatasource creates an enabled datasource named prefix and a random
// suffix. Packages test concurrently against one database. Another
// package's open transaction can hold a name, and an insert of that name
// then blocks until the transaction ends.
func newDatasource(t *testing.T, q *gen.Queries, prefix string, precedence int32) gen.Datasource {
	t.Helper()
	row, err := q.CreateDatasource(context.Background(), gen.CreateDatasourceParams{
		Name: prefix + "-" + uuid.NewString(), Enabled: true, Precedence: precedence,
	})
	require.NoError(t, err)
	return row
}

// newFetch records a fetch run under parent and its fetches row.
func newFetch(t *testing.T, q *gen.Queries, user gen.User, parent gen.Run, ds gen.Datasource) gen.Fetch {
	t.Helper()
	ctx := context.Background()
	run, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindFetch,
		Trigger: gen.RunTriggerRun, ParentID: &parent.ID,
	})
	require.NoError(t, err)
	row, err := q.CreateFetch(ctx, gen.CreateFetchParams{
		ID: run.ID, UserID: &user.ID, Datasource: ds.Name, Endpoint: "https://" + ds.Name + ".test", Kind: gen.FetchKindIdentity,
	})
	require.NoError(t, err)
	return row
}

// newStatedKey records a key of user stating a description of its own,
// linked to statement.
func newStatedKey(t *testing.T, q *gen.Queries, user gen.User, statement gen.Statement) gen.StatedKey {
	t.Helper()
	if statement.UserID != user.ID {
		t.Fatalf("statement %s is not %s's", statement.ID, user.ID)
	}
	return upsertKey(t, q, statement, gen.UpsertStatedKeyParams{
		Identifiers: []types.Identifier{{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: uuid.NewString()}},
	})
}

// servedKey records a served fetch key, which is what a block references.
func servedKey(t *testing.T, q *gen.Queries, fetch gen.Fetch, key gen.StatedKey, value string) uuid.UUID {
	t.Helper()
	id := db.NewID()
	require.NoError(t, q.CreateFetchKey(context.Background(), gen.CreateFetchKeyParams{
		ID: id, FetchID: fetch.ID, UserID: fetch.UserID, StatedKeyID: &key.ID,
		Outcome: gen.FetchOutcomeServed, Attempts: 1, AnsweredAt: ptr.To(answered),
		SentType: ptr.To(types.IdentifierTypeIsin), SentValue: ptr.To(value),
	}))
	return id
}

// answered is the moment a test's datasource gave its answer.
var answered = time.Date(2026, time.March, 2, 10, 0, 0, 0, time.UTC)

// fetching is a fetch on fresh rows, with the rows it needs.
type fetching struct {
	user      gen.User
	statement gen.Statement
	run       gen.Run
	ds        gen.Datasource
	fetch     gen.Fetch
	key       gen.StatedKey
}

func newFetching(t *testing.T, q *gen.Queries) fetching {
	t.Helper()
	f := fetching{user: newUser(t, q, "fetching-"+uuid.NewString()+"@example.com")}
	f.statement = newStatement(t, q, f.user)
	var err error
	f.run, err = q.GetRun(context.Background(), gen.GetRunParams{ID: f.statement.ID, UserID: &f.user.ID})
	require.NoError(t, err)
	f.ds = newDatasource(t, q, "fetching", 10)
	f.fetch = newFetch(t, q, f.user, f.run, f.ds)
	f.key = newStatedKey(t, q, f.user, f.statement)
	return f
}

// TestDatasources checks that the schema seeds none, that they list in
// precedence order, that two may not share a precedence, and that a config
// is a JSON object.
func TestDatasources(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)

	seeded, err := q.ListDatasources(ctx)
	require.NoError(t, err)
	if len(seeded) != 0 {
		t.Errorf("ListDatasources of a migrated database = %+v, want none", seeded)
	}

	beta := newDatasource(t, q, "beta", 20)
	gamma := newDatasource(t, q, "gamma", 30)
	alpha := newDatasource(t, q, "alpha", 10)

	listed, err := q.ListDatasources(ctx)
	require.NoError(t, err)
	var names []string
	for _, ds := range listed {
		names = append(names, ds.Name)
	}
	if diff := cmp.Diff([]string{alpha.Name, beta.Name, gamma.Name}, names); diff != "" {
		t.Errorf("ListDatasources mismatch (-want +got):\n%s", diff)
	}

	t.Run("a shared precedence", func(t *testing.T) {
		tx := begin(t)
		q := gen.New(tx)
		newDatasource(t, q, "one", 1)
		newDatasource(t, q, "two", 1)
		_, err := tx.Exec(ctx, "SET CONSTRAINTS ALL IMMEDIATE")
		if !sqlstate(err, pgerrcode.UniqueViolation) {
			t.Errorf("two datasources at one precedence: err = %v, want a unique violation", err)
		}
	})

	t.Run("a config that is not an object", func(t *testing.T) {
		q := gen.New(begin(t))
		_, err := q.CreateDatasource(ctx, gen.CreateDatasourceParams{Name: "listed-" + uuid.NewString(), Precedence: 12, Config: []byte("[]")})
		if !sqlstate(err, pgerrcode.CheckViolation) {
			t.Errorf("CreateDatasource with an array config: err = %v, want a check violation", err)
		}
	})
}

// TestUpdateDatasource checks that the endpoint and the credential are set
// and cleared, that the config is replaced, and that the precedence is set
// by position.
func TestUpdateDatasource(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	alpha, beta := newDatasource(t, q, "alpha", 1), newDatasource(t, q, "beta", 2)

	row, err := q.UpdateDatasource(ctx, gen.UpdateDatasourceParams{Name: beta.Name, Enabled: false, Endpoint: "http://stub", Credential: "secret", Config: []byte(`{"plan": "basic"}`)})
	require.NoError(t, err)
	if row.Enabled || row.Endpoint == nil || *row.Endpoint != "http://stub" || row.Credential == nil || *row.Credential != "secret" || string(row.Config) != `{"plan": "basic"}` {
		t.Errorf("UpdateDatasource = %+v, want disabled at http://stub holding secret and plan basic", row)
	}
	row, err = q.UpdateDatasource(ctx, gen.UpdateDatasourceParams{Name: beta.Name, Enabled: true})
	require.NoError(t, err)
	if !row.Enabled || row.Credential != nil || row.Endpoint != nil || string(row.Config) != "{}" {
		t.Errorf("UpdateDatasource with an empty credential and endpoint and no config = %+v, want enabled with both NULL and the config empty", row)
	}
	if _, err := q.UpdateDatasource(ctx, gen.UpdateDatasourceParams{Name: "gone"}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("UpdateDatasource of no datasource: err = %v, want ErrNotFound", err)
	}

	// alpha and beta swap precedences in one statement, which the deferred
	// constraint admits.
	require.NoError(t, q.SetDatasourcePrecedence(ctx, []string{beta.Name, alpha.Name}))
	listed, err := q.ListDatasources(ctx)
	require.NoError(t, err)
	if len(listed) != 2 || listed[0].Name != beta.Name || listed[0].Precedence != 1 || listed[1].Precedence != 2 {
		t.Errorf("ListDatasources after reorder = %+v, want beta at 1 then alpha at 2", listed)
	}
}

// TestFetches checks that a fetch is a run of its own user.
func TestFetches(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "fetches@example.com")
	other := newUser(t, q, "fetches-other@example.com")
	statement := newStatement(t, q, user)
	ds := newDatasource(t, q, "fetches", 10)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: &user.ID})
	require.NoError(t, err)

	fetch := newFetch(t, q, user, run, ds)
	got, err := q.GetFetch(ctx, gen.GetFetchParams{ID: fetch.ID, UserID: &user.ID})
	require.NoError(t, err)
	if got.Datasource != ds.Name || got.Kind != gen.FetchKindIdentity {
		t.Errorf("GetFetch = %+v, want the identity fetch of %s", got, ds.Name)
	}
	if _, err := q.GetFetch(ctx, gen.GetFetchParams{ID: fetch.ID, UserID: &other.ID}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("GetFetch as another user: err = %v, want ErrNotFound", err)
	}

	// Each violation aborts the transaction, so each takes its own.
	violations := []struct {
		name string
		want string
		do   func(q *gen.Queries, user, other gen.User) error
	}{
		{name: "fetch on an id that is no run", want: pgerrcode.ForeignKeyViolation, do: func(q *gen.Queries, user, _ gen.User) error {
			ds := newDatasource(t, q, "no-run", 10)
			_, err := q.CreateFetch(ctx, gen.CreateFetchParams{ID: db.NewID(), UserID: &user.ID, Datasource: ds.Name, Kind: gen.FetchKindIdentity})
			return err
		}},
		{name: "fetch on another user's run", want: pgerrcode.ForeignKeyViolation, do: func(q *gen.Queries, user, other gen.User) error {
			ds := newDatasource(t, q, "cross-user", 10)
			run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerUser})
			require.NoError(t, err)
			_, err = q.CreateFetch(ctx, gen.CreateFetchParams{ID: run.ID, UserID: &other.ID, Datasource: ds.Name, Kind: gen.FetchKindIdentity})
			return err
		}},
		{name: "fetch of an unknown datasource", want: pgerrcode.ForeignKeyViolation, do: func(q *gen.Queries, user, _ gen.User) error {
			run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerUser})
			require.NoError(t, err)
			_, err = q.CreateFetch(ctx, gen.CreateFetchParams{ID: run.ID, UserID: &user.ID, Datasource: "absent", Kind: gen.FetchKindIdentity})
			return err
		}},
	}
	for _, tc := range violations {
		t.Run(tc.name, func(t *testing.T) {
			q := newTx(t)
			user := newUser(t, q, "fetches-"+uuid.NewString()+"@example.com")
			other := newUser(t, q, "fetches-other-"+uuid.NewString()+"@example.com")
			if err := tc.do(q, user, other); !sqlstate(err, tc.want) {
				t.Errorf("%s: err = %v, want sqlstate %s", tc.name, err, tc.want)
			}
		})
	}
}

// TestFetchKeys checks what each outcome admits.
func TestFetchKeys(t *testing.T) {
	ctx := context.Background()

	isin := ptr.To(types.IdentifierTypeIsin)
	at := ptr.To(answered)
	tests := []struct {
		name string
		want string
		arg  gen.CreateFetchKeyParams
	}{
		{name: "not served with an identifier sent", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeNotServed, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r")}},
		{name: "served without an identifier sent", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1, AnsweredAt: at}},
		{name: "a type sent with neither a value nor a domain", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: isin, AnsweredAt: at}},
		{name: "served with a reason", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r"), AnsweredAt: at}},
		{name: "served with no moment of the answer", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: isin, SentValue: ptr.To("v")}},
		{name: "a moment of the answer for a key not served", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeFailedTemporary, Attempts: 1, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r"), AnsweredAt: at}},
		{name: "failed without a reason", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeFailedPermanent, Attempts: 1, SentType: isin, SentValue: ptr.To("v")}},
		{name: "blocked having called", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeBlocked, Attempts: 1, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r")}},
		{name: "a domain sent with no identifier", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeNotServed, SentDomain: "XLON", Reason: ptr.To("r")}},
		{name: "candidates offered by a key not served", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeNotServed, Reason: ptr.To("r"), Candidates: 2}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			f := newFetching(t, q)
			tc.arg.ID, tc.arg.FetchID, tc.arg.UserID, tc.arg.StatedKeyID = db.NewID(), f.fetch.ID, &f.user.ID, &f.key.ID
			if err := q.CreateFetchKey(ctx, tc.arg); !sqlstate(err, tc.want) {
				t.Errorf("CreateFetchKey(%s): err = %v, want sqlstate %s", tc.name, err, tc.want)
			}
		})
	}

	t.Run("a key the cache answered", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)
		require.NoError(t, q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: db.NewID(), FetchID: f.fetch.ID, UserID: &f.user.ID, StatedKeyID: &f.key.ID,
			Outcome: gen.FetchOutcomeServed, Attempts: 0, SentType: isin, SentValue: ptr.To("v"), Candidates: 1, AnsweredAt: at,
		}))
	})

	t.Run("a domain no stated key asked for", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)
		require.NoError(t, q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: db.NewID(), FetchID: f.fetch.ID, UserID: &f.user.ID,
			Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: ptr.To(types.IdentifierTypeMicTicker), SentDomain: "XNYS", AnsweredAt: at,
		}))
		items, err := q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: f.fetch.ID})
		require.NoError(t, err)
		if len(items) != 1 || items[0].FetchKey.StatedKeyID != nil || items[0].KeyIdentifiers != nil || items[0].FetchKey.SentValue != nil {
			t.Errorf("ListFetchItems = %+v, want the domain with no stated key", items)
		}
	})

	t.Run("a key that failed while its datasource was paused", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)
		require.NoError(t, q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: db.NewID(), FetchID: f.fetch.ID, UserID: &f.user.ID, StatedKeyID: &f.key.ID,
			Outcome: gen.FetchOutcomeFailedTemporary, Attempts: 0, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("paused"),
		}))
	})

	t.Run("an instrument on a key nothing served", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)
		found, err := q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "USD"})
		require.NoError(t, err)

		id := db.NewID()
		require.NoError(t, q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: id, FetchID: f.fetch.ID, UserID: &f.user.ID, StatedKeyID: &f.key.ID,
			Outcome: gen.FetchOutcomeNotServed, Reason: ptr.To("serves no isin"),
		}))
		err = q.SetFetchKeyInstrument(ctx, gen.SetFetchKeyInstrumentParams{ID: id, UserID: &f.user.ID, InstrumentID: &found.Instrument.ID})
		if !sqlstate(err, pgerrcode.CheckViolation) {
			t.Errorf("SetFetchKeyInstrument on a not_served key: err = %v, want a check violation", err)
		}
	})

	t.Run("one row per key of a fetch", func(t *testing.T) {
		q := newTx(t)
		f := newFetching(t, q)

		servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
		err := q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: db.NewID(), FetchID: f.fetch.ID, UserID: &f.user.ID, StatedKeyID: &f.key.ID,
			Outcome: gen.FetchOutcomeNotServed, Reason: ptr.To("r"),
		})
		if !sqlstate(err, pgerrcode.UniqueViolation) {
			t.Errorf("CreateFetchKey of a key the fetch already holds: err = %v, want a unique violation", err)
		}
	})
}

// TestDatasourceBlocks checks one open block per scope, each reported by a
// finding of the fetch run, and that clearing one clears its finding and
// admits the next.
func TestDatasourceBlocks(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	f := newFetching(t, q)
	ds, fetch, stated := f.ds, f.fetch, f.key
	key := servedKey(t, q, fetch, stated, "GB00B03MLX29")

	block := func(scope gen.BlockScope, value *string) gen.CreateDatasourceBlockParams {
		arg := gen.CreateDatasourceBlockParams{
			ID: db.NewID(), Datasource: ds.Name, Kind: gen.FetchKindIdentity,
			Scope: scope, Reason: "refused", FetchKeyID: key,
			FindingID: db.NewID(), RunID: fetch.ID,
		}
		if value != nil {
			arg.SentType, arg.SentValue = ptr.To(types.IdentifierTypeIsin), value
		}
		return arg
	}

	create := func(arg gen.CreateDatasourceBlockParams, want int64) {
		t.Helper()
		n, err := q.CreateDatasourceBlock(ctx, arg)
		require.NoError(t, err)
		if n != want {
			t.Errorf("CreateDatasourceBlock(%s %v) = %d findings, want %d", arg.Scope, arg.SentValue, n, want)
		}
	}
	first := block(gen.BlockScopeIdentifier, ptr.To("GB00B03MLX29"))
	create(first, 1)
	create(block(gen.BlockScopeIdentifier, ptr.To("GB00B03MLX29")), 0)
	create(block(gen.BlockScopeIdentifier, ptr.To("US0378331005")), 1)
	create(block(gen.BlockScopeDatasource, nil), 1)
	create(block(gen.BlockScopeDatasource, nil), 0)
	domain := func(mic string) gen.CreateDatasourceBlockParams {
		arg := block(gen.BlockScopeDomain, nil)
		arg.SentType, arg.SentDomain = ptr.To(types.IdentifierTypeMicTicker), mic
		return arg
	}
	create(domain("XNYS"), 1)
	create(domain("XNYS"), 0)

	open, err := q.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: ds.Name, Kind: gen.FetchKindIdentity})
	require.NoError(t, err)
	if len(open) != 4 {
		t.Fatalf("ListOpenBlocks = %d rows, want 4: two identifiers, the domain and the datasource", len(open))
	}
	findings, err := q.ListRunFindings(ctx, []uuid.UUID{fetch.ID})
	require.NoError(t, err)
	if len(findings) != 4 {
		t.Fatalf("ListRunFindings = %+v, want one per open block", findings)
	}
	for _, f := range findings {
		if f.Finding.Kind != gen.FindingKindBlock || f.Finding.BlockID == nil || f.Finding.ClearedAt != nil {
			t.Errorf("finding = %+v, want an open finding on a block", f.Finding)
		}
		if !slices.Equal(f.KeyIdentifiers, stated.Identifiers) {
			t.Errorf("block finding key = %v, want %v: the key whose call created the block", f.KeyIdentifiers, stated.Identifiers)
		}
		if f.BlockReason == nil || *f.BlockReason != "refused" {
			t.Errorf("block finding reason = %v, want the block's %q", f.BlockReason, "refused")
		}
	}

	_, err = q.ClearDatasourceBlock(ctx, first.ID)
	require.NoError(t, err)
	findings, err = q.ListRunFindings(ctx, []uuid.UUID{fetch.ID})
	require.NoError(t, err)
	for _, f := range findings {
		if cleared := f.Finding.ClearedAt != nil; cleared != (*f.Finding.BlockID == first.ID) {
			t.Errorf("finding on block %s cleared = %v after clearing block %s", *f.Finding.BlockID, cleared, first.ID)
		}
	}
	create(block(gen.BlockScopeIdentifier, ptr.To("GB00B03MLX29")), 1)
	open, err = q.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: ds.Name, Kind: gen.FetchKindIdentity})
	require.NoError(t, err)
	if len(open) != 4 {
		t.Errorf("ListOpenBlocks after clearing one and opening it again = %d rows, want 4", len(open))
	}

	domained := block(gen.BlockScopeDatasource, nil)
	domained.SentDomain = "XLON"
	scopes := []struct {
		name string
		arg  gen.CreateDatasourceBlockParams
	}{
		{name: "an identifier block naming none", arg: block(gen.BlockScopeIdentifier, nil)},
		{name: "a datasource block naming one", arg: block(gen.BlockScopeDatasource, ptr.To("GB00B03MLX29"))},
		{name: "a datasource block naming a domain", arg: domained},
		{name: "a domain block naming a value", arg: func() gen.CreateDatasourceBlockParams {
			arg := domain("XNYS")
			arg.SentValue = ptr.To("AAPL")
			return arg
		}()},
		{name: "a domain block naming no domain", arg: domain("")},
	}
	for _, tc := range scopes {
		t.Run(tc.name, func(t *testing.T) {
			// The violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			f := newFetching(t, q)
			arg := tc.arg
			arg.Datasource = f.ds.Name
			arg.FetchKeyID = servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
			arg.RunID = f.fetch.ID
			if _, err := q.CreateDatasourceBlock(ctx, arg); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateDatasourceBlock(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}

// TestIdentityCoverage checks one row per instrument and datasource, the
// latest served fetch key winning, and the provenance a fetch key gives the
// rows written from its response.
func TestIdentityCoverage(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	f := newFetching(t, q)
	ds := f.ds
	first := servedKey(t, q, f.fetch, f.key, "GB00B03MLX29")
	second := servedKey(t, q, newFetch(t, q, f.user, f.run, ds), f.key, "GB00B03MLX29")

	instrument, err := q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock, FetchKeyID: &first})
	require.NoError(t, err)
	if instrument.FetchKeyID == nil || *instrument.FetchKeyID != first {
		t.Errorf("instrument provenance = %v, want fetch key %s", instrument.FetchKeyID, first)
	}
	listing, err := q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: instrument.ID, Currency: "GBP", FetchKeyID: &first})
	require.NoError(t, err)
	identifier, err := q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument.ID, ListingID: &listing.ID, Type: types.IdentifierTypeSedol, Value: "B03MLX2", FetchKeyID: &first})
	require.NoError(t, err)
	if listing.FetchKeyID == nil || identifier.FetchKeyID == nil || *listing.FetchKeyID != first || *identifier.FetchKeyID != first {
		t.Errorf("listing provenance %v, identifier provenance %v, want fetch key %s", listing.FetchKeyID, identifier.FetchKeyID, first)
	}

	require.NoError(t, q.UpsertIdentityCoverage(ctx, gen.UpsertIdentityCoverageParams{InstrumentID: instrument.ID, Datasource: ds.Name, FetchKeyID: first}))
	require.NoError(t, q.UpsertIdentityCoverage(ctx, gen.UpsertIdentityCoverageParams{InstrumentID: instrument.ID, Datasource: ds.Name, FetchKeyID: second}))
	rows, err := q.ListIdentityCoverage(ctx, []uuid.UUID{instrument.ID, db.NewID()})
	require.NoError(t, err)
	if len(rows) != 1 || rows[0].Datasource != ds.Name || rows[0].FetchKeyID != second {
		t.Errorf("ListIdentityCoverage = %+v, want one row for %s holding fetch key %s", rows, ds.Name, second)
	}
}
