//go:build dbtest

package db_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func newDatasource(t *testing.T, q *gen.Queries, name string, precedence int32) gen.Datasource {
	t.Helper()
	row, err := q.CreateDatasource(context.Background(), gen.CreateDatasourceParams{
		Name: name, Enabled: true, Precedence: precedence,
	})
	require.NoError(t, err)
	return row
}

// newFetch records a fetch run under parent and its fetches row.
func newFetch(t *testing.T, q *gen.Queries, user gen.User, parent gen.Run, ds gen.Datasource) gen.Fetch {
	t.Helper()
	ctx := context.Background()
	run, err := q.CreateRun(ctx, gen.CreateRunParams{
		ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindFetch,
		Trigger: gen.RunTriggerRun, ParentID: &parent.ID,
	})
	require.NoError(t, err)
	row, err := q.CreateFetch(ctx, gen.CreateFetchParams{
		ID: run.ID, UserID: user.ID, Datasource: ds.Name, Kind: gen.FetchKindIdentity,
	})
	require.NoError(t, err)
	return row
}

func newStatedKey(t *testing.T, q *gen.Queries, user gen.User, statement gen.Statement) gen.StatedKey {
	t.Helper()
	key, err := q.CreateStatedKey(context.Background(), gen.CreateStatedKeyParams{
		ID: db.NewID(), StatementID: statement.ID, UserID: user.ID,
		Description: ptr.To(uuid.NewString()), Identifiers: []types.Identifier{},
	})
	require.NoError(t, err)
	return key
}

// servedKey records a served fetch key, which is what a block references.
func servedKey(t *testing.T, q *gen.Queries, fetch gen.Fetch, key gen.StatedKey, value string) uuid.UUID {
	t.Helper()
	id := db.NewID()
	require.NoError(t, q.CreateFetchKey(context.Background(), gen.CreateFetchKeyParams{
		ID: id, FetchID: fetch.ID, UserID: fetch.UserID, StatedKeyID: key.ID,
		Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: ptr.To(types.IdentifierTypeIsin), SentValue: ptr.To(value),
	}))
	return id
}

// TestDatasources checks that the schema seeds none, and that name breaks a
// tie in precedence.
func TestDatasources(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)

	seeded, err := q.ListDatasources(ctx)
	require.NoError(t, err)
	if len(seeded) != 0 {
		t.Errorf("ListDatasources of a migrated database = %+v, want none", seeded)
	}

	newDatasource(t, q, "beta", 20)
	newDatasource(t, q, "gamma", 10)
	newDatasource(t, q, "alpha", 10)

	listed, err := q.ListDatasources(ctx)
	require.NoError(t, err)
	var names []string
	for _, ds := range listed {
		names = append(names, ds.Name)
	}
	want := []string{"alpha", "gamma", "beta"}
	if len(names) != len(want) || names[0] != want[0] || names[1] != want[1] || names[2] != want[2] {
		t.Errorf("ListDatasources = %v, want %v", names, want)
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
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)

	fetch := newFetch(t, q, user, run, ds)
	got, err := q.GetFetch(ctx, gen.GetFetchParams{ID: fetch.ID, UserID: user.ID})
	require.NoError(t, err)
	if got.Datasource != ds.Name || got.Kind != gen.FetchKindIdentity {
		t.Errorf("GetFetch = %+v, want the identity fetch of %s", got, ds.Name)
	}
	if _, err := q.GetFetch(ctx, gen.GetFetchParams{ID: fetch.ID, UserID: other.ID}); !errors.Is(err, db.ErrNotFound) {
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
			_, err := q.CreateFetch(ctx, gen.CreateFetchParams{ID: db.NewID(), UserID: user.ID, Datasource: ds.Name, Kind: gen.FetchKindIdentity})
			return err
		}},
		{name: "fetch on another user's run", want: pgerrcode.ForeignKeyViolation, do: func(q *gen.Queries, user, other gen.User) error {
			ds := newDatasource(t, q, "cross-user", 10)
			run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerUser})
			require.NoError(t, err)
			_, err = q.CreateFetch(ctx, gen.CreateFetchParams{ID: run.ID, UserID: other.ID, Datasource: ds.Name, Kind: gen.FetchKindIdentity})
			return err
		}},
		{name: "fetch of an unknown datasource", want: pgerrcode.ForeignKeyViolation, do: func(q *gen.Queries, user, _ gen.User) error {
			run, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindFetch, Trigger: gen.RunTriggerUser})
			require.NoError(t, err)
			_, err = q.CreateFetch(ctx, gen.CreateFetchParams{ID: run.ID, UserID: user.ID, Datasource: "absent", Kind: gen.FetchKindIdentity})
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
	tests := []struct {
		name string
		want string
		arg  gen.CreateFetchKeyParams
	}{
		{name: "not served with an identifier sent", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeNotServed, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r")}},
		{name: "served without an identifier sent", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1}},
		{name: "a type sent with no value", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: isin}},
		{name: "served with a reason", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeServed, Attempts: 1, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r")}},
		{name: "failed without a reason", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeFailedPermanent, Attempts: 1, SentType: isin, SentValue: ptr.To("v")}},
		{name: "blocked having called", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeBlocked, Attempts: 1, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r")}},
		{name: "failed without calling", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeFailedTemporary, Attempts: 0, SentType: isin, SentValue: ptr.To("v"), Reason: ptr.To("r")}},
		{name: "a domain sent with no identifier", want: pgerrcode.CheckViolation, arg: gen.CreateFetchKeyParams{
			Outcome: gen.FetchOutcomeNotServed, SentDomain: "XLON", Reason: ptr.To("r")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			user := newUser(t, q, "fetch-keys-"+uuid.NewString()+"@example.com")
			statement := newStatement(t, q, user)
			run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
			require.NoError(t, err)
			fetch := newFetch(t, q, user, run, newDatasource(t, q, "fetch-keys-"+uuid.NewString(), 10))
			key := newStatedKey(t, q, user, statement)
			tc.arg.ID, tc.arg.FetchID, tc.arg.UserID, tc.arg.StatedKeyID = db.NewID(), fetch.ID, user.ID, key.ID
			if err := q.CreateFetchKey(ctx, tc.arg); !sqlstate(err, tc.want) {
				t.Errorf("CreateFetchKey(%s): err = %v, want sqlstate %s", tc.name, err, tc.want)
			}
		})
	}

	t.Run("an instrument on a key nothing answered", func(t *testing.T) {
		q := newTx(t)
		user := newUser(t, q, "fetch-keys-instrument@example.com")
		statement := newStatement(t, q, user)
		run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
		require.NoError(t, err)
		fetch := newFetch(t, q, user, run, newDatasource(t, q, "fetch-keys-instrument", 10))
		key := newStatedKey(t, q, user, statement)
		found, err := q.GetInstrumentByIdentifier(ctx, gen.GetInstrumentByIdentifierParams{Type: types.IdentifierTypeCurrency, Value: "USD"})
		require.NoError(t, err)

		id := db.NewID()
		require.NoError(t, q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: id, FetchID: fetch.ID, UserID: user.ID, StatedKeyID: key.ID,
			Outcome: gen.FetchOutcomeNotServed, Reason: ptr.To("serves no isin"),
		}))
		err = q.SetFetchKeyInstrument(ctx, gen.SetFetchKeyInstrumentParams{ID: id, UserID: user.ID, InstrumentID: &found.Instrument.ID})
		if !sqlstate(err, pgerrcode.CheckViolation) {
			t.Errorf("SetFetchKeyInstrument on a not_served key: err = %v, want a check violation", err)
		}
	})

	t.Run("one row per key of a fetch", func(t *testing.T) {
		q := newTx(t)
		user := newUser(t, q, "fetch-keys-unique@example.com")
		statement := newStatement(t, q, user)
		run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
		require.NoError(t, err)
		fetch := newFetch(t, q, user, run, newDatasource(t, q, "fetch-keys-unique", 10))
		key := newStatedKey(t, q, user, statement)

		servedKey(t, q, fetch, key, "GB00B03MLX29")
		err = q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
			ID: db.NewID(), FetchID: fetch.ID, UserID: user.ID, StatedKeyID: key.ID,
			Outcome: gen.FetchOutcomeNotServed, Reason: ptr.To("r"),
		})
		if !sqlstate(err, pgerrcode.UniqueViolation) {
			t.Errorf("CreateFetchKey of a key the fetch already holds: err = %v, want a unique violation", err)
		}
	})
}

// TestFetchIdentifiers checks one assertion per triple, counting empty domains
// as equal.
func TestFetchIdentifiers(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "fetch-identifiers@example.com")
	statement := newStatement(t, q, user)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	fetch := newFetch(t, q, user, run, newDatasource(t, q, "fetch-identifiers", 10))
	key := servedKey(t, q, fetch, newStatedKey(t, q, user, statement), "GB00B03MLX29")

	require.NoError(t, q.CreateFetchIdentifier(ctx, gen.CreateFetchIdentifierParams{
		FetchKeyID: key, Type: types.IdentifierTypeIsin, Value: "GB00B03MLX29"}))
	require.NoError(t, q.CreateFetchIdentifier(ctx, gen.CreateFetchIdentifierParams{
		FetchKeyID: key, Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "SHEL"}))

	listed, err := q.ListFetchIdentifiers(ctx, key)
	require.NoError(t, err)
	if len(listed) != 2 {
		t.Fatalf("ListFetchIdentifiers = %d rows, want 2", len(listed))
	}

	err = q.CreateFetchIdentifier(ctx, gen.CreateFetchIdentifierParams{
		FetchKeyID: key, Type: types.IdentifierTypeIsin, Value: "GB00B03MLX29"})
	if !sqlstate(err, pgerrcode.UniqueViolation) {
		t.Errorf("CreateFetchIdentifier of a triple with no domain twice: err = %v, want a unique violation", err)
	}
}

// TestDatasourceBlocks checks one open block per scope, each reported by a
// finding of the fetch run, and that clearing one clears its finding and
// admits the next.
func TestDatasourceBlocks(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "blocks@example.com")
	statement := newStatement(t, q, user)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
	require.NoError(t, err)
	ds := newDatasource(t, q, "blocks", 10)
	fetch := newFetch(t, q, user, run, ds)
	key := servedKey(t, q, fetch, newStatedKey(t, q, user, statement), "GB00B03MLX29")

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

	open, err := q.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: ds.Name, Kind: gen.FetchKindIdentity})
	require.NoError(t, err)
	if len(open) != 3 {
		t.Fatalf("ListOpenBlocks = %d rows, want 3: two identifiers and the datasource", len(open))
	}
	findings, err := q.ListRunFindings(ctx, fetch.ID)
	require.NoError(t, err)
	if len(findings) != 3 {
		t.Fatalf("ListRunFindings = %+v, want one per open block", findings)
	}
	for _, f := range findings {
		if f.Kind != gen.FindingKindBlock || f.BlockID == nil || f.ClearedAt != nil {
			t.Errorf("finding = %+v, want an open finding on a block", f)
		}
	}

	require.NoError(t, q.ClearDatasourceBlock(ctx, first.ID))
	findings, err = q.ListRunFindings(ctx, fetch.ID)
	require.NoError(t, err)
	for _, f := range findings {
		if cleared := f.ClearedAt != nil; cleared != (*f.BlockID == first.ID) {
			t.Errorf("finding on block %s cleared = %v after clearing block %s", *f.BlockID, cleared, first.ID)
		}
	}
	create(block(gen.BlockScopeIdentifier, ptr.To("GB00B03MLX29")), 1)
	open, err = q.ListOpenBlocks(ctx, gen.ListOpenBlocksParams{Datasource: ds.Name, Kind: gen.FetchKindIdentity})
	require.NoError(t, err)
	if len(open) != 3 {
		t.Errorf("ListOpenBlocks after clearing one and opening it again = %d rows, want 3", len(open))
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
	}
	for _, tc := range scopes {
		t.Run(tc.name, func(t *testing.T) {
			// The violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			user := newUser(t, q, "blocks-"+uuid.NewString()+"@example.com")
			statement := newStatement(t, q, user)
			run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
			require.NoError(t, err)
			ds := newDatasource(t, q, "blocks-"+uuid.NewString(), 10)
			fetch := newFetch(t, q, user, run, ds)
			arg := tc.arg
			arg.Datasource = ds.Name
			arg.FetchKeyID = servedKey(t, q, fetch, newStatedKey(t, q, user, statement), "GB00B03MLX29")
			arg.RunID = fetch.ID
			if _, err := q.CreateDatasourceBlock(ctx, arg); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateDatasourceBlock(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}
