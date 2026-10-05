//go:build dbtest

package db_test

import (
	"context"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// owned is every table whose rows a user's runs write.
var owned = []string{
	"runs", "statements", "stated_keys", "statement_items", "statement_splits", "transactions",
	"resolution_keys", "fetches", "fetch_keys", "fetch_identifiers", "identity_coverage",
	"datasource_blocks", "findings", "replays",
}

// TestDeleteUserCascades checks that deleting a user deletes every row their
// runs wrote. The test first clears the provenance that their fetches gave
// instruments and deletes the replays an administrator started. An
// administrator's replay of the user's run names both users, so the test
// deletes them in both orders.
func TestDeleteUserCascades(t *testing.T) {
	for _, adminFirst := range []bool{true, false} {
		t.Run(fmt.Sprintf("administrator first %v", adminFirst), func(t *testing.T) {
			ctx := context.Background()
			tx := begin(t)
			q := gen.New(tx)
			user := newUser(t, q, "cascade-"+uuid.NewString()+"@example.com")
			admin := newUser(t, q, "cascade-admin-"+uuid.NewString()+"@example.com")
			statement := newStatement(t, q, user)
			run, err := q.GetRun(ctx, gen.GetRunParams{ID: statement.ID, UserID: user.ID})
			require.NoError(t, err)
			one, two := newStatedKey(t, q, user, statement), newStatedKey(t, q, user, statement)
			require.NoError(t, q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: user.ID, Ids: []uuid.UUID{one.ID, two.ID}, GroupIds: []uuid.UUID{one.ID, one.ID}}))
			names(t, q, statement, one)
			require.NoError(t, q.CreateStatementItem(ctx, gen.CreateStatementItemParams{StatementID: statement.ID, UserID: user.ID, Ordinal: 1, Reason: "r", Stated: []byte(`{}`)}))
			require.NoError(t, q.CreateStatementSplit(ctx, gen.CreateStatementSplitParams{StatementID: statement.ID, UserID: user.ID, Ordinal: 0, StatedKeyID: one.ID, EffectiveDate: date(2026, 3, 1), Quantity: decimal.NewFromInt(1)}))
			resolved(t, q, newResolution(t, q, user, statement), one, gen.ResolutionOutcomeUnavailable)

			ds := newDatasource(t, q, "cascade-"+uuid.NewString(), 10)
			fetch := newFetch(t, q, user, run, ds)
			key := servedKey(t, q, fetch, one, "GB00B03MLX29")
			require.NoError(t, q.CreateFetchIdentifier(ctx, gen.CreateFetchIdentifierParams{FetchKeyID: key, Type: types.IdentifierTypeIsin, Value: "GB00B03MLX29"}))
			instrument, err := q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: gen.AssetClassStock, FetchKeyID: &key})
			require.NoError(t, err)
			require.NoError(t, q.UpsertIdentityCoverage(ctx, gen.UpsertIdentityCoverageParams{InstrumentID: instrument.ID, Datasource: ds.Name, FetchKeyID: key}))
			_, err = q.CreateDatasourceBlock(ctx, gen.CreateDatasourceBlockParams{
				ID: db.NewID(), Datasource: ds.Name, Kind: gen.FetchKindIdentity, Scope: gen.BlockScopeIdentifier,
				SentType: ptr.To(types.IdentifierTypeIsin), SentValue: ptr.To("GB00B03MLX29"), Reason: "refused",
				FetchKeyID: key, FindingID: db.NewID(), RunID: fetch.ID,
			})
			require.NoError(t, err)
			replay, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: user.ID, Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator})
			require.NoError(t, err)
			require.NoError(t, q.CreateReplay(ctx, gen.CreateReplayParams{ID: replay.ID, UserID: user.ID, SourceID: run.ID, StartedBy: admin.ID}))

			_, err = tx.Exec(ctx, "UPDATE instruments SET fetch_key_id = NULL WHERE fetch_key_id IN (SELECT id FROM fetch_keys WHERE user_id = $1)", user.ID)
			require.NoError(t, err)
			_, err = tx.Exec(ctx, "DELETE FROM replays WHERE started_by = $1", admin.ID)
			require.NoError(t, err)
			order := []uuid.UUID{user.ID, admin.ID}
			if adminFirst {
				order = []uuid.UUID{admin.ID, user.ID}
			}
			for _, id := range order {
				_, err := tx.Exec(ctx, "DELETE FROM users WHERE id = $1", id)
				require.NoError(t, err)
			}

			for _, table := range owned {
				var n int
				require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM "+table).Scan(&n))
				if n != 0 {
					t.Errorf("%s has %d rows after the users were deleted, want none", table, n)
				}
			}
			var provenance *uuid.UUID
			require.NoError(t, tx.QueryRow(ctx, "SELECT fetch_key_id FROM instruments WHERE id = $1", instrument.ID).Scan(&provenance))
			if provenance != nil {
				t.Errorf("instrument provenance = %v, want none: it stands as reference data", provenance)
			}
		})
	}
}
