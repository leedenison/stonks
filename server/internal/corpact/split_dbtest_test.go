//go:build dbtest

package corpact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/testutil/dbtest"
	"github.com/leedenison/stonks/server/internal/testutil/metrictest"
	"github.com/leedenison/stonks/server/internal/types"
)

// splitDay is the effective date of every stated split. It is a Friday, as a
// broker dates a split that trades adjusted from the Monday.
var splitDay = day(2024, 6, 7)

// instr creates an instrument of class with a USD listing whose primary
// venue is XNAS.
func (s *stack) instr(t *testing.T, class types.AssetClass) uuid.UUID {
	t.Helper()
	return s.listed(t, class, map[string]*string{"USD": ptr.To("XNAS")})
}

// listed creates an instrument of class with a listing in each currency of
// primaries, at the primary venue it maps to. A nil venue states none.
func (s *stack) listed(t *testing.T, class types.AssetClass, primaries map[string]*string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: class})
	require.NoError(t, err)
	for currency, venue := range primaries {
		_, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: currency, PrimaryMic: venue})
		require.NoError(t, err)
	}
	return instr.ID
}

// names asserts that mt names instr on day on.
func (s *stack) names(t *testing.T, mt types.MarketTicker, instr uuid.UUID, on time.Time) {
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
	ticker := dbtest.Ticker(t, s.q, mt).ID
	require.NoError(t, s.q.CreateIdentifierAssertion(ctx, gen.CreateIdentifierAssertionParams{
		ID: db.NewID(), FetchKeyID: &key, TickerID: ticker, Market: mt.Market, InstrID: &instr, AssertedFrom: on, AssertedBefore: on.AddDate(0, 0, 1),
	}))
	locked, err := tickers.Lock(ctx, s.q, []uuid.UUID{ticker})
	require.NoError(t, err)
	require.NoError(t, tickervalidity.Rebuild(ctx, s.q, locked))
}

// user creates a user.
func (s *stack) user(t *testing.T) uuid.UUID {
	t.Helper()
	u, err := s.q.CreateUser(context.Background(), gen.CreateUserParams{ID: db.NewID(), Email: uuid.NewString() + "@example.com", Role: gen.UserRoleUser})
	require.NoError(t, err)
	return u.ID
}

// fixedKey creates a key of user with a fixed association to instr, made
// through an ISIN.
func (s *stack) fixedKey(t *testing.T, user, instr uuid.UUID) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	via, key := db.NewID(), db.NewID()
	isin := uuid.NewString()
	_, err := s.tx.Exec(ctx, `INSERT INTO identifiers (id, instr_id, type, value) VALUES ($1, $2, 'isin', $3)`, via, instr, isin)
	require.NoError(t, err)
	_, err = s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, instr_id, via_identifier_id, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'isin', 'value', $3::text)), $4, $5, 'stated')`, key, user, isin, instr, via)
	require.NoError(t, err)
	return key
}

// datedKey creates a key of the stack's user dated through the ticker value
// at venue.
func (s *stack) datedKey(t *testing.T, venue, value string) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	key := db.NewID()
	var market, operating string
	require.NoError(t, s.tx.QueryRow(ctx, "SELECT market, operating_mic FROM mic_markets WHERE mic = $1", venue).Scan(&market, &operating))
	row := dbtest.Ticker(t, s.q, types.MarketTicker{Market: market, Value: value})
	_, err := s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, via_ticker_id, via_market, via_venue, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'domain', $3::text, 'value', $4::text)), $5, $6, $7, 'datasource')`,
		key, *s.parent.UserID, venue, value, row.ID, market, operating)
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
	instr := s.instr(t, types.AssetClassStock)
	s.names(t, us("NVDA"), instr, day(2024, 1, 2))
	return instr, s.fixedKey(t, *s.parent.UserID, instr)
}

// TestSplitMatched checks that a split matches an action of equal ratio that a
// datasource dates on the Monday after the stated day.
func TestSplitMatched(t *testing.T) {
	s := newStack(t)
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

// TestStandingFindingsOnce checks that two fetch runs over one unhandled
// action and one unmatched split leave one finding of each. Only the first
// run counts them.
func TestStandingFindingsOnce(t *testing.T) {
	s := newStack(t)
	_, key := s.nvda(t)
	s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(market.CorpAct{ExDate: day(2024, 1, 10), Ratio: ratio(1, 1), Type: "spin_off"})
	found := func() map[string]int64 {
		out := map[string]int64{}
		for k, v := range metrictest.Counts(t, reader) {
			if strings.HasPrefix(k, "stonks.findings{") {
				out[k] = v
			}
		}
		return out
	}

	// The first collection drops counts that earlier tests left.
	found()
	s.fetch(t, nvda)
	want := map[string]int64{"stonks.findings{kind=unhandled_corp_act}": 1, "stonks.findings{kind=unmatched_split}": 1}
	if diff := cmp.Diff(want, found()); diff != "" {
		t.Errorf("findings counted by the first fetch mismatch (-want +got):\n%s", diff)
	}
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, nvda)
	if diff := cmp.Diff(map[string]int64{}, found()); diff != "" {
		t.Errorf("findings counted by the second fetch mismatch (-want +got):\n%s", diff)
	}
	if got := s.findings(t, us("NVDA")); len(got) != 1 {
		t.Errorf("unhandled corporate action findings = %+v, want one", got)
	}
	if got := s.unmatchedSplits(t); len(got) != 1 {
		t.Errorf("unmatched split findings = %+v, want one", got)
	}
}

var cmpIgnoreID = cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".id" }, cmp.Ignore())

// TestSplitRun checks that the finding goes on the run of the source's
// fetch, and that the stored answer of another datasource does not make a
// fetch cover the split.
func TestSplitRun(t *testing.T) {
	t.Run("the source covers", func(t *testing.T) {
		s := newStack(t)
		_, key := s.nvda(t)
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
		s.fetch(t, ticker("XNAS", "NVDA"))
		if got := s.unmatchedSplits(t); len(got) != 1 || got[0].datasource != s.alphaName {
			t.Errorf("findings = %+v, want one on %s's run", got, s.alphaName)
		}
	})
	t.Run("only a stored answer of another datasource covers", func(t *testing.T) {
		s := newStack(t)
		_, key := s.nvda(t)
		s.source(t, s.betaName)
		s.beta.Responses[ticker("XNAS", "NVDA")] = covering()
		s.fetch(t, ticker("XNAS", "NVDA"))
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.source(t, s.alphaName)
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
			instr := s.instr(t, types.AssetClassStock)
			s.names(t, us("NVDA"), instr, day(2024, 1, 2))
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
		instr, other := s.instr(t, types.AssetClassStock), s.instr(t, types.AssetClassStock)
		s.names(t, us("NVDA"), instr, day(2024, 6, 1))
		s.names(t, us("NVDA"), other, day(2024, 6, 9))
		key := s.fixedKey(t, *s.parent.UserID, instr)
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.alpha.Responses[ticker("XNAS", "NVDA")] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
		s.fetch(t, ticker("XNAS", "NVDA"))
		if got := s.unmatchedSplits(t); len(got) != 1 {
			t.Errorf("findings = %+v, want one", got)
		}
	})
	t.Run("an earlier ticker", func(t *testing.T) {
		s := newStack(t)
		instr := s.instr(t, types.AssetClassStock)
		s.names(t, us("OLD"), instr, day(2020, 1, 2))
		s.alpha.Responses[ticker("XNAS", "OLD")] = covering(reported(day(2024, 6, 10), ratio(1, 10)))
		s.fetch(t, ticker("XNAS", "OLD"))
		s.names(t, us("NEW"), instr, day(2024, 1, 2))
		key := s.fixedKey(t, *s.parent.UserID, instr)
		s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
		s.alpha.Responses[ticker("XNAS", "NEW")] = covering()
		s.now = answered.AddDate(0, 0, 1)
		s.fetch(t, ticker("XNAS", "NEW"))
		if got := s.unmatchedSplits(t); len(got) != 0 {
			t.Errorf("findings = %+v, want none", got)
		}
	})
}

// TestSplitOtherMarket checks that a split is not checked against the
// instrument's calendar when only the answer of a second market covers it.
func TestSplitOtherMarket(t *testing.T) {
	s := newStack(t)
	dbtest.CorpActSource(t, s.tx, "XTSE", s.betaName)
	instr := s.listed(t, types.AssetClassStock, map[string]*string{"USD": ptr.To("XNAS"), "CAD": nil})
	s.names(t, types.MarketTicker{Market: "XTSE", Value: "NVDA"}, instr, day(2024, 1, 2))
	key := s.fixedKey(t, *s.parent.UserID, instr)
	s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
	s.beta.Responses[ticker("XTSE", "NVDA")] = covering()
	s.fetch(t, ticker("XTSE", "NVDA"))
	if got := s.unmatchedSplits(t); len(got) != 0 {
		t.Errorf("findings = %+v, want none", got)
	}
}

// TestSplitClass checks that a stated split raises a finding only when
// corporate actions restate its instrument's class.
func TestSplitClass(t *testing.T) {
	tests := []struct {
		class types.AssetClass
		want  int
	}{
		{types.AssetClassOption, 0},
		{types.AssetClassFixedIncome, 0},
		{types.AssetClassMutualFund, 0},
		{types.AssetClassEtf, 1},
	}
	for _, tc := range tests {
		t.Run(string(tc.class), func(t *testing.T) {
			s := newStack(t)
			instr := s.instr(t, tc.class)
			s.names(t, us("NVDA"), instr, day(2024, 1, 2))
			key := s.fixedKey(t, *s.parent.UserID, instr)
			s.states(t, *s.parent.UserID, key, ptr.To(ratio(1, 10)))
			s.alpha.Responses[ticker("XNAS", "NVDA")] = covering()
			s.fetch(t, ticker("XNAS", "NVDA"))
			if got := s.unmatchedSplits(t); len(got) != tc.want {
				t.Errorf("findings = %+v, want %d", got, tc.want)
			}
		})
	}
}

// TestSplitUsers checks that a user's fetch checks only that user's splits,
// and that a system fetch checks every user's.
func TestSplitUsers(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	instr, mine := s.nvda(t)
	other := s.user(t)
	theirs := s.fixedKey(t, other, instr)
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

// TestSplitNearUnhandled checks that a split without a ratio matches an
// unhandled action within the window, on a day the answer still covers, and
// stops matching it once an administrator decides the action leaves the
// quantity held.
func TestSplitNearUnhandled(t *testing.T) {
	s := newStack(t)
	_, key := s.nvda(t)
	s.states(t, *s.parent.UserID, key, nil)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(market.CorpAct{ExDate: splitDay.AddDate(0, 0, 1), Ratio: ratio(1, 1), Type: "spin_off"})
	s.fetch(t, nvda)
	if got := s.unmatchedSplits(t); len(got) != 0 {
		t.Fatalf("findings = %+v, want none", got)
	}

	f := s.findings(t, us("NVDA"))
	require.Len(t, f, 1)
	s.decide(t, f[0].act, gen.CorpActDecisionSpinOffPriceFactor)
	s.now = answered.AddDate(0, 0, 1)
	s.fetch(t, nvda)
	if got := s.unmatchedSplits(t); len(got) != 1 || got[0].key != key {
		t.Errorf("findings after the decision = %+v, want one on the key", got)
	}
}

// TestListInstrActs checks that an instrument's actions come from every
// datasource under a ticker that named it on the ex-date and not from the
// days the ticker named another instrument, that only the market's source is
// applied, and that the evidence for one of them holds the other
// datasource's actions within the window and the distinct stated splits
// nearby, those filed under another ticker of the instrument included.
func TestListInstrActs(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	earlier := s.instr(t, types.AssetClassStock)
	s.names(t, us("NVDA"), earlier, day(2023, 1, 2))
	instr, key := s.nvda(t)
	user := *s.parent.UserID
	s.states(t, user, key, ptr.To(ratio(1, 10)))
	s.states(t, user, key, ptr.To(ratio(1, 10)))
	s.names(t, us("NVDB"), instr, day(2024, 1, 2))
	s.states(t, user, s.datedKey(t, "XNAS", "NVDB"), nil)
	nvda := ticker("XNAS", "NVDA")
	s.alpha.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)), reported(day(2023, 6, 1), ratio(1, 4)))
	s.fetch(t, nvda)
	s.source(t, s.betaName)
	s.beta.Responses[nvda] = covering(reported(day(2024, 6, 10), ratio(1, 10)), reported(day(2024, 6, 10+window+1), ratio(1, 3)))
	s.fetch(t, nvda)
	s.source(t, s.alphaName)

	acts, err := ListInstrActs(ctx, s.q, instr)
	require.NoError(t, err)
	type listed struct {
		Datasource, Day string
		Applied         bool
	}
	var got []listed
	for _, a := range acts {
		got = append(got, listed{a.Datasource, a.ExDate.Format(time.DateOnly), a.Applied})
	}
	want := []listed{{s.betaName, "2024-06-18", false}, {s.alphaName, "2024-06-10", true}, {s.betaName, "2024-06-10", false}}
	if diff := cmp.Diff(want, got); diff != "" {
		t.Errorf("actions mismatch (-want +got):\n%s", diff)
	}

	ev, err := ReadEvidence(ctx, s.q, acts[1].ID)
	require.NoError(t, err)
	if !ev.Act.Applied || len(ev.Others) != 1 || ev.Others[0].ID != acts[2].ID {
		t.Errorf("evidence = %+v, want the applied action with beta's same-day action as the only other", ev)
	}
	wantSplits := []StmtSplit{{EffectiveDate: splitDay, Ratio: ptr.To(ratio(1, 10))}, {EffectiveDate: splitDay}}
	if diff := cmp.Diff(wantSplits, ev.StmtSplits, cmpopts.EquateComparable(decimal.Decimal{})); diff != "" {
		t.Errorf("stated splits mismatch (-want +got):\n%s", diff)
	}
}

// TestReadEvidenceUnnamed checks that an action of a ticker that named
// nothing on its ex-date comes back alone, and that a missing action is not
// found.
func TestReadEvidenceUnnamed(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	f := s.unhandledNVDA(t)
	ev, err := ReadEvidence(ctx, s.q, f.act)
	require.NoError(t, err)
	if ev.Act.ID != f.act || ev.Act.Applied || len(ev.Others) != 0 || len(ev.StmtSplits) != 0 {
		t.Errorf("evidence = %+v, want the action alone, not applied", ev)
	}
	if _, err := ReadEvidence(ctx, s.q, db.NewID()); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("ReadEvidence of no action: err = %v, want ErrNotFound", err)
	}
}
