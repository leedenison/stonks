//go:build dbtest

package corpact

import (
	"context"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// splitDay is the effective date of every stated split. It is a Friday, as a
// broker dates a split that trades adjusted from the Monday.
var splitDay = day(2024, 6, 7)

// instrument creates an instrument of class.
func (s *stack) instrument(t *testing.T, class types.AssetClass) uuid.UUID {
	t.Helper()
	inst, err := s.q.CreateInstr(context.Background(), gen.CreateInstrParams{ID: db.NewID(), AssetClass: class})
	require.NoError(t, err)
	return inst.ID
}

// names records an assertion, dated on, that mt names inst.
func (s *stack) names(t *testing.T, mt types.MarketTicker, inst uuid.UUID, on time.Time) {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: s.parent.UserID, Kind: gen.RunKindDatasourceFetch, Trigger: gen.RunTriggerRun, ParentID: &s.parent.ID})
	require.NoError(t, err)
	_, err = s.q.CreateFetch(ctx, gen.CreateFetchParams{ID: run.ID, UserID: s.parent.UserID, Datasource: s.alphaName, Endpoint: "https://example.com", Kind: gen.FetchKindIdentity})
	require.NoError(t, err)
	key := db.NewID()
	require.NoError(t, s.q.CreateFetchKey(ctx, gen.CreateFetchKeyParams{
		ID: key, FetchID: run.ID, UserID: s.parent.UserID, Outcome: gen.FetchOutcomeServed, Attempts: 1,
		SentType: ptr.To(types.IdentifierTypeMicTicker), SentDomain: mt.Market, SentValue: &mt.Value, AnsweredAt: &on,
	}))
	require.NoError(t, s.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, Type: types.IdentifierTypeMicTicker, Domain: mt.Market, Value: mt.Value, InstrID: &inst, AssertedOn: on,
	}))
}

// user creates a user.
func (s *stack) user(t *testing.T) uuid.UUID {
	t.Helper()
	u, err := s.q.CreateUser(context.Background(), gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	return u.ID
}

// fixedKey creates a key of user with a fixed association to inst, made
// through an ISIN.
func (s *stack) fixedKey(t *testing.T, user, inst uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	via, key := db.NewID(), db.NewID()
	isin := uuid.NewString()
	_, err := s.tx.Exec(ctx, `INSERT INTO identifiers (id, instr_id, type, value) VALUES ($1, $2, 'isin', $3)`, via, inst, isin)
	require.NoError(t, err)
	_, err = s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, instr_id, via_id, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'isin', 'value', $3::text)), $4, $5, 'stated')`, key, user, isin, inst, via)
	require.NoError(t, err)
	return key
}

// datedKey creates a key of the stack's user dated through the ticker value
// at venue.
func (s *stack) datedKey(t *testing.T, venue, value string) uuid.UUID {
	t.Helper()
	key := db.NewID()
	_, err := s.tx.Exec(context.Background(), `INSERT INTO stated_keys (id, user_id, identifiers, via_type, via_domain, via_value, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'domain', $3::text, 'value', $4::text)), 'mic_ticker', $3, $4, 'datasource')`,
		key, *s.parent.UserID, venue, value)
	require.NoError(t, err)
	return key
}

// states records a statement of user that states a split of key on splitDay,
// with ratio r where r is not nil.
func (s *stack) states(t *testing.T, user, key uuid.UUID, r *types.Ratio) {
	t.Helper()
	ctx := context.Background()
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), UserID: &user, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser})
	require.NoError(t, err)
	_, err = s.q.CreateStmt(ctx, gen.CreateStmtParams{ID: run.ID, UserID: user, Broker: gen.BrokerIbkr, OrderFrom: day(2024, 1, 1), OrderBefore: day(2025, 1, 1)})
	require.NoError(t, err)
	arg := gen.CreateStmtSplitParams{StmtID: run.ID, UserID: user, StatedKeyID: key, EffectiveDate: splitDay, Quantity: decimal.NewFromInt(90)}
	if r != nil {
		arg.RatioFrom, arg.RatioTo = &r.From, &r.To
	}
	require.NoError(t, s.q.CreateStmtSplit(ctx, arg))
}

// splitFound is an unmatched split finding, with the datasource of its fetch
// run.
type splitFound struct {
	id         uuid.UUID
	key        uuid.UUID
	day        time.Time
	datasource string
	detail     string
	cleared    bool
}

func (s *stack) unmatchedSplits(t *testing.T) []splitFound {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `
		SELECT findings.id, findings.stated_key_id, findings.effective_date, fetches.datasource, findings.detail, findings.cleared_at IS NOT NULL
		FROM findings
		JOIN fetches ON fetches.id = findings.run_id
		WHERE findings.kind = 'unmatched_split'
		ORDER BY findings.created_at, findings.id`)
	require.NoError(t, err)
	out, err := pgx.CollectRows(rows, func(r pgx.CollectableRow) (splitFound, error) {
		var f splitFound
		return f, r.Scan(&f.id, &f.key, &f.day, &f.datasource, &f.detail, &f.cleared)
	})
	require.NoError(t, err)
	return out
}

// nvda is a stock that US:NVDA names, with a key of the stack's user fixed
// to it.
func (s *stack) nvda(t *testing.T) (uuid.UUID, uuid.UUID) {
	t.Helper()
	inst := s.instrument(t, types.AssetClassStock)
	s.names(t, us("NVDA"), inst, day(2024, 1, 2))
	return inst, s.fixedKey(t, *s.parent.UserID, inst)
}

// TestSplitMatched checks that a split matches an action of equal ratio that a
// datasource dates on the Monday after the stated day.
func TestSplitMatched(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	_, key := s.nvda(t)
	s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
	s.alpha.Responses[ticker("XNAS", "NVDA")] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
	s.fetch(t, ticker("XNAS", "NVDA"))
	if got := s.unmatchedSplits(t); len(got) != 0 {
		t.Errorf("findings = %+v, want none", got)
	}
}

// TestSplitUnmatched checks the finding a split raises when the calendar
// lacks it. Once the finding is cleared, a repeat fetch and a second
// statement of the same split add no finding and keep it cleared.
func TestSplitUnmatched(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.only(t, s.alphaName)
	_, key := s.nvda(t)
	user := *s.parent.UserID
	s.states(t, user, key, ptr.To(ratio(1, 10)))
	s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
	s.fetch(t, ticker("XNAS", "NVDA"))

	got := s.unmatchedSplits(t)
	want := []splitFound{{key: key, day: splitDay, datasource: s.alphaName, detail: "a split of 10 for 1 on 2024-06-07 lacks a corporate action of equal ratio within 7 days"}}
	if diff := cmp.Diff(want, got, cmp.AllowUnexported(splitFound{}), cmpIgnoreID); diff != "" {
		t.Fatalf("findings mismatch (-want +got):\n%s", diff)
	}

	_, err := s.q.ClearFinding(ctx, got[0].id)
	require.NoError(t, err)
	s.states(t, user, key, nil)
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, ticker("XNAS", "NVDA"))
	if again := s.unmatchedSplits(t); len(again) != 1 || !again[0].cleared {
		t.Errorf("findings after a repeat = %+v, want the one, still cleared", again)
	}
}

var cmpIgnoreID = cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".id" }, cmp.Ignore())

// TestSplitRun checks that the finding goes on the run of the first covering
// datasource, and that another datasource's stored answer does not make a
// fetch cover the split.
func TestSplitRun(t *testing.T) {
	t.Run("both cover", func(t *testing.T) {
		s := newStack(t)
		_, key := s.nvda(t)
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
		s.beta.Responses[ticker("XNAS", "NVDA")] = covering()
		s.fetch(t, ticker("XNAS", "NVDA"))
		if got := s.unmatchedSplits(t); len(got) != 1 || got[0].datasource != s.alphaName {
			t.Errorf("findings = %+v, want one on %s's run", got, s.alphaName)
		}
	})
	t.Run("only a stored answer of another datasource covers", func(t *testing.T) {
		s := newStack(t)
		_, key := s.nvda(t)
		s.only(t, s.betaName)
		s.beta.Responses[ticker("XNAS", "NVDA")] = covering()
		s.fetch(t, ticker("XNAS", "NVDA"))
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.only(t, s.alphaName)
		s.alpha.Responses[ticker("XNAS", "NVDA")] = market.CorpActResult{}
		s.now = answered.AddDate(0, 0, 1)
		s.fetch(t, ticker("XNAS", "NVDA"))
		if got := s.unmatchedSplits(t); len(got) != 0 {
			t.Errorf("findings = %+v, want none", got)
		}
	})
}

// TestSplitUnrelatedFetch checks that a fetch of another ticker leaves a split
// unchecked, although a stored answer covers it.
func TestSplitUnrelatedFetch(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	_, key := s.nvda(t)
	s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
	s.fetch(t, ticker("XNAS", "NVDA"))
	s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
	s.alpha.Responses[ticker("XNAS", "AAPL")] = covering()
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, ticker("XNAS", "AAPL"))
	if got := s.unmatchedSplits(t); len(got) != 0 {
		t.Errorf("findings = %+v, want none", got)
	}
}

// TestSplitUncovered checks that a split on a day the answer does not cover
// raises nothing: before its coverage, or on the day of an unhandled action.
func TestSplitUncovered(t *testing.T) {
	tests := []struct {
		name   string
		answer market.CorpActResult
	}{
		{name: "before coverage", answer: market.CorpActResult{Coverage: &types.Period{From: day(2025, 1, 1), Before: day(2027, 1, 1)}}},
		{name: "an unhandled action's day", answer: covering(market.CorpAct{ExDate: splitDay, Ratio: ratio(1, 1), Type: "spin_off"})},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			s.only(t, s.alphaName)
			_, key := s.nvda(t)
			s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
			s.alpha.Responses[ticker("XNAS", "NVDA")] = tc.answer
			s.fetch(t, ticker("XNAS", "NVDA"))
			if got := s.unmatchedSplits(t); len(got) != 0 {
				t.Errorf("findings = %+v, want none", got)
			}
		})
	}
}

// TestSplitDatedKey checks that a dated key's split is checked through a
// ticker of the fetched market, at whichever venue the key states it, and not
// through the same value in another market.
func TestSplitDatedKey(t *testing.T) {
	tests := []struct {
		venue string
		want  int
	}{
		{venue: "XNYS", want: 1},
		{venue: "XLON", want: 0},
	}
	for _, tc := range tests {
		t.Run(tc.venue, func(t *testing.T) {
			s := newStack(t)
			s.only(t, s.alphaName)
			inst := s.instrument(t, types.AssetClassStock)
			s.names(t, us("NVDA"), inst, day(2024, 1, 2))
			key := s.datedKey(t, tc.venue, "NVDA")
			s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
			s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
			s.fetch(t, ticker("XNAS", "NVDA"))
			if got := s.unmatchedSplits(t); len(got) != tc.want {
				t.Errorf("findings = %+v, want %d", got, tc.want)
			}
		})
	}
}

// TestSplitCalendarByTickerValidity checks that the calendar attributes an action by
// what its ticker named on the ex-date. An action of a ticker that has moved
// to another instrument does not match. An action of the instrument's earlier
// ticker matches, although the fetch did not ask for that ticker.
func TestSplitCalendarByTickerValidity(t *testing.T) {
	t.Run("a reused ticker", func(t *testing.T) {
		s := newStack(t)
		s.only(t, s.alphaName)
		inst, other := s.instrument(t, types.AssetClassStock), s.instrument(t, types.AssetClassStock)
		s.names(t, us("NVDA"), inst, day(2024, 6, 1))
		s.names(t, us("NVDA"), other, day(2024, 6, 9))
		key := s.fixedKey(t, *s.parent.UserID, inst)
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.alpha.Responses[ticker("XNAS", "NVDA")] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
		s.fetch(t, ticker("XNAS", "NVDA"))
		if got := s.unmatchedSplits(t); len(got) != 1 {
			t.Errorf("findings = %+v, want one", got)
		}
	})
	t.Run("an earlier ticker", func(t *testing.T) {
		s := newStack(t)
		s.only(t, s.alphaName)
		inst := s.instrument(t, types.AssetClassStock)
		s.names(t, us("OLD"), inst, day(2020, 1, 2))
		s.alpha.Responses[ticker("XNAS", "OLD")] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
		s.fetch(t, ticker("XNAS", "OLD"))
		s.names(t, us("NEW"), inst, day(2024, 1, 2))
		key := s.fixedKey(t, *s.parent.UserID, inst)
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.alpha.Responses[ticker("XNAS", "NEW")] = covering()
		s.now = answered.AddDate(0, 0, 1)
		s.fetch(t, ticker("XNAS", "NEW"))
		if got := s.unmatchedSplits(t); len(got) != 0 {
			t.Errorf("findings = %+v, want none", got)
		}
	})
}

// TestSplitOption checks that a split of an option raises nothing.
func TestSplitOption(t *testing.T) {
	s := newStack(t)
	s.only(t, s.alphaName)
	inst := s.instrument(t, types.AssetClassOption)
	s.names(t, us("NVDA"), inst, day(2024, 1, 2))
	key := s.fixedKey(t, *s.parent.UserID, inst)
	s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
	s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
	s.fetch(t, ticker("XNAS", "NVDA"))
	if got := s.unmatchedSplits(t); len(got) != 0 {
		t.Errorf("findings = %+v, want none", got)
	}
}

// TestSplitUsers checks that a user's fetch checks only that user's splits,
// and that a system fetch checks every user's.
func TestSplitUsers(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.only(t, s.alphaName)
	inst, mine := s.nvda(t)
	other := s.user(t)
	theirs := s.fixedKey(t, other, inst)
	s.states(t, *s.parent.UserID, mine, ptr.To(ratio(1, 10)))
	s.states(t, other, theirs, ptr.To(ratio(1, 10)))
	s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
	s.fetch(t, ticker("XNAS", "NVDA"))
	if got := s.unmatchedSplits(t); len(got) != 1 || got[0].key != mine {
		t.Fatalf("findings of the user's fetch = %+v, want one of key %s", got, mine)
	}

	system, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindReplayRun, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	s.now = answered.AddDate(0, 0, 1)
	require.NoError(t, s.recorder.Fetch(ctx, system, []market.CorpActRequest{{Ticker: ticker("XNAS", "NVDA"), Period: asked}}))
	got := s.unmatchedSplits(t)
	keys := map[uuid.UUID]bool{}
	for _, f := range got {
		keys[f.key] = true
	}
	if diff := cmp.Diff(map[uuid.UUID]bool{mine: true, theirs: true}, keys); diff != "" {
		t.Errorf("keys of the findings after a system fetch mismatch (-want +got):\n%s", diff)
	}
	if len(got) != 2 {
		t.Errorf("findings after a system fetch = %+v, want two", got)
	}
}
