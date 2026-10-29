//go:build dbtest

package resolve

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// later is the day of the reassert tickers run, a month after the stack's
// first answers.
var later = on(2026, 4, 2)

// withoutFacts rebuilds the named datasources without identifier facts.
func (s *stack) withoutFacts(t *testing.T, names ...string) {
	t.Helper()
	for _, n := range names {
		s.identityOnly[n] = true
	}
	require.NoError(t, s.sources.Reload(context.Background()))
}

// trade records a transaction of key on day d, which makes the ticker that
// dates the key a held ticker.
func (s *stack) trade(t *testing.T, key gen.StatedKey, d time.Time) {
	t.Helper()
	_, err := s.q.CreateTxn(context.Background(), gen.CreateTxnParams{
		ID: db.NewID(), UserID: s.user.ID, Broker: gen.BrokerIbkr, StmtID: s.stmt.ID, StatedKeyID: key.ID,
		OrderDate: d, SettlementDate: d, AsAt: d, Quantity: decimal.NewFromInt(1),
	})
	require.NoError(t, err)
}

// reassertTickers asks again about the held tickers among vs, under a new
// system run on the day later.
func (s *stack) reassertTickers(t *testing.T, vs ...types.Identifier) (gen.Run, []WrittenIdentifierFacts) {
	t.Helper()
	return s.reassertTickersAt(t, later.Add(10*time.Hour), vs...)
}

// reassertTickersAt asks again about the held tickers among vs, under a new
// system run whose datasources answer at the moment at.
func (s *stack) reassertTickersAt(t *testing.T, at time.Time, vs ...types.Identifier) (gen.Run, []WrittenIdentifierFacts) {
	t.Helper()
	ctx := context.Background()
	s.now = at
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindReassertTickers, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	rows, err := s.q.ListHeldTickers(ctx)
	require.NoError(t, err)
	var held []gen.ListHeldTickersRow
	for _, r := range rows {
		if slices.Contains(vs, types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: r.Venue, Value: r.Value}) {
			held = append(held, r)
		}
	}
	require.Len(t, held, len(vs))
	written, err := s.reasserter.ReassertTickers(ctx, run, held)
	require.NoError(t, err)
	return run, written
}

// reassertedKey is one fetch key of a reassert tickers run, with its
// datasource.
type reassertedKey struct {
	datasource string
	kind       gen.FetchKind
	id         uuid.UUID
	fetch      uuid.UUID
	outcome    gen.FetchOutcome
	stated     *uuid.UUID
}

// reasserted returns the fetch keys of run's fetches, by datasource.
func (s *stack) reasserted(t *testing.T, run gen.Run) map[string][]reassertedKey {
	t.Helper()
	rows, err := s.tx.Query(context.Background(), `
		SELECT fetches.datasource, fetches.kind, fetch_keys.id, fetches.id, fetch_keys.outcome, fetch_keys.stated_key_id
		FROM fetch_keys
		JOIN fetches ON fetches.id = fetch_keys.fetch_id
		JOIN runs ON runs.id = fetches.id
		WHERE runs.parent_id = $1
		ORDER BY fetch_keys.id`, run.ID)
	require.NoError(t, err)
	defer rows.Close()
	out := map[string][]reassertedKey{}
	for rows.Next() {
		var k reassertedKey
		require.NoError(t, rows.Scan(&k.datasource, &k.kind, &k.id, &k.fetch, &k.outcome, &k.stated))
		out[k.datasource] = append(out[k.datasource], k)
	}
	require.NoError(t, rows.Err())
	return out
}

// assertedOn returns the instrument that each assertion of v holding day d
// names, or nil for a negative one.
func (s *stack) assertedOn(t *testing.T, v types.Identifier, d time.Time) []*uuid.UUID {
	t.Helper()
	rows, err := s.q.ListIdentifierAssertions(context.Background(), gen.ListIdentifierAssertionsParams{Markets: []string{mt(v).Market}, Values: []string{v.Value}})
	require.NoError(t, err)
	var out []*uuid.UUID
	for _, r := range rows {
		if ia := r.IdentifierAssertion; (types.Period{From: ia.AssertedFrom, Before: ia.AssertedBefore}).Contains(d) {
			out = append(out, r.IdentifierAssertion.InstrID)
		}
	}
	return out
}

// instrOf returns the instrument id names.
func (s *stack) instrOf(t *testing.T, id types.Identifier) uuid.UUID {
	t.Helper()
	row, err := s.q.FindIdentifier(context.Background(), gen.FindIdentifierParams{Type: id.Type, Domain: id.Domain, Value: id.Value})
	require.NoError(t, err)
	return row.Instr.ID
}

// TestReassertTickers covers a held ticker on XLON, which alpha declines and
// beta answers. The run asks both again although the instrument has identity
// coverage, and writes only an assertion dated on its own day. The assertion
// keeps the venue and the currency, GBX, that beta stated, and the run
// rebuilds the ticker's name with them.
func TestReassertTickers(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZR"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "GB00ZZR"+uuid.NewString()[:5])
	s.heldOnXLON(t, v, listing(types.AssetClassStock, "GBP", isin, v))
	instr := s.instrOf(t, isin)
	coverage := `SELECT count(*) FROM identity_coverage WHERE instr_id = $1`
	shape := `SELECT (SELECT count(*) FROM identifiers WHERE instr_id = $1) + (SELECT count(*) FROM listings WHERE instr_id = $1)`
	covered, rows := s.count(t, coverage, instr), s.count(t, shape, instr)
	require.Positive(t, covered)

	extra := id(types.IdentifierTypeIsin, "", "GB00ZZX"+uuid.NewString()[:5])
	s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{
		listing(types.AssetClassStock, "GBX", isin, v, id(types.IdentifierTypeMicTicker, "XLON", "ZZO"+uuid.NewString()[:4])),
		listing(types.AssetClassStock, "USD", extra, id(types.IdentifierTypeMicTicker, "XNAS", "ZZU"+uuid.NewString()[:4])),
	}}
	run, written := s.reassertTickers(t, v)

	keys := s.reasserted(t, run)
	if a := keys[s.alphaName]; len(a) != 1 || a[0].outcome != gen.FetchOutcomeNotServed || a[0].stated != nil {
		t.Errorf("alpha's keys = %+v, want one not served, naming no stated key", a)
	}
	b := keys[s.betaName]
	if len(b) != 1 || b[0].outcome != gen.FetchOutcomeServed || b[0].kind != gen.FetchKindIdentity {
		t.Fatalf("beta's keys = %+v, want one served identity key", b)
	}
	if got := s.assertedOn(t, v, later); len(got) != 1 || got[0] == nil || *got[0] != instr {
		t.Errorf("assertions of %s on %s = %v, want one naming %s", v.Name(), later.Format(time.DateOnly), got, instr)
	}
	stored, err := tickervalidity.Load(ctx, s.q, []types.MarketTicker{mt(v)})
	require.NoError(t, err)
	ivs := stored[mt(v)]
	if !confirmedOn(ivs, instr, on(2026, 3, 20)) {
		t.Errorf("ticker validity of %s = %+v, want %s confirmed between the two assertions", v.Name(), ivs, instr)
	}
	if got := s.count(t, coverage, instr); got != covered {
		t.Errorf("coverage rows = %d, want %d: extending ticker validity changes no coverage", got, covered)
	}
	if got := s.count(t, shape, instr); got != rows {
		t.Errorf("identifier and listing rows = %d, want %d: extending ticker validity writes only assertions", got, rows)
	}
	if n := s.count(t, `SELECT count(*) FROM identifiers WHERE value = $1`, extra.Value); n != 0 {
		t.Errorf("rows of %s = %d, want none", extra.Name(), n)
	}
	if len(written) != 1 || written[0].Run.ID != b[0].fetch || !slices.Equal(written[0].Tickers, []types.MarketTicker{mt(v)}) {
		t.Errorf("written = %+v, want %s under beta's datasource fetch run %s", written, v.Name(), b[0].fetch)
	}
	if diff := cmp.Diff([]string{v.Value + " at XLON in GBX"}, s.names(t, instr)); diff != "" {
		t.Errorf("names mismatch (-want +got):\n%s", diff)
	}
}

// TestReassertTickersUnknownCurrency checks that an answer in a currency the
// system lacks, such as ZAC, completes the run. The assertion that the run
// writes has a null currency.
func TestReassertTickersUnknownCurrency(t *testing.T) {
	s := newStack(t)
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZC"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "GB00ZZC"+uuid.NewString()[:5])
	s.heldOnXLON(t, v, listing(types.AssetClassStock, "GBP", isin, v))
	instr := s.instrOf(t, isin)
	s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "ZAC", isin, v)}}
	s.reassertTickers(t, v)
	if got := s.assertedOn(t, v, later); len(got) != 1 || got[0] == nil || *got[0] != instr {
		t.Errorf("assertions of %s on %s = %v, want one naming %s", v.Name(), later.Format(time.DateOnly), got, instr)
	}
	n := s.count(t, `SELECT count(*) FROM identifier_assertions JOIN tickers ON tickers.id = identifier_assertions.ticker_id
		WHERE tickers.value = $1 AND identifier_assertions.currency IS NULL`, v.Value)
	if n != 1 {
		t.Errorf("assertions of %s without a currency = %d, want 1", v.Name(), n)
	}
}

// TestReassertTickersAnswers checks what each kind of identity answer asserts.
func TestReassertTickersAnswers(t *testing.T) {
	tests := []struct {
		name string
		// answer is beta's answer to the reassert tickers run, given the held
		// ticker.
		answer func(v types.Identifier) market.IdentityResult
		// want reports whether the assertions on the day are as expected.
		want func(got []*uuid.UUID) bool
	}{
		{
			name: "an answer that lists the ticker nowhere asserts nothing",
			answer: func(types.Identifier) market.IdentityResult {
				return market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", id(types.IdentifierTypeIsin, "", "GB00ZZN"+uuid.NewString()[:5]))}}
			},
			want: func(got []*uuid.UUID) bool { return len(got) == 0 },
		},
		{
			name: "an answer whose stable identifiers name no stored instrument asserts nothing",
			answer: func(v types.Identifier) market.IdentityResult {
				return market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", id(types.IdentifierTypeIsin, "", "GB00ZZM"+uuid.NewString()[:5]), v)}}
			},
			want: func(got []*uuid.UUID) bool { return len(got) == 0 },
		},
		{
			name: "an answer that carries only the ticker asserts nothing",
			answer: func(v types.Identifier) market.IdentityResult {
				return market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", v)}}
			},
			want: func(got []*uuid.UUID) bool { return len(got) == 0 },
		},
		{
			name:   "an unknown ticker is asserted to name nothing",
			answer: func(types.Identifier) market.IdentityResult { return market.IdentityResult{Unknown: true} },
			want:   func(got []*uuid.UUID) bool { return len(got) == 1 && got[0] == nil },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			v := id(types.IdentifierTypeMicTicker, "XLON", "ZZA"+uuid.NewString()[:4])
			s.heldOnXLON(t, v, listing(types.AssetClassStock, "GBP", id(types.IdentifierTypeIsin, "", "GB00ZZA"+uuid.NewString()[:5]), v))
			s.beta.Responses[v] = tc.answer(v)
			s.reassertTickers(t, v)
			if got := s.assertedOn(t, v, later); !tc.want(got) {
				t.Errorf("assertions of %s on %s = %v", v.Name(), later.Format(time.DateOnly), got)
			}
		})
	}
}

// TestReassertTickersDisagreement checks that two answers naming different
// stored instruments on the day of a reassert tickers run are a finding of that
// run.
func TestReassertTickersDisagreement(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZD"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "GB00ZZD"+uuid.NewString()[:5])
	s.heldOnXLON(t, v, listing(types.AssetClassStock, "GBP", isin, v))
	other := id(types.IdentifierTypeIsin, "", "GB00ZZE"+uuid.NewString()[:5])
	instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	_, err = s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, Type: other.Type, Value: other.Value})
	require.NoError(t, err)

	s.script.Declines = nil
	s.script.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", other, v)}}
	run, _ := s.reassertTickers(t, v)
	rows, err := s.q.ListRunFindings(ctx, []uuid.UUID{run.ID})
	require.NoError(t, err)
	var kinds []gen.FindingKind
	for _, r := range rows {
		kinds = append(kinds, r.Finding.Kind)
	}
	if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindIdentifierDisagreement}, kinds); diff != "" {
		t.Errorf("findings of the reassert tickers run mismatch (-want +got):\n%s", diff)
	}
}

// TestReassertTickersReanswer checks that a later answer of one datasource on
// the day of an earlier one replaces it. The later run leaves its disagreement
// findings empty, and ticker validity follows the later answer. Two answers at
// one moment are ordered by their fetch keys.
func TestReassertTickersReanswer(t *testing.T) {
	tests := []struct {
		name string
		// gap is the time between the two runs.
		gap time.Duration
		// unknown makes beta's second answer unknown. Otherwise it names
		// another stored instrument.
		unknown bool
	}{
		{name: "a later answer names another instrument", gap: 4 * time.Hour},
		{name: "a later answer finds the ticker unknown", gap: 4 * time.Hour, unknown: true},
		{name: "a second answer at the same moment names another instrument"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			ctx := context.Background()
			v := id(types.IdentifierTypeMicTicker, "XLON", "ZZQ"+uuid.NewString()[:4])
			isin := id(types.IdentifierTypeIsin, "", "GB00ZZQ"+uuid.NewString()[:5])
			s.heldOnXLON(t, v, listing(types.AssetClassStock, "GBP", isin, v))
			other := id(types.IdentifierTypeIsin, "", "GB00ZZP"+uuid.NewString()[:5])
			instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
			require.NoError(t, err)
			_, err = s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, Type: other.Type, Value: other.Value})
			require.NoError(t, err)

			first := later.Add(10 * time.Hour)
			s.reassertTickersAt(t, first, v)
			s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", other, v)}}
			if tc.unknown {
				s.beta.Responses[v] = market.IdentityResult{Unknown: true}
			}
			run, _ := s.reassertTickersAt(t, first.Add(tc.gap), v)

			rows, err := s.q.ListRunFindings(ctx, []uuid.UUID{run.ID})
			require.NoError(t, err)
			for _, r := range rows {
				if r.Finding.Kind == gen.FindingKindIdentifierDisagreement {
					t.Errorf("the later run raised %s: %s", r.Finding.Kind, *r.Finding.Detail)
				}
			}
			stored, err := tickervalidity.Load(ctx, s.q, []types.MarketTicker{mt(v)})
			require.NoError(t, err)
			ivs := stored[mt(v)]
			i := slices.IndexFunc(ivs, func(iv tickervalidity.Interval) bool { return iv.Contains(later) })
			switch {
			case tc.unknown && i >= 0:
				t.Errorf("ticker validity on %s = %+v, want none", later.Format(time.DateOnly), ivs[i])
			case !tc.unknown && (i < 0 || ivs[i].Instr != instr.ID):
				t.Errorf("ticker validity of %s = %+v, want %s on %s", v.Name(), ivs, instr.ID, later.Format(time.DateOnly))
			}
		})
	}
}

// TestReassertTickersHistory checks that reasserting tickers asks a datasource
// that serves identifier facts for the history of the ticker's instrument
// under its stable identifier, and not for the ticker's identity.
func TestReassertTickersHistory(t *testing.T) {
	s := newStack(t)
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZH"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "US00ZZH"+uuid.NewString()[:5])
	s.script.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "USD", isin, v)}}
	key := s.state(t, types.AssetClassStock, "USD", v)
	s.trade(t, key, on(2026, 1, 5))
	s.resolveOn(t, []time.Time{on(2026, 1, 5)}, key)
	require.True(t, s.datedThrough(t, s.key(t, key.ID), v, gen.ArbiterDatasource))
	s.alphaFacts.Sent = nil

	run, _ := s.reassertTickers(t, v)
	want := []market.IdentifierFactRequest{{Identifier: isin, Period: market.History(window{since}, s.now)}}
	if diff := cmp.Diff(want, s.alphaFacts.Sent); diff != "" {
		t.Errorf("identifier facts requests mismatch (-want +got):\n%s", diff)
	}
	for _, k := range s.reasserted(t, run)[s.alphaName] {
		if k.kind == gen.FetchKindIdentity {
			t.Errorf("alpha's key %+v is an identity request, want only the history", k)
		}
	}
}

// TestReassertTickersRefusesRun checks that ReassertTickers refuses a run that
// is not a system reassert tickers run.
func TestReassertTickersRefusesRun(t *testing.T) {
	s := newStack(t)
	if _, err := s.reasserter.ReassertTickers(context.Background(), s.resolution, nil); err == nil {
		t.Error("ReassertTickers under a user's resolve statement keys run succeeded, want an error")
	}
}

// heldAtTwoVenues holds one ticker through two keys, one stated at XNYS and
// one at XNAS, both dated to the instrument of isin. Beta answers the first
// key, and the second is dated from the facts of the market.
func (s *stack) heldAtTwoVenues(t *testing.T, value string, isin types.Identifier) (nyse, nasdaq types.Identifier) {
	t.Helper()
	nyse = id(types.IdentifierTypeMicTicker, "XNYS", value)
	nasdaq = id(types.IdentifierTypeMicTicker, "XNAS", value)
	s.enableBeta(t)
	s.withoutFacts(t, s.alphaName, s.betaName)
	s.script.Declines = []string{"XNYS", "XNAS"}
	s.beta.Responses[nyse] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "USD", isin, nyse)}}
	for _, v := range []types.Identifier{nyse, nasdaq} {
		key := s.state(t, types.AssetClassStock, "USD", v)
		s.trade(t, key, on(2026, 1, 5))
		got := s.resolveOn(t, []time.Time{on(2026, 1, 5)}, key)
		require.Equal(t, gen.ResolutionOutcomeMatched, got[0].Outcome)
	}
	return nyse, nasdaq
}

// TestReassertTickersAcrossVenues checks that keys held at two venues of one
// market make one identity fetch, and that an answer listing the ticker at
// another venue of the market asserts the held instrument once, at the
// market.
func TestReassertTickersAcrossVenues(t *testing.T) {
	s := newStack(t)
	isin := id(types.IdentifierTypeIsin, "", "US00ZZV"+uuid.NewString()[:5])
	nyse, nasdaq := s.heldAtTwoVenues(t, "ZZV"+uuid.NewString()[:4], isin)
	instr := s.instrOf(t, isin)
	moved := market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "USD", isin, nasdaq)}}
	s.beta.Responses[nyse], s.beta.Responses[nasdaq] = moved, moved

	run, _ := s.reassertTickers(t, nyse, nasdaq)
	if keys := s.reasserted(t, run)[s.betaName]; len(keys) != 1 {
		t.Errorf("beta's fetch keys = %+v, want one for the market ticker", keys)
	}
	if got := s.assertedOn(t, nyse, later); len(got) != 1 || got[0] == nil || *got[0] != instr {
		t.Errorf("assertions of %s on %s = %v, want one naming %s", mt(nyse).Name(), later.Format(time.DateOnly), got, instr)
	}
}

// TestReassertTickersOtherMarket checks that an answer listing the held
// ticker only in another market asserts nothing.
func TestReassertTickersOtherMarket(t *testing.T) {
	s := newStack(t)
	isin := id(types.IdentifierTypeIsin, "", "US00ZZW"+uuid.NewString()[:5])
	value := "ZZW" + uuid.NewString()[:4]
	nyse, nasdaq := s.heldAtTwoVenues(t, value, isin)
	elsewhere := market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", isin, id(types.IdentifierTypeMicTicker, "XLON", value))}}
	s.beta.Responses[nyse], s.beta.Responses[nasdaq] = elsewhere, elsewhere

	s.reassertTickers(t, nyse, nasdaq)
	if got := s.assertedOn(t, nyse, later); len(got) != 0 {
		t.Errorf("assertions of %s on %s = %v, want none", mt(nyse).Name(), later.Format(time.DateOnly), got)
	}
}
