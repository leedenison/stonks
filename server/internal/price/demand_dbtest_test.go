//go:build dbtest

package price

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
)

// today is the day of answered, the moment of the pinned clock. The last
// completed day is the day before it.
var today = types.Day(answered)

// gapsOf returns the gaps of the demand for user, by listing, after putting
// every currency in use. A test asserts on its own listings alone.
func (s *stack) gapsOf(t *testing.T, user *uuid.UUID) (map[uuid.UUID]Gap, []uuid.UUID) {
	t.Helper()
	d := NewDemand(s.q, s.families, func() time.Time { return answered })
	require.NoError(t, d.UseCurrencies(context.Background()))
	gaps, unasked, err := d.Gaps(context.Background(), user)
	require.NoError(t, err)
	out := map[uuid.UUID]Gap{}
	for _, g := range gaps {
		out[g.Listing] = g
	}
	return out, unasked
}

// ticker gives the listing of instr in each of currencies a current ticker,
// so its gaps can be asked. The first is at XNAS and the second at XNYS.
func (s *stack) ticker(t *testing.T, instr uuid.UUID, currencies ...string) string {
	t.Helper()
	value := "T" + strings.ToUpper(strings.ReplaceAll(uuid.NewString(), "-", "")[:10])
	for i, c := range currencies {
		s.resolves(t, instr, "US", value, []string{"XNAS", "XNYS"}[i], c)
	}
	return value
}

// TestGapsListings checks which listings each kind of key demands, from a
// week before its first transaction on a Saturday to the last completed day.
func TestGapsListings(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	saturday := day(2026, 2, 28)
	want := []types.Period{span(day(2026, 2, 21), today)}

	stock, listings := s.instr(t, types.AssetClassMutualFund, "USD", "GBP")
	s.ticker(t, stock, "USD")
	usd := listings["USD"]
	s.txn(t, h, s.fixedKey(t, h, stock, &usd), saturday, "10")

	both, bothListings := s.instr(t, types.AssetClassMutualFund, "USD", "EUR")
	s.ticker(t, both, "USD")
	s.txn(t, h, s.fixedKey(t, h, both, nil), saturday, "10")

	option, optionListings := s.instr(t, types.AssetClassOption, "USD")
	s.txn(t, h, s.fixedKey(t, h, option, nil), saturday, "1")
	s.txn(t, h, s.looseKey(t, h), saturday, "1")

	gaps, unasked := s.gapsOf(t, nil)
	if diff := cmp.Diff(want, gaps[usd].Periods); diff != "" {
		t.Errorf("gap of the listing a fixed key names mismatch (-want +got):\n%s", diff)
	}
	if _, ok := gaps[listings["GBP"]]; ok || contains(unasked, listings["GBP"]) {
		t.Error("the other listing of the instrument is demanded, want the named listing alone")
	}
	if diff := cmp.Diff(want, gaps[bothListings["USD"]].Periods); diff != "" {
		t.Errorf("gap of a listing under a key that names only the instrument mismatch (-want +got):\n%s", diff)
	}
	if !contains(unasked, bothListings["EUR"]) {
		t.Error("the listing without a current ticker is not in unasked, want it there with its gap")
	}
	if _, ok := gaps[optionListings["USD"]]; ok || contains(unasked, optionListings["USD"]) {
		t.Error("an option is demanded, want none")
	}
}

// TestGapsDatedKey checks that a key dated through a ticker demands the
// listing that the ticker reaches at the key's venue, under the ticker.
func TestGapsDatedKey(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	h := s.holder(t)
	instr, listings := s.instr(t, types.AssetClassMutualFund, "USD", "GBP")
	value := s.ticker(t, instr, "USD")
	ticker := dbtestTicker(t, s, "US", value)
	require.NoError(t, s.q.CreateTickerValidity(ctx, gen.CreateTickerValidityParams{
		TickerIds: []uuid.UUID{ticker}, Markets: []string{"US"}, InstrIds: []uuid.UUID{instr},
		ValidFroms: []string{""}, ValidBefores: []string{""}, Validities: []string{"confirmed"},
	}))
	key := db.NewID()
	_, err := s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, identifiers, via_ticker_id, via_market, via_venue, arbiter)
		VALUES ($1, $2, jsonb_build_array(jsonb_build_object('type', 'mic_ticker', 'domain', 'XNAS', 'value', $3::text)), $4, 'US', 'XNAS', 'datasource')`, key, h.user, value, ticker)
	require.NoError(t, err)
	s.txn(t, h, key, day(2026, 3, 2), "5")

	gaps, unasked := s.gapsOf(t, nil)
	got := gaps[listings["USD"]]
	if diff := cmp.Diff([]types.Period{span(day(2026, 2, 23), today)}, got.Periods); diff != "" {
		t.Errorf("gap mismatch (-want +got):\n%s", diff)
	}
	if want := (types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: value}); got.Identifier != want {
		t.Errorf("identifier = %v, want %v", got.Identifier, want)
	}
	if _, ok := gaps[listings["GBP"]]; ok || contains(unasked, listings["GBP"]) {
		t.Error("the listing the ticker does not reach is demanded, want the USD listing alone")
	}
}

// TestGapsAcrossUsers checks that the demand of a listing joins the days of
// every user, that a closed holding ends on its last day, and that the demand
// for one user leaves out what only another holds.
func TestGapsAcrossUsers(t *testing.T) {
	s := newStack(t)
	a, b := s.holder(t), s.holder(t)
	shared, sharedListings := s.instr(t, types.AssetClassMutualFund, "USD")
	s.ticker(t, shared, "USD")
	usd := sharedListings["USD"]
	keyA := s.fixedKey(t, a, shared, &usd)
	s.txn(t, a, keyA, day(2026, 1, 12), "10")
	s.txn(t, a, keyA, day(2026, 1, 20), "-10")
	s.txn(t, b, s.fixedKey(t, b, shared, &usd), day(2026, 1, 15), "3")
	other, otherListings := s.instr(t, types.AssetClassMutualFund, "USD")
	s.ticker(t, other, "USD")
	alone := otherListings["USD"]
	keyB := s.fixedKey(t, b, other, &alone)
	s.txn(t, b, keyB, day(2026, 2, 2), "1")
	s.txn(t, b, keyB, day(2026, 2, 4), "-1")

	gaps, _ := s.gapsOf(t, nil)
	if diff := cmp.Diff([]types.Period{span(day(2026, 1, 5), today)}, gaps[usd].Periods); diff != "" {
		t.Errorf("gap of the shared listing mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]types.Period{span(day(2026, 1, 26), day(2026, 2, 5))}, gaps[alone].Periods); diff != "" {
		t.Errorf("gap of the closed holding mismatch (-want +got):\n%s", diff)
	}
	gaps, _ = s.gapsOf(t, &a.user)
	if _, ok := gaps[alone]; ok {
		t.Error("the demand for one user holds a listing that only another holds")
	}
	if diff := cmp.Diff([]types.Period{span(day(2026, 1, 5), today)}, gaps[usd].Periods); diff != "" {
		t.Errorf("gap of the shared listing for one user mismatch (-want +got):\n%s", diff)
	}
}

// TestGapsRates checks that the currency of a held listing and the currency
// of a cash holding are each demanded against USD over one span, from a week
// before the first transaction of any user, and that USD itself is not. The
// demand for one user holds every rate.
func TestGapsRates(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	h, bystander := s.holder(t), s.holder(t)
	fund, listings := s.instr(t, types.AssetClassMutualFund, "GBP", "USD")
	s.ticker(t, fund, "GBP", "USD")
	gbp, usd := listings["GBP"], listings["USD"]
	s.txn(t, h, s.fixedKey(t, h, fund, &gbp), day(2026, 2, 2), "10")
	s.txn(t, h, s.fixedKey(t, h, fund, &usd), day(2026, 3, 2), "10")
	cash := db.NewID()
	_, err := s.tx.Exec(ctx, `INSERT INTO stated_keys (id, user_id, currency, identifiers, instr_id, via_identifier_id, arbiter)
		SELECT $1, $2, 'EUR', jsonb_build_array(jsonb_build_object('type', 'currency', 'value', 'EUR')), instr_id, id, 'stated'
		FROM identifiers WHERE type = 'currency' AND value = 'EUR'`, cash, h.user)
	require.NoError(t, err)
	s.txn(t, h, cash, day(2026, 3, 3), "250")

	d := NewDemand(s.q, s.families, func() time.Time { return answered })
	if _, _, err := d.Gaps(ctx, nil); err == nil || !strings.Contains(err.Error(), "EUR") {
		t.Errorf("Gaps() before UseCurrencies error = %v, want one that names EUR", err)
	}
	gaps, _ := s.gapsOf(t, &bystander.user)
	s.gapsOf(t, nil)
	rates, err := s.q.ListRateListings(ctx)
	require.NoError(t, err)
	got := map[string][]types.Period{}
	for _, r := range rates {
		if g, ok := gaps[r.ID]; ok {
			got[r.Code] = g.Periods
			if want := (types.Identifier{Type: types.IdentifierTypeCurrency, Value: r.Code}); g.Identifier != want {
				t.Errorf("identifier of the %s rate = %v, want %v", r.Code, g.Identifier, want)
			}
		}
	}
	whole := []types.Period{span(day(2026, 1, 26), today)}
	if diff := cmp.Diff(map[string][]types.Period{"EUR": whole, "GBP": whole}, got); diff != "" {
		t.Errorf("rate gaps mismatch (-want +got):\n%s", diff)
	}
	if len(rates) != 2 {
		t.Errorf("%d rate listings after two calls of UseCurrencies, want 2", len(rates))
	}
}

// TestGapsAfterCoverage checks that a gap is a day that every datasource
// leaves uncovered. A day that one datasource covers drops out of the gap for
// the others, and a listing covered whole has none.
func TestGapsAfterCoverage(t *testing.T) {
	s := newStack(t)
	h := s.holder(t)
	instr, listings := s.instr(t, types.AssetClassMutualFund, "USD")
	s.ticker(t, instr, "USD")
	usd := listings["USD"]
	s.txn(t, h, s.fixedKey(t, h, instr, &usd), day(2026, 3, 2), "10")
	s.record(t, s.answer(t, s.beta, usd, "USD", span(day(2026, 2, 26), day(2026, 3, 5))))

	gaps, _ := s.gapsOf(t, nil)
	want := []types.Period{span(day(2026, 2, 23), day(2026, 2, 26)), span(day(2026, 3, 5), today)}
	if diff := cmp.Diff(want, gaps[usd].Periods); diff != "" {
		t.Errorf("gap mismatch (-want +got):\n%s", diff)
	}
	s.record(t, s.answer(t, s.alpha, usd, "USD", span(day(2026, 2, 1), day(2026, 2, 26))))
	s.record(t, s.answer(t, s.alpha, usd, "USD", span(day(2026, 3, 5), today)))
	if gaps, _ := s.gapsOf(t, nil); len(gaps[usd].Periods) != 0 {
		t.Errorf("gap = %v after every day is covered, want none", gaps[usd].Periods)
	}
}

func contains(ids []uuid.UUID, id uuid.UUID) bool {
	for _, i := range ids {
		if i == id {
			return true
		}
	}
	return false
}

// dbtestTicker returns the id of the ticker value at market.
func dbtestTicker(t *testing.T, s *stack, market, value string) uuid.UUID {
	t.Helper()
	rows, err := s.q.ListTickers(context.Background(), gen.ListTickersParams{Markets: []string{market}, Values: []string{value}})
	require.NoError(t, err)
	require.Len(t, rows, 1)
	return rows[0].ID
}
