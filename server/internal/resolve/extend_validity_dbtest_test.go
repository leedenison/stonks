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
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// later is the day of the extend validity run, a month after the stack's
// first answers.
var later = on(2026, 4, 2)

// withoutEvents rebuilds the named datasources without identifier events.
func (s *stack) withoutEvents(t *testing.T, names ...string) {
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
	_, err := s.q.CreateTransaction(context.Background(), gen.CreateTransactionParams{
		ID: db.NewID(), UserID: s.user.ID, Broker: gen.BrokerIbkr, StatementID: s.statement.ID, StatedKeyID: key.ID,
		OrderDate: d, SettlementDate: d, AsAt: d, Quantity: decimal.NewFromInt(1),
	})
	require.NoError(t, err)
}

// extendValidity extends the validity of the held tickers among vs, under a new
// system run on the day later.
func (s *stack) extendValidity(t *testing.T, vs ...types.Identifier) (gen.Run, []WrittenIdentifierFacts) {
	t.Helper()
	ctx := context.Background()
	s.now = later.Add(10 * time.Hour)
	run, err := s.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindExtendValidity, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	rows, err := s.q.ListHeldTickers(ctx)
	require.NoError(t, err)
	var held []gen.ListHeldTickersRow
	for _, r := range rows {
		if slices.Contains(vs, types.Identifier{Type: r.Type, Domain: r.Domain, Value: r.Value}) {
			held = append(held, r)
		}
	}
	require.Len(t, held, len(vs))
	written, err := s.extender.ExtendValidity(ctx, run, held)
	require.NoError(t, err)
	return run, written
}

// extendedKey is one fetch key of an extend validity run, with its datasource.
type extendedKey struct {
	datasource string
	kind       gen.FetchKind
	id         uuid.UUID
	fetch      uuid.UUID
	outcome    gen.FetchOutcome
	stated     *uuid.UUID
}

// extended returns the fetch keys of run's fetches, by datasource.
func (s *stack) extended(t *testing.T, run gen.Run) map[string][]extendedKey {
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
	out := map[string][]extendedKey{}
	for rows.Next() {
		var k extendedKey
		require.NoError(t, rows.Scan(&k.datasource, &k.kind, &k.id, &k.fetch, &k.outcome, &k.stated))
		out[k.datasource] = append(out[k.datasource], k)
	}
	require.NoError(t, rows.Err())
	return out
}

// assertedOn returns the instruments the assertions of v on day d name, a
// nil for each negative one.
func (s *stack) assertedOn(t *testing.T, v types.Identifier, d time.Time) []*uuid.UUID {
	t.Helper()
	rows, err := s.q.ListIdentifierAssertions(context.Background(), gen.ListIdentifierAssertionsParams{Type: v.Type, Domain: v.Domain, Value: v.Value})
	require.NoError(t, err)
	var out []*uuid.UUID
	for _, r := range rows {
		if r.AssertedOn.Equal(d) {
			out = append(out, r.InstrumentID)
		}
	}
	return out
}

// instrumentOf returns the instrument id names.
func (s *stack) instrumentOf(t *testing.T, id types.Identifier) uuid.UUID {
	t.Helper()
	row, err := s.q.FindIdentifier(context.Background(), gen.FindIdentifierParams{Type: id.Type, Domain: id.Domain, Value: id.Value})
	require.NoError(t, err)
	return row.Instrument.ID
}

// count runs a query that counts rows.
func (s *stack) count(t *testing.T, sql string, args ...any) int {
	t.Helper()
	var n int
	require.NoError(t, s.tx.QueryRow(context.Background(), sql, args...).Scan(&n))
	return n
}

// heldOnXLON returns a key held through v, a ticker on XLON that alpha
// declines and beta resolves to l, with both datasources serving identity
// alone.
func heldOnXLON(t *testing.T, s *stack, v types.Identifier, l market.Listing) gen.StatedKey {
	t.Helper()
	s.enableBeta(t)
	s.withoutEvents(t, s.alphaName, s.betaName)
	s.script.Declines = []string{"XLON"}
	s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{l}}
	key := s.state(t, types.AssetClassStock, "GBP", v)
	s.trade(t, key, on(2026, 1, 5))
	got := s.resolveOn(t, []time.Time{on(2026, 1, 5)}, key)
	require.Equal(t, gen.ResolutionOutcomeMatched, got[0].Outcome)
	require.True(t, datedThrough(s.key(t, key.ID), v, gen.ArbiterDatasource))
	return key
}

// TestExtendValidity covers a held ticker on XLON, which alpha declines and
// beta answers. The run asks both again although the instrument has identity
// coverage, and writes only an assertion dated on its own day.
func TestExtendValidity(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZR"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "GB00ZZR"+uuid.NewString()[:5])
	heldOnXLON(t, s, v, listing(types.AssetClassStock, "GBP", isin, v))
	inst := s.instrumentOf(t, isin)
	coverage := `SELECT count(*) FROM identity_coverage WHERE instrument_id = $1`
	shape := `SELECT (SELECT count(*) FROM identifiers WHERE instrument_id = $1) + (SELECT count(*) FROM listings WHERE instrument_id = $1)`
	covered, rows := s.count(t, coverage, inst), s.count(t, shape, inst)
	require.Positive(t, covered)

	extra := id(types.IdentifierTypeIsin, "", "GB00ZZX"+uuid.NewString()[:5])
	s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{
		listing(types.AssetClassStock, "GBP", isin, v, id(types.IdentifierTypeMicTicker, "XLON", "ZZO"+uuid.NewString()[:4])),
		listing(types.AssetClassStock, "USD", extra, id(types.IdentifierTypeMicTicker, "XNAS", "ZZU"+uuid.NewString()[:4])),
	}}
	run, written := s.extendValidity(t, v)

	keys := s.extended(t, run)
	if a := keys[s.alphaName]; len(a) != 1 || a[0].outcome != gen.FetchOutcomeNotServed || a[0].stated != nil {
		t.Errorf("alpha's keys = %+v, want one not served, naming no stated key", a)
	}
	b := keys[s.betaName]
	if len(b) != 1 || b[0].outcome != gen.FetchOutcomeServed || b[0].kind != gen.FetchKindIdentity {
		t.Fatalf("beta's keys = %+v, want one served identity key", b)
	}
	if got := s.assertedOn(t, v, later); len(got) != 1 || got[0] == nil || *got[0] != inst {
		t.Errorf("assertions of %s on %s = %v, want one naming %s", v.Name(), later.Format(time.DateOnly), got, inst)
	}
	ivs, err := validity.Of(ctx, s.q, v)
	require.NoError(t, err)
	if !confirmedOn(ivs, inst, on(2026, 3, 20)) {
		t.Errorf("validity of %s = %+v, want %s confirmed between the two assertions", v.Name(), ivs, inst)
	}
	if got := s.count(t, coverage, inst); got != covered {
		t.Errorf("coverage rows = %d, want %d: extending validity changes no coverage", got, covered)
	}
	if got := s.count(t, shape, inst); got != rows {
		t.Errorf("identifier and listing rows = %d, want %d: extending validity writes only assertions", got, rows)
	}
	if n := s.count(t, `SELECT count(*) FROM identifiers WHERE value = $1`, extra.Value); n != 0 {
		t.Errorf("rows of %s = %d, want none", extra.Name(), n)
	}
	if len(written) != 1 || written[0].Run.ID != b[0].fetch || !slices.Equal(written[0].Identifiers, []types.Identifier{v}) {
		t.Errorf("written = %+v, want %s under beta's fetch run %s", written, v.Name(), b[0].fetch)
	}
}

// TestExtendValiditySharedIdentifier checks that a listing carrying the ticker
// and a datasource ticker of the instrument, but no stable identifier, makes
// the assertion name the instrument that the ticker's validity names.
func TestExtendValiditySharedIdentifier(t *testing.T) {
	s := newStack(t)
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZS"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "GB00ZZS"+uuid.NewString()[:5])
	ds := id(types.IdentifierTypeDatasourceTicker, "eodhd", v.Value+".LSE")
	heldOnXLON(t, s, v, listing(types.AssetClassStock, "GBP", isin, ds, v))
	inst := s.instrumentOf(t, isin)

	s.beta.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", ds, v)}}
	s.extendValidity(t, v)
	if got := s.assertedOn(t, v, later); len(got) != 1 || got[0] == nil || *got[0] != inst {
		t.Errorf("assertions of %s on %s = %v, want one naming %s", v.Name(), later.Format(time.DateOnly), got, inst)
	}
}

// TestExtendValidityAnswers checks what each kind of identity answer asserts.
func TestExtendValidityAnswers(t *testing.T) {
	tests := []struct {
		name string
		// answer is beta's answer to the extend validity run, given the held
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
			name:   "an unknown ticker is asserted to name nothing",
			answer: func(types.Identifier) market.IdentityResult { return market.IdentityResult{Unknown: true} },
			want:   func(got []*uuid.UUID) bool { return len(got) == 1 && got[0] == nil },
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newStack(t)
			v := id(types.IdentifierTypeMicTicker, "XLON", "ZZA"+uuid.NewString()[:4])
			heldOnXLON(t, s, v, listing(types.AssetClassStock, "GBP", id(types.IdentifierTypeIsin, "", "GB00ZZA"+uuid.NewString()[:5]), v))
			s.beta.Responses[v] = tc.answer(v)
			s.extendValidity(t, v)
			if got := s.assertedOn(t, v, later); !tc.want(got) {
				t.Errorf("assertions of %s on %s = %v", v.Name(), later.Format(time.DateOnly), got)
			}
		})
	}
}

// TestExtendValidityDisagreement checks that two answers naming different
// stored instruments on the day of an extend validity run are a finding of that
// run.
func TestExtendValidityDisagreement(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZD"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "GB00ZZD"+uuid.NewString()[:5])
	heldOnXLON(t, s, v, listing(types.AssetClassStock, "GBP", isin, v))
	other := id(types.IdentifierTypeIsin, "", "GB00ZZE"+uuid.NewString()[:5])
	inst, err := s.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	_, err = s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: inst.ID, Type: other.Type, Value: other.Value})
	require.NoError(t, err)

	s.script.Declines = nil
	s.script.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "GBP", other, v)}}
	run, _ := s.extendValidity(t, v)
	rows, err := s.q.ListRunFindings(ctx, []uuid.UUID{run.ID})
	require.NoError(t, err)
	var kinds []gen.FindingKind
	for _, r := range rows {
		kinds = append(kinds, r.Finding.Kind)
	}
	if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindDisagreement}, kinds); diff != "" {
		t.Errorf("findings of the extend validity run mismatch (-want +got):\n%s", diff)
	}
}

// TestExtendValidityHistory checks that extending validity asks a datasource
// that serves identifier events for the history of the ticker's instrument
// under its stable identifier, and not for the ticker's identity.
func TestExtendValidityHistory(t *testing.T) {
	s := newStack(t)
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZH"+uuid.NewString()[:4])
	isin := id(types.IdentifierTypeIsin, "", "US00ZZH"+uuid.NewString()[:5])
	s.script.Responses[v] = market.IdentityResult{Listings: []market.Listing{listing(types.AssetClassStock, "USD", isin, v)}}
	key := s.state(t, types.AssetClassStock, "USD", v)
	s.trade(t, key, on(2026, 1, 5))
	s.resolveOn(t, []time.Time{on(2026, 1, 5)}, key)
	require.True(t, datedThrough(s.key(t, key.ID), v, gen.ArbiterDatasource))
	s.alphaFacts.Sent = nil

	run, _ := s.extendValidity(t, v)
	want := []market.IdentifierEventRequest{{Identifier: isin, Period: market.History(window{since}, s.now)}}
	if diff := cmp.Diff(want, s.alphaFacts.Sent); diff != "" {
		t.Errorf("identifier events requests mismatch (-want +got):\n%s", diff)
	}
	for _, k := range s.extended(t, run)[s.alphaName] {
		if k.kind == gen.FetchKindIdentity {
			t.Errorf("alpha's key %+v is an identity request, want only the history", k)
		}
	}
}

// TestExtendValidityRefusesRun checks that ExtendValidity refuses a run that
// is not a system extend validity run.
func TestExtendValidityRefusesRun(t *testing.T) {
	s := newStack(t)
	if _, err := s.extender.ExtendValidity(context.Background(), s.resolution, nil); err == nil {
		t.Error("ExtendValidity under a user's resolution run succeeded, want an error")
	}
}
