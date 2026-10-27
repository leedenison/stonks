//go:build dbtest

package resolve

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// resolveOn resolves keys under the stack's resolution, each stating rows on
// dates.
func (s *stack) resolveOn(t *testing.T, dates []time.Time, keys ...gen.StatedKey) []gen.ResolutionKey {
	t.Helper()
	ks := make([]KeyDates, len(keys))
	for i, k := range keys {
		ks[i] = KeyDates{Row: k, Dates: dates}
	}
	out, _, err := s.resolver.Resolve(context.Background(), s.resolution, ks)
	require.NoError(t, err)
	return out
}

// identityFetches counts the identity fetches of the stack's resolution.
func (s *stack) identityFetches(t *testing.T) int {
	t.Helper()
	n := 0
	for _, child := range s.fetches(t) {
		fetch, err := s.q.GetFetch(context.Background(), gen.GetFetchParams{ID: child.ID, UserID: &s.user.ID})
		require.NoError(t, err)
		if fetch.Kind == gen.FetchKindIdentity {
			n++
		}
	}
	return n
}

// lookup is a point in time request for v on day d.
func lookup(v types.Identifier, d time.Time) market.IdentifierEventRequest {
	return market.IdentifierEventRequest{Identifier: v, Period: types.Period{From: d, Before: d.AddDate(0, 0, 1)}}
}

// datedThrough reports whether k has a dated association with arbiter
// through the MIC_TICKER v, given at its operating MIC.
func (s *stack) datedThrough(t *testing.T, k gen.StatedKey, v types.Identifier, arbiter gen.Arbiter) bool {
	t.Helper()
	if k.InstrID != nil || k.ViaIdentifierID != nil || k.Arbiter == nil || *k.Arbiter != arbiter {
		return false
	}
	rows, err := tickers.OfKeys(context.Background(), s.q, []gen.StatedKey{k})
	require.NoError(t, err)
	_, at, ok := tickers.Dated(k, rows)
	return ok && at == v
}

// TestDated checks that a key stating only a venue ticker is dated through
// its ticker, and that the identity answer's history and a lookup on the key's
// earliest date are recorded. A lookup naming an entity the system lacks
// creates it without a ticker row. A second key of the ticker is dated from the
// facts without an identity fetch. Resolving the first key again with an
// earlier date asks only for that day.
func TestDated(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZD")
	share := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZSHARE")
	composite := id(types.IdentifierTypeOpenfigiComposite, "", "BBG00ZZCOMP1")
	other := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZOTHER")
	renamed := on(2022, 6, 9)
	s.script.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{{Type: v.Type, Value: v.Value}}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{share, composite, v}},
	}}
	s.alphaFacts.Responses[share] = market.IdentifierEventResult{
		Events: []market.IdentifierEvent{{Ticker: mt(v), Action: gen.IdentifierEventActionAssigned, On: renamed}},
		Assertions: []market.Assertion{
			{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{share}}, At: renamed},
			{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{share}}, At: on(2026, 3, 2)},
		},
		Coverage: []market.Coverage{{Market: mt(v).Market, Ticker: ptr.To(mt(v)), Period: types.Period{From: renamed, Before: on(2026, 3, 3)}}},
	}
	s.alphaFacts.Responses[v] = market.IdentifierEventResult{Assertions: []market.Assertion{
		{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{other}, Class: types.AssetClassEtf, Currency: "USD"}, At: on(2021, 7, 1)},
	}}

	key := s.state(t, types.AssetClassStock, "USD", v)
	got := s.resolveOn(t, []time.Time{on(2024, 1, 16), on(2021, 7, 1)}, key)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, key.ID); !s.datedThrough(t, k, v, gen.ArbiterDatasource) {
		t.Errorf("key = %+v, want dated through %s by the datasource", k, v.Name())
	}
	history := market.History(window{since}, answered)
	want := []market.IdentifierEventRequest{{Identifier: share, Period: history}, lookup(v, on(2021, 7, 1))}
	if diff := cmp.Diff(want, s.alphaFacts.Sent); diff != "" {
		t.Errorf("identifier events requests mismatch (-want +got):\n%s", diff)
	}
	held, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: share.Type, Value: share.Value})
	require.NoError(t, err)
	ticker, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: v.Type, Domain: v.Domain, Value: v.Value})
	require.NoError(t, err)
	if ticker.Instr.ID != held.Instr.ID {
		t.Errorf("%s names %s, want the identity answer's instrument %s", v.Name(), ticker.Instr.ID, held.Instr.ID)
	}
	ivs, err := tickervalidity.Of(ctx, s.q, mt(v))
	require.NoError(t, err)
	if !confirmedOn(ivs, held.Instr.ID, on(2024, 1, 16)) {
		t.Errorf("ticker validity of %s = %+v, want the history to confirm %s on 2024-01-16", v.Name(), ivs, held.Instr.ID)
	}
	created, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: other.Type, Value: other.Value})
	require.NoError(t, err)
	if created.Instr.AssetClass != types.AssetClassEtf || !confirmedOn(ivs, created.Instr.ID, on(2021, 7, 1)) {
		t.Errorf("the lookup's entity = %+v under %+v, want an ETF the ticker names on 2021-07-01", created.Instr, ivs)
	}

	s.rerun(t)
	s.alphaFacts.Sent = nil
	second := s.state(t, "", "USD", v)
	got = s.resolveOn(t, []time.Time{on(2024, 1, 16), on(2021, 7, 1), on(2020, 1, 2)}, second)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("second resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, second.ID); !s.datedThrough(t, k, v, gen.ArbiterStated) {
		t.Errorf("second key = %+v, want dated through %s from the facts", k, v.Name())
	}
	if n := s.identityFetches(t); n != 0 {
		t.Errorf("identity fetches for the second key = %d, want none: the facts decide", n)
	}
	if diff := cmp.Diff([]market.IdentifierEventRequest{lookup(v, on(2020, 1, 2))}, s.alphaFacts.Sent); diff != "" {
		t.Errorf("second key's requests mismatch (-want +got):\n%s", diff)
	}

	s.rerun(t)
	s.alphaFacts.Sent = nil
	got = s.resolveOn(t, []time.Time{on(2019, 5, 1)}, s.key(t, key.ID))
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("third resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, key.ID); !s.datedThrough(t, k, v, gen.ArbiterDatasource) {
		t.Errorf("key resolved again = %+v, want its association kept", k)
	}
	if diff := cmp.Diff([]market.IdentifierEventRequest{lookup(v, on(2019, 5, 1))}, s.alphaFacts.Sent); diff != "" {
		t.Errorf("third resolution's requests mismatch (-want +got):\n%s", diff)
	}
}

type window struct{ since time.Time }

func (w window) Window(time.Time) time.Time { return w.since }

// TestDatedTickerMoves checks that an identity answer naming a ticker that
// another instrument carries moves the ticker row to the answer's instrument,
// found through its stable identifier, without a merge. A key the user fixed
// through the row keeps its instrument, row and arbiter.
func TestDatedTickerMoves(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZM")
	old, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	line, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: old.ID, Currency: "USD"})
	require.NoError(t, err)
	row, err := s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: old.ID, ListingID: &line.ID, Type: v.Type, Domain: v.Domain, Value: v.Value})
	require.NoError(t, err)
	chosen := s.state(t, types.AssetClassStock, "USD", v, descr)
	_, err = s.q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: chosen.ID, UserID: s.user.ID, InstrID: &old.ID, ListingID: &line.ID, ViaIdentifierID: &row.ID, Arbiter: gen.ArbiterUser})
	require.NoError(t, err)

	share := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZMOVED")
	s.script.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{{Type: v.Type, Value: v.Value}}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{share, v}},
	}}
	key := s.state(t, types.AssetClassStock, "USD", v)
	got := s.resolveOn(t, []time.Time{on(2026, 2, 2)}, key)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("resolution = %+v, want the key matched", got)
	}
	present, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: share.Type, Value: share.Value})
	require.NoError(t, err)
	moved, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: v.Type, Domain: v.Domain, Value: v.Value})
	require.NoError(t, err)
	if present.Instr.ID == old.ID || moved.Instr.ID != present.Instr.ID || moved.Identifier.ID != row.ID {
		t.Errorf("%s row = %+v on %s, want the same row moved to the answer's instrument %s", v.Name(), moved.Identifier, moved.Instr.ID, present.Instr.ID)
	}
	if !s.exists(t, old.ID) {
		t.Errorf("the old instrument is gone, want it kept: a ticker merges nothing")
	}
	if k := s.key(t, chosen.ID); k.InstrID == nil || *k.InstrID != old.ID || k.ViaIdentifierID == nil || *k.ViaIdentifierID != row.ID || k.Arbiter == nil || *k.Arbiter != gen.ArbiterUser {
		t.Errorf("the user's key = %+v, want it fixed on %s through the row it was made through", k, old.ID)
	}
	if k := s.key(t, key.ID); !s.datedThrough(t, k, v, gen.ArbiterDatasource) {
		t.Errorf("key = %+v, want dated through %s", k, v.Name())
	}
}

// TestDatedStable checks that a key stating a stable identifier beside a
// venue ticker is fixed through the stable one, and does not ask for
// identifier events.
func TestDatedStable(t *testing.T) {
	s := newStack(t)
	s.script.Responses[isin] = market.IdentityResult{Filtered: []types.Identifier{isin}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBX", Identifiers: []types.Identifier{figi, isin, comp, xlon}},
	}}
	key := s.state(t, types.AssetClassStock, "GBX", isin, xlon)
	got := s.resolveOn(t, []time.Time{on(2021, 7, 1)}, key)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, key.ID); k.InstrID == nil || k.ViaTickerID != nil {
		t.Errorf("key = %+v, want a fixed association", k)
	}
	if len(s.alphaFacts.Sent) != 0 {
		t.Errorf("identifier events requests = %+v, want none for a key fixed through a stable identifier", s.alphaFacts.Sent)
	}
	if _, err := s.q.FindIdentifier(context.Background(), gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value}); errors.Is(err, db.ErrNotFound) {
		t.Errorf("ISIN not written, want the instrument identified by it")
	}
}

// runFindings returns the findings of the stack's resolution that concern a
// ticker.
func (s *stack) runFindings(t *testing.T) []gen.Finding {
	t.Helper()
	rows, err := s.q.ListRunFindings(context.Background(), []uuid.UUID{s.resolution.ID})
	require.NoError(t, err)
	var out []gen.Finding
	for _, r := range rows {
		if r.Finding.StatedKeyID == nil && r.Finding.Kind != gen.FindingKindBlock {
			out = append(out, r.Finding)
		}
	}
	return out
}

// eventFetchKey returns the fetch key of datasource's identifier events
// request for subject under the stack's resolution.
func (s *stack) eventFetchKey(t *testing.T, datasource string, subject types.Identifier) gen.FetchKey {
	t.Helper()
	ctx := context.Background()
	for _, child := range s.fetches(t) {
		fetch, err := s.q.GetFetch(ctx, gen.GetFetchParams{ID: child.ID, UserID: &s.user.ID})
		require.NoError(t, err)
		if fetch.Datasource != datasource || fetch.Kind != gen.FetchKindIdentifierEvents {
			continue
		}
		items, err := s.q.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: child.ID})
		require.NoError(t, err)
		for _, it := range items {
			if it.FetchKey.SentType != nil && *it.FetchKey.SentType == subject.Type && it.FetchKey.SentValue != nil && *it.FetchKey.SentValue == subject.Value {
				return it.FetchKey
			}
		}
	}
	t.Fatalf("no identifier events fetch key of %s for %s", datasource, subject.Name())
	return gen.FetchKey{}
}

// TestDisagreement checks that two datasources whose histories name
// different instruments for one ticker on one day leave one disagreement on
// the run that wrote them, naming the lower precedence datasource's fetch
// key, and that a later run reading the same facts records none. The facts
// each run wrote come back in order: the identity fetch, then alpha's events
// fetch, then beta's.
func TestDisagreement(t *testing.T) {
	s := newStack(t)
	s.enableBeta(t)
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZG")
	share := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZGSHAR")
	other := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZGOTHR")
	day := on(2024, 1, 16)
	s.script.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{{Type: v.Type, Value: v.Value}}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{share, v}},
	}}
	s.alphaFacts.Responses[share] = market.IdentifierEventResult{Assertions: []market.Assertion{{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{share}}, At: day}}}
	s.betaFacts.Responses[share] = market.IdentifierEventResult{Assertions: []market.Assertion{{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{other}, Class: types.AssetClassEtf, Currency: "USD"}, At: day}}}
	key := s.state(t, types.AssetClassStock, "USD", v)
	out, written, err := s.resolver.Resolve(context.Background(), s.resolution, []KeyDates{{Row: key, Dates: []time.Time{day}}})
	require.NoError(t, err)
	require.Len(t, out, 1)

	found := s.runFindings(t)
	loser := s.eventFetchKey(t, s.betaName, share)
	if len(found) != 1 || found[0].Kind != gen.FindingKindIdentifierDisagreement || found[0].FetchKeyID == nil || *found[0].FetchKeyID != loser.ID {
		t.Errorf("findings = %+v, want one disagreement naming beta's fetch key %s", found, loser.ID)
	}
	if len(written) != 3 {
		t.Fatalf("written = %+v, want the identity fetch, then alpha's and beta's events fetches", written)
	}
	kinds := []gen.FetchKind{}
	sources := []string{}
	for _, w := range written {
		fetch, err := s.q.GetFetch(context.Background(), gen.GetFetchParams{ID: w.Run.ID, UserID: &s.user.ID})
		require.NoError(t, err)
		kinds, sources = append(kinds, fetch.Kind), append(sources, fetch.Datasource)
		if !slices.Contains(w.Tickers, mt(v)) {
			t.Errorf("written by %s %s = %v, want %s among them", fetch.Datasource, fetch.Kind, w.Tickers, mt(v).Name())
		}
	}
	wantKinds := []gen.FetchKind{gen.FetchKindIdentity, gen.FetchKindIdentifierEvents, gen.FetchKindIdentifierEvents}
	if diff := cmp.Diff(wantKinds, kinds); diff != "" || sources[1] != s.alphaName || sources[2] != s.betaName {
		t.Errorf("written runs = %v from %v, want identity then alpha's then beta's events", kinds, sources)
	}

	s.rerun(t)
	second := s.state(t, "", "USD", v)
	s.resolveOn(t, []time.Time{day}, second)
	if found := s.runFindings(t); len(found) != 0 {
		t.Errorf("findings of a run that only read the facts = %+v, want none", found)
	}
}

// TestMissedEvent checks the finding when one datasource asserts two
// instruments inside its own coverage. The run that wrote the facts records a
// missed event naming the coverage's fetch key. A run that only reads the
// facts records none.
func TestMissedEvent(t *testing.T) {
	s := newStack(t)
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZM")
	share := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZMSHAR")
	other := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZMOTHR")
	s.script.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{{Type: v.Type, Value: v.Value}}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{share, v}},
	}}
	s.alphaFacts.Responses[share] = market.IdentifierEventResult{
		Assertions: []market.Assertion{
			{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{share}}, At: on(2023, 1, 2)},
			{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{other}, Class: types.AssetClassStock, Currency: "USD"}, At: on(2023, 6, 1)},
		},
		Coverage: []market.Coverage{{Market: mt(v).Market, Ticker: ptr.To(mt(v)), Period: types.Period{From: on(2023, 1, 1), Before: on(2024, 2, 1)}}},
	}
	key := s.state(t, types.AssetClassStock, "USD", v)
	s.resolveOn(t, []time.Time{on(2024, 1, 16)}, key)
	found := s.runFindings(t)
	coverage := s.eventFetchKey(t, s.alphaName, share)
	if len(found) != 1 || found[0].Kind != gen.FindingKindMissedEvent || found[0].FetchKeyID == nil || *found[0].FetchKeyID != coverage.ID {
		t.Errorf("findings = %+v, want one missed event naming the coverage's fetch key %s", found, coverage.ID)
	}

	s.rerun(t)
	second := s.state(t, "", "USD", v)
	s.resolveOn(t, []time.Time{on(2024, 1, 16)}, second)
	if found := s.runFindings(t); len(found) != 0 {
		t.Errorf("findings of a run that only read the facts = %+v, want none", found)
	}
}

// TestBrokenBracket checks that a witnessed event between two assertions of
// one instrument leaves a broken bracket finding on the run that wrote the
// event, naming the event's fetch key.
func TestBrokenBracket(t *testing.T) {
	s := newStack(t)
	v := id(types.IdentifierTypeMicTicker, "XNAS", "ZZB")
	share := id(types.IdentifierTypeOpenfigiShareClass, "", "BBG00ZZBSHAR")
	s.script.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{{Type: v.Type, Value: v.Value}}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Primary: "XNAS", Identifiers: []types.Identifier{share, v}},
	}}
	s.alphaFacts.Responses[share] = market.IdentifierEventResult{
		Events: []market.IdentifierEvent{{Ticker: mt(v), Action: gen.IdentifierEventActionRenamed, ToValue: "ZZC", On: on(2023, 5, 1)}},
		Assertions: []market.Assertion{
			{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{share}}, At: on(2023, 1, 2)},
			{Ticker: mt(v), Of: market.Listing{Identifiers: []types.Identifier{share}}, At: on(2024, 1, 2)},
		},
	}
	key := s.state(t, types.AssetClassStock, "USD", v)
	s.resolveOn(t, []time.Time{on(2024, 1, 16)}, key)
	found := s.runFindings(t)
	event := s.eventFetchKey(t, s.alphaName, share)
	if len(found) != 1 || found[0].Kind != gen.FindingKindBrokenTickerValidityBracket || found[0].FetchKeyID == nil || *found[0].FetchKeyID != event.ID {
		t.Errorf("findings = %+v, want one broken bracket naming the event's fetch key %s", found, event.ID)
	}
}

// TestWriteDatedSegment checks that a key dated through a ticker at a segment
// MIC names the operating MIC of that segment as its venue.
func TestWriteDatedSegment(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	v := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNGS", Value: "SEG" + uuid.NewString()[:6]}
	key := s.state(t, types.AssetClassStock, "USD", v)
	res := &resolution{row: key, mics: s.mics}
	_, err := tickers.Get(ctx, s.q, s.mics, []types.MarketTicker{mt(v)})
	require.NoError(t, err)
	require.NoError(t, writeDated(ctx, s.q, s.user.ID, res, v, gen.ArbiterDatasource))
	got := s.key(t, key.ID)
	at := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: v.Value}
	if got.ViaVenue == nil || *got.ViaVenue != "XNAS" || !s.datedThrough(t, got, at, gen.ArbiterDatasource) {
		t.Errorf("key = %+v, want dated through %s", got, at.Name())
	}
}
