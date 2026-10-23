//go:build dbtest

package resolve

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// on is the UTC midnight of a day.
func on(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

// resolveOn resolves keys under the stack's resolution, each stating rows on
// dates.
func (s *stack) resolveOn(t *testing.T, dates []time.Time, keys ...gen.StatedKey) []gen.ResolutionKey {
	t.Helper()
	ks := make([]KeyOrderDates, len(keys))
	for i, k := range keys {
		ks[i] = KeyOrderDates{Row: k, Dates: dates}
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

// datedThrough reports whether k has a dated association through v with
// arbiter.
func datedThrough(k gen.StatedKey, v types.Identifier, arbiter gen.Arbiter) bool {
	return k.InstrumentID == nil && k.ViaID == nil && k.ViaType != nil && *k.ViaType == v.Type &&
		k.ViaDomain != nil && *k.ViaDomain == v.Domain && k.ViaValue != nil && *k.ViaValue == v.Value &&
		k.Arbiter != nil && *k.Arbiter == arbiter
}

// TestDated checks that a key stating only a venue ticker is dated through
// its value, and that the identity answer's history and a lookup on the key's
// earliest date are recorded. A lookup naming an entity the system lacks
// creates it without a ticker row. A second key of the value is dated from the
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
		Events: []market.IdentifierEvent{{Value: v, Action: gen.IdentifierEventActionAssigned, On: renamed}},
		Assertions: []market.Assertion{
			{Value: v, Of: market.Listing{Identifiers: []types.Identifier{share}}, At: renamed},
			{Value: v, Of: market.Listing{Identifiers: []types.Identifier{share}}, At: on(2026, 3, 2)},
		},
		Coverage: []market.Coverage{{Identifier: v, Period: types.Period{From: renamed, Before: on(2026, 3, 3)}}},
	}
	s.alphaFacts.Responses[v] = market.IdentifierEventResult{Assertions: []market.Assertion{
		{Value: v, Of: market.Listing{Identifiers: []types.Identifier{other}, Class: types.AssetClassEtf, Currency: "USD"}, At: on(2021, 7, 1)},
	}}

	key := s.state(t, types.AssetClassStock, "USD", v)
	got := s.resolveOn(t, []time.Time{on(2024, 1, 16), on(2021, 7, 1)}, key)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, key.ID); !datedThrough(k, v, gen.ArbiterDatasource) {
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
	if ticker.Instrument.ID != held.Instrument.ID {
		t.Errorf("%s names %s, want the identity answer's instrument %s", v.Name(), ticker.Instrument.ID, held.Instrument.ID)
	}
	ivs, err := validity.Of(ctx, s.q, v)
	require.NoError(t, err)
	if !confirmedOn(ivs, held.Instrument.ID, on(2024, 1, 16)) {
		t.Errorf("validity of %s = %+v, want the history to confirm %s on 2024-01-16", v.Name(), ivs, held.Instrument.ID)
	}
	created, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: other.Type, Value: other.Value})
	require.NoError(t, err)
	if created.Instrument.AssetClass != types.AssetClassEtf || !confirmedOn(ivs, created.Instrument.ID, on(2021, 7, 1)) {
		t.Errorf("the lookup's entity = %+v under %+v, want an ETF the value names on 2021-07-01", created.Instrument, ivs)
	}

	s.rerun(t)
	s.alphaFacts.Sent = nil
	second := s.state(t, "", "USD", v)
	got = s.resolveOn(t, []time.Time{on(2024, 1, 16), on(2021, 7, 1), on(2020, 1, 2)}, second)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("second resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, second.ID); !datedThrough(k, v, gen.ArbiterStated) {
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
	if k := s.key(t, key.ID); !datedThrough(k, v, gen.ArbiterDatasource) {
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
	old, err := s.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	line, err := s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: old.ID, Currency: "USD"})
	require.NoError(t, err)
	row, err := s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: old.ID, ListingID: &line.ID, Type: v.Type, Domain: v.Domain, Value: v.Value})
	require.NoError(t, err)
	chosen := s.state(t, types.AssetClassStock, "USD", v, descr)
	_, err = s.q.SetStatedKeyAssociation(ctx, gen.SetStatedKeyAssociationParams{ID: chosen.ID, UserID: s.user.ID, InstrumentID: &old.ID, ListingID: &line.ID, ViaID: &row.ID, Arbiter: gen.ArbiterUser})
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
	if present.Instrument.ID == old.ID || moved.Instrument.ID != present.Instrument.ID || moved.Identifier.ID != row.ID {
		t.Errorf("%s row = %+v on %s, want the same row moved to the answer's instrument %s", v.Name(), moved.Identifier, moved.Instrument.ID, present.Instrument.ID)
	}
	if !s.exists(t, old.ID) {
		t.Errorf("the old instrument is gone, want it kept: a ticker merges nothing")
	}
	if k := s.key(t, chosen.ID); k.InstrumentID == nil || *k.InstrumentID != old.ID || k.ViaID == nil || *k.ViaID != row.ID || k.Arbiter == nil || *k.Arbiter != gen.ArbiterUser {
		t.Errorf("the user's key = %+v, want it fixed on %s through the row it was made through", k, old.ID)
	}
	if k := s.key(t, key.ID); !datedThrough(k, v, gen.ArbiterDatasource) {
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
	if k := s.key(t, key.ID); k.InstrumentID == nil || k.ViaType != nil {
		t.Errorf("key = %+v, want a fixed association", k)
	}
	if len(s.alphaFacts.Sent) != 0 {
		t.Errorf("identifier events requests = %+v, want none for a key fixed through a stable identifier", s.alphaFacts.Sent)
	}
	if _, err := s.q.FindIdentifier(context.Background(), gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value}); errors.Is(err, db.ErrNotFound) {
		t.Errorf("ISIN not written, want the instrument identified by it")
	}
}
