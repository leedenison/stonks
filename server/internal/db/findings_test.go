//go:build dbtest

package db_test

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

// TestFindings checks what each kind of finding carries: a block its block
// alone, the kinds that concern a key their stated key and grounds, a
// dropped candidate its fetch key and one of the steps that is a finding,
// and the kinds that concern a ticker their fetch key and grounds without a
// key. An unmatched split carries its key and day. A finding is listed with
// what its key states.
func TestFindings(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	user := newUser(t, q, "findings@example.com")
	stmt := newStmt(t, q, user)
	run, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
	require.NoError(t, err)
	fetch := newFetch(t, q, user, run, newDatasource(t, q, "findings", 10))
	key := newStatedKey(t, q, user, stmt)
	fetchKey := servedKey(t, q, fetch, key, "GB00B03MLX29")

	dropped := gen.CreateFindingParams{
		ID: db.NewID(), RunID: run.ID, Kind: gen.FindingKindDropped, StatedKeyID: &key.ID, FetchKeyID: &fetchKey,
		Step: ptr.To(gen.DropStepStated), Detail: ptr.To("candidates in USD, not the stated GBP"),
	}
	require.NoError(t, q.CreateFinding(ctx, dropped))
	require.NoError(t, q.CreateFinding(ctx, gen.CreateFindingParams{
		ID: db.NewID(), RunID: run.ID, Kind: gen.FindingKindContradiction, StatedKeyID: &key.ID, Detail: ptr.To("ISIN and CUSIP name different instruments"),
	}))
	split := newRun(t, q, user)
	require.NoError(t, q.CreateFinding(ctx, gen.CreateFindingParams{
		ID: db.NewID(), RunID: split.ID, Kind: gen.FindingKindUnmatchedSplit, StatedKeyID: &key.ID, EffectiveDate: ptr.To(time.Date(2024, time.June, 7, 0, 0, 0, 0, time.UTC)), Detail: ptr.To("a split on 2024-06-07"),
	}))
	if listed, err := q.ListRunFindings(ctx, []uuid.UUID{split.ID}); err != nil || len(listed) != 1 || listed[0].KeyID == nil || listed[0].Finding.EffectiveDate == nil {
		t.Errorf("an unmatched split finding = %+v, %v, want its key and day", listed, err)
	}
	findings, err := q.ListRunFindings(ctx, []uuid.UUID{run.ID})
	require.NoError(t, err)
	if len(findings) != 2 {
		t.Fatalf("ListRunFindings = %d rows, want 2", len(findings))
	}
	for _, kind := range []gen.FindingKind{gen.FindingKindIdentifierDisagreement, gen.FindingKindBrokenTickerValidityBracket} {
		value := newRun(t, q, user)
		require.NoError(t, q.CreateFinding(ctx, gen.CreateFindingParams{ID: db.NewID(), RunID: value.ID, Kind: kind, FetchKeyID: &fetchKey, Detail: ptr.To("XNAS:META on 2026-03-02")}))
		listed, err := q.ListRunFindings(ctx, []uuid.UUID{value.ID})
		require.NoError(t, err)
		if len(listed) != 1 || listed[0].Finding.StatedKeyID != nil || listed[0].KeyID != nil || listed[0].Finding.FetchKeyID == nil {
			t.Errorf("a %s finding = %+v, want its fetch key and no key", kind, listed)
		}
	}
	if f := findings[0].Finding; f.Kind != gen.FindingKindDropped || f.Step == nil || *f.Step != gen.DropStepStated || f.FetchKeyID == nil || *f.FetchKeyID != fetchKey || f.Detail == nil {
		t.Errorf("dropped finding = %+v, want its step, fetch key and detail", f)
	}
	below := gen.CreateFindingParams{
		ID: db.NewID(), RunID: fetch.ID, Kind: gen.FindingKindContradiction, StatedKeyID: &key.ID, Detail: ptr.To("found below"),
	}
	require.NoError(t, q.CreateFinding(ctx, below))
	if rolled, err := q.ListRunFindings(ctx, []uuid.UUID{run.ID, fetch.ID}); err != nil || len(rolled) != 3 || rolled[2].Finding.ID != below.ID {
		t.Errorf("ListRunFindings(run, fetch) = %d rows, %v, want the run's two then the fetch's", len(rolled), err)
	}
	for _, f := range findings {
		if !slices.Equal(f.KeyIdentifiers, key.Identifiers) {
			t.Errorf("finding %s key = %v, want the key's identifiers %v", f.Finding.Kind, f.KeyIdentifiers, key.Identifiers)
		}
	}

	tests := []struct {
		name string
		// key gives a finding of a ticker a key; noKey takes the key from a
		// finding of a key.
		key, noKey bool
		// corpAct names a corporate action.
		corpAct bool
		arg     gen.CreateFindingParams
	}{
		{name: "dropped without a step", arg: gen.CreateFindingParams{Kind: gen.FindingKindDropped, FetchKeyID: &fetchKey, Detail: ptr.To("d")}},
		{name: "dropped without a fetch key", arg: gen.CreateFindingParams{Kind: gen.FindingKindDropped, Step: ptr.To(gen.DropStepStated), Detail: ptr.To("d")}},
		{name: "a block with detail", arg: gen.CreateFindingParams{Kind: gen.FindingKindBlock, Detail: ptr.To("d")}},
		{name: "a contradiction without detail", arg: gen.CreateFindingParams{Kind: gen.FindingKindContradiction}},
		{name: "a merge with a step", arg: gen.CreateFindingParams{Kind: gen.FindingKindMerged, Step: ptr.To(gen.DropStepStated), Detail: ptr.To("d")}},
		{name: "a disagreement without a fetch key", arg: gen.CreateFindingParams{Kind: gen.FindingKindIdentifierDisagreement, Detail: ptr.To("d")}},
		{name: "a disagreement with a key", key: true, arg: gen.CreateFindingParams{Kind: gen.FindingKindIdentifierDisagreement, FetchKeyID: &fetchKey, Detail: ptr.To("d")}},
		{name: "a broken bracket without detail", arg: gen.CreateFindingParams{Kind: gen.FindingKindBrokenTickerValidityBracket, FetchKeyID: &fetchKey}},
		{name: "a contradiction without a key", noKey: true, arg: gen.CreateFindingParams{Kind: gen.FindingKindContradiction, Detail: ptr.To("d")}},
		{name: "an unhandled corporate action without the corporate action", arg: gen.CreateFindingParams{Kind: gen.FindingKindUnhandledCorpAct, Detail: ptr.To("d")}},
		{name: "an unmatched split without its day", arg: gen.CreateFindingParams{Kind: gen.FindingKindUnmatchedSplit, Detail: ptr.To("d")}},
		{name: "an unmatched split without a key", noKey: true, arg: gen.CreateFindingParams{Kind: gen.FindingKindUnmatchedSplit, EffectiveDate: ptr.To(time.Date(2024, time.June, 7, 0, 0, 0, 0, time.UTC)), Detail: ptr.To("d")}},
		{name: "a contradiction with a day", arg: gen.CreateFindingParams{Kind: gen.FindingKindContradiction, EffectiveDate: ptr.To(time.Date(2024, time.June, 7, 0, 0, 0, 0, time.UTC)), Detail: ptr.To("d")}},
		{name: "a missed event naming a corporate action", corpAct: true, arg: gen.CreateFindingParams{Kind: gen.FindingKindMissedEvent, FetchKeyID: &fetchKey, Detail: ptr.To("d")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Each violation aborts the transaction, so each case takes its own.
			q := newTx(t)
			user := newUser(t, q, "findings-"+uuid.NewString()+"@example.com")
			stmt := newStmt(t, q, user)
			run, err := q.GetRun(ctx, gen.GetRunParams{ID: stmt.ID, UserID: &user.ID})
			require.NoError(t, err)
			key := newStatedKey(t, q, user, stmt)
			tc.arg.ID, tc.arg.RunID = db.NewID(), run.ID
			switch tc.arg.Kind {
			case gen.FindingKindDropped, gen.FindingKindContradiction, gen.FindingKindMerged, gen.FindingKindUnmatchedSplit:
				if !tc.noKey {
					tc.arg.StatedKeyID = &key.ID
				}
			default:
				if tc.key {
					tc.arg.StatedKeyID = &key.ID
				}
			}
			if tc.arg.FetchKeyID != nil {
				fetch := newFetch(t, q, user, run, newDatasource(t, q, "findings", 10))
				tc.arg.FetchKeyID = ptr.To(servedKey(t, q, fetch, key, "GB00B03MLX29"))
			}
			if tc.corpAct {
				_, event := newCorpAct(t, q, newFetching(t, q))
				tc.arg.CorpActID = &event
			}
			if err := q.CreateFinding(ctx, tc.arg); !sqlstate(err, pgerrcode.CheckViolation) {
				t.Errorf("CreateFinding(%s): err = %v, want a check violation", tc.name, err)
			}
		})
	}
}

// occurrences are the finding kinds that belong to the run that raised them,
// so two runs may each raise one for the same subject.
var occurrences = []gen.FindingKind{
	gen.FindingKindDropped, gen.FindingKindContradiction, gen.FindingKindMerged,
	gen.FindingKindIdentifierDisagreement, gen.FindingKindBrokenTickerValidityBracket, gen.FindingKindMissedEvent,
}

// subjects maps each standing kind to the columns its unique index covers.
var subjects = map[gen.FindingKind]string{
	gen.FindingKindBlock:            "block_id",
	gen.FindingKindUnhandledCorpAct: "corp_act_id",
	gen.FindingKindUnmatchedSplit:   "stated_key_id, effective_date",
}

// TestStandingKinds checks that every finding kind is an occurrence or has a
// unique index over its subject. The index must hold a cleared finding too,
// so its predicate leaves out cleared_at.
func TestStandingKinds(t *testing.T) {
	ctx := context.Background()
	rows, err := pool.Query(ctx, "SELECT unnest(enum_range(NULL::finding_kind))::text")
	require.NoError(t, err)
	kinds, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	rows, err = pool.Query(ctx, "SELECT indexdef FROM pg_indexes WHERE tablename = 'findings'")
	require.NoError(t, err)
	defs, err := pgx.CollectRows(rows, pgx.RowTo[string])
	require.NoError(t, err)
	for _, kind := range kinds {
		k := gen.FindingKind(kind)
		if slices.Contains(occurrences, k) {
			continue
		}
		cols, ok := subjects[k]
		if !ok {
			t.Errorf("finding kind %s is neither an occurrence nor a standing kind with a subject", kind)
			continue
		}
		if !slices.ContainsFunc(defs, func(d string) bool {
			where, partial := strings.CutPrefix(d[strings.Index(d, ")")+1:], " WHERE ")
			return strings.HasPrefix(d, "CREATE UNIQUE INDEX") && strings.Contains(d, "USING btree ("+cols+")") &&
				(!partial || strings.Contains(where, "'"+kind+"'") && !strings.Contains(where, "cleared_at"))
		}) {
			t.Errorf("findings has no unique index over (%s) for the kind %s; indexes %v", cols, kind, defs)
		}
	}
}

// TestCreateStandingFinding checks that a second standing finding of one
// subject is skipped and reported as not new, even once the first is
// cleared. Two occurrences of one subject both insert.
func TestCreateStandingFinding(t *testing.T) {
	ctx := context.Background()
	q := newTx(t)
	f := newFetching(t, q)
	_, act := newCorpAct(t, q, f)
	day := ptr.To(time.Date(2024, time.June, 7, 0, 0, 0, 0, time.UTC))
	tests := []struct {
		name string
		arg  gen.CreateStandingFindingParams
	}{
		{name: "an unhandled corporate action", arg: gen.CreateStandingFindingParams{Kind: gen.FindingKindUnhandledCorpAct, CorpActID: &act, Detail: ptr.To("d")}},
		{name: "an unmatched split", arg: gen.CreateStandingFindingParams{Kind: gen.FindingKindUnmatchedSplit, StatedKeyID: &f.key.ID, EffectiveDate: day, Detail: ptr.To("d")}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			first := tc.arg
			first.ID, first.RunID = db.NewID(), newRun(t, q, f.user).ID
			created, err := db.CreateStandingFinding(ctx, q, first)
			require.NoError(t, err)
			if !created {
				t.Fatal("CreateStandingFinding() of a new subject = false, want true")
			}
			_, err = q.ClearFinding(ctx, first.ID)
			require.NoError(t, err)
			second := tc.arg
			second.ID, second.RunID = db.NewID(), newRun(t, q, f.user).ID
			created, err = db.CreateStandingFinding(ctx, q, second)
			require.NoError(t, err)
			if created {
				t.Error("CreateStandingFinding() of a named subject = true, want false")
			}
			if listed, err := q.ListRunFindings(ctx, []uuid.UUID{second.RunID}); err != nil || len(listed) != 0 {
				t.Errorf("findings of the second run = %+v, %v, want none", listed, err)
			}
		})
	}
	for range 2 {
		require.NoError(t, q.CreateFinding(ctx, gen.CreateFindingParams{
			ID: db.NewID(), RunID: newRun(t, q, f.user).ID, Kind: gen.FindingKindContradiction, StatedKeyID: &f.key.ID, Detail: ptr.To("d"),
		}))
	}
}
