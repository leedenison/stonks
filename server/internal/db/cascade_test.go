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
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// owned is every table whose rows a user's runs write.
var owned = []string{
	"runs", "stmts", "stated_keys", "stmt_keys", "stmt_items", "stmt_splits", "txns",
	"resolution_keys", "fetches", "fetch_keys", "identifier_assertions",
	"identifier_period_assertions", "identity_coverage",
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
			stmt := newStmt(t, q, user)
			run, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
			require.NoError(t, err)
			one, two := newStatedKey(t, q, user, stmt), newStatedKey(t, q, user, stmt)
			require.NoError(t, q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: user.ID, Ids: []uuid.UUID{one.ID, two.ID}, GroupIds: []uuid.UUID{one.ID, one.ID}}))
			names(t, q, stmt, one)
			require.NoError(t, q.CreateStmtItem(ctx, gen.CreateStmtItemParams{StmtID: stmt.ID, UserID: user.ID, Ordinal: 1, Reason: "r", Stated: []byte(`{}`)}))
			require.NoError(t, q.CreateStmtSplit(ctx, gen.CreateStmtSplitParams{StmtID: stmt.ID, UserID: user.ID, Ordinal: 0, StatedKeyID: one.ID, EffectiveDate: date(2026, 3, 1), Quantity: decimal.NewFromInt(1)}))
			resolved(t, q, newResolution(t, q, user, stmt), one, gen.ResolutionOutcomeUnavailable)

			ds := newDatasource(t, q, "cascade", 10)
			fetch := newFetch(t, q, user, run, ds)
			key := servedKey(t, q, fetch, one, "GB00B03MLX29")
			shel := newTicker(t, q, "XLON", "SHEL")
			require.NoError(t, q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
				ID: db.NewID(), FetchKeyID: &key, TickerID: shel.ID, Market: shel.Market, AssertedOn: date(2026, 3, 2),
			}))
			_, err = tx.Exec(ctx, "INSERT INTO identifier_period_assertions (id, fetch_key_id, market, ticker_id, asserted_from, asserted_before) VALUES ($1, $2, 'XLON', $3, '2026-01-01', '2026-03-02')", db.NewID(), key, shel.ID)
			require.NoError(t, err)
			instr, err := q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock, FetchKeyID: &key})
			require.NoError(t, err)
			require.NoError(t, q.UpsertIdentityCoverage(ctx, gen.UpsertIdentityCoverageParams{InstrID: instr.ID, Datasource: ds.Name, FetchKeyID: key}))
			_, err = q.CreateDatasourceBlock(ctx, gen.CreateDatasourceBlockParams{
				ID: db.NewID(), Datasource: ds.Name, Kind: gen.FetchKindIdentity, Scope: gen.BlockScopeIdentifier,
				SentType: ptr.To(types.IdentifierTypeIsin), SentValue: ptr.To("GB00B03MLX29"), Reason: "refused",
				FetchKeyID: key, FindingID: db.NewID(), RunID: fetch.ID,
			})
			require.NoError(t, err)
			replay, err := q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user.ID, Kind: gen.RunKindReplayRun, Trigger: gen.RunTriggerAdministrator})
			require.NoError(t, err)
			require.NoError(t, q.CreateReplay(ctx, gen.CreateReplayParams{ID: replay.ID, UserID: user.ID, SourceID: run.ID, StartedBy: admin.ID}))

			_, err = tx.Exec(ctx, "UPDATE instrs SET fetch_key_id = NULL WHERE fetch_key_id IN (SELECT id FROM fetch_keys WHERE user_id = $1)", user.ID)
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
			if rows, err := q.ListTickersByID(ctx, []uuid.UUID{shel.ID}); err != nil || len(rows) != 1 {
				t.Errorf("ListTickersByID(SHEL) = %v, %v, want the row: a ticker outlives its facts", rows, err)
			}
			var provenance *uuid.UUID
			require.NoError(t, tx.QueryRow(ctx, "SELECT fetch_key_id FROM instrs WHERE id = $1", instr.ID).Scan(&provenance))
			if provenance != nil {
				t.Errorf("instrument provenance = %v, want none: it stands as reference data", provenance)
			}
		})
	}
}
