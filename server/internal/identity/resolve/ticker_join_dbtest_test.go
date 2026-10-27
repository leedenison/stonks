//go:build dbtest

package resolve

import (
	"context"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// identifierNames returns the names of the identifiers of instrument, sorted.
func (s *stack) identifierNames(t *testing.T, instr uuid.UUID) []string {
	t.Helper()
	rows, err := s.q.ListIdentifiers(context.Background(), instr)
	require.NoError(t, err)
	var out []string
	for _, row := range rows {
		out = append(out, to.Identifier(row).Name())
	}
	slices.Sort(out)
	return out
}

// stored returns the id of a new GBP-listed instrument that carries ids.
func (s *stack) stored(t *testing.T, ids ...types.Identifier) uuid.UUID {
	t.Helper()
	ctx := context.Background()
	instr, err := s.q.CreateInstr(ctx, gen.CreateInstrParams{ID: db.NewID(), AssetClass: types.AssetClassStock})
	require.NoError(t, err)
	_, err = s.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: instr.ID, Currency: "GBP"})
	require.NoError(t, err)
	for _, id := range ids {
		_, err = s.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr.ID, Type: id.Type, Domain: id.Domain, Value: id.Value})
		require.NoError(t, err)
	}
	return instr.ID
}

// tickerAnswers has alpha and beta answer v, each with one GBP listing that
// carries v beside its own identifiers.
func (s *stack) tickerAnswers(v types.Identifier, alpha, beta []types.Identifier) {
	bare := types.Identifier{Type: v.Type, Value: v.Value}
	s.script.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{bare}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: append(alpha, v)},
	}}
	s.beta.Responses[v] = market.IdentityResult{Filtered: []types.Identifier{bare}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "GBP", Identifiers: append(beta, v)},
	}}
}

// TestTickerJoins checks that two same-day answers sharing a venue ticker
// write one instrument, whose identifiers a later key matches without a
// fetch.
func TestTickerJoins(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.enableBeta(t)
	v := id(types.IdentifierTypeMicTicker, "XLON", "ZZJ")
	s.tickerAnswers(v, []types.Identifier{figi}, []types.Identifier{isin, cusip})
	key := s.state(t, types.AssetClassStock, "GBP", v)
	got := s.resolveOn(t, []time.Time{on(2026, 2, 2)}, key)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("resolution = %+v, want the key matched", got)
	}
	if kinds := s.findings(t, key.ID); len(kinds) != 0 {
		t.Errorf("findings = %v, want none: the answers join through %s", kinds, v.Name())
	}
	found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: figi.Type, Value: figi.Value})
	require.NoError(t, err)
	instr := found.Instr.ID
	want := []string{"cusip 92857W308", "isin GB00BH4HKS39", "openfigi_share_class BBG001S5XDT5"}
	if diff := cmp.Diff(want, s.identifierNames(t, instr)); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	betaKey := s.fetchKey(t, key.ID, s.betaName)
	rows, err := s.q.ListIdentifiers(ctx, instr)
	require.NoError(t, err)
	for _, row := range rows {
		if stable := to.Identifier(row); stable.Type == isin.Type || stable.Type == cusip.Type {
			if row.FetchKeyID == nil || *row.FetchKeyID != betaKey.ID {
				t.Errorf("%s from fetch key %v, want beta's %s", stable.Name(), row.FetchKeyID, betaKey.ID)
			}
		}
	}

	s.rerun(t)
	second := s.state(t, types.AssetClassStock, "GBP", isin)
	got = s.resolve(t, second)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("second resolution = %+v, want the key matched", got)
	}
	if k := s.key(t, second.ID); k.InstrID == nil || *k.InstrID != instr {
		t.Errorf("second key = %+v, want it on %s", k, instr)
	}
	if n := len(s.fetches(t)); n != 0 {
		t.Errorf("fetches for the ISIN key = %d, want none: the join wrote the ISIN", n)
	}
}

// TestTickerJoinsAcrossVenues checks that two same-day answers carrying one
// ticker at two venues of its market join into one instrument.
func TestTickerJoinsAcrossVenues(t *testing.T) {
	s := newStack(t)
	ctx := context.Background()
	s.enableBeta(t)
	nasdaq := id(types.IdentifierTypeMicTicker, "XNAS", "ZZK")
	bats := id(types.IdentifierTypeMicTicker, "BATS", "ZZK")
	bare := types.Identifier{Type: nasdaq.Type, Value: nasdaq.Value}
	s.script.Responses[nasdaq] = market.IdentityResult{Filtered: []types.Identifier{bare}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{figi, nasdaq}},
	}}
	s.beta.Responses[nasdaq] = market.IdentityResult{Filtered: []types.Identifier{bare}, Listings: []market.Listing{
		{Class: types.AssetClassStock, Currency: "USD", Identifiers: []types.Identifier{isin, bats}},
	}}
	key := s.state(t, types.AssetClassStock, "USD", nasdaq)
	got := s.resolveOn(t, []time.Time{on(2026, 2, 2)}, key)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("resolution = %+v, want the key matched", got)
	}
	if kinds := s.findings(t, key.ID); len(kinds) != 0 {
		t.Errorf("findings = %v, want none: the answers join through the market ticker %s", kinds, mt(nasdaq).Name())
	}
	byFIGI, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: figi.Type, Value: figi.Value})
	require.NoError(t, err)
	byISIN, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: isin.Type, Value: isin.Value})
	require.NoError(t, err)
	if byFIGI.Instr.ID != byISIN.Instr.ID {
		t.Errorf("the FIGI names %s and the ISIN %s, want one instrument", byFIGI.Instr.ID, byISIN.Instr.ID)
	}
}

// TestTickerJoinKeepsInstrsApart checks that a claim joined through the
// ticker alone never merges with a stored instrument. The claim is dropped
// with a contradiction finding. That holds whether the write creates the
// instrument or takes a stored one. It also holds whether the claim joined
// the datasource winner on its day, or joined the stored instrument through
// a stable identifier on a later day.
func TestTickerJoinKeepsInstrsApart(t *testing.T) {
	ctx := context.Background()
	// takesStored checks that the key is dated through v on target, that
	// the other claim is dropped with a contradiction, and that target and
	// other keep their own identifiers.
	takesStored := func(t *testing.T, s *stack, key gen.StatedKey, v types.Identifier, target, other uuid.UUID) {
		t.Helper()
		got := s.resolveOn(t, []time.Time{on(2026, 2, 2)}, key)
		if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
			t.Fatalf("resolution = %+v, want the key matched", got)
		}
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, key.ID)); diff != "" {
			t.Errorf("findings mismatch (-want +got):\n%s", diff)
		}
		if k := s.key(t, key.ID); !s.datedThrough(t, k, v, gen.ArbiterStated) {
			t.Errorf("key = %+v, want dated through %s on the stored instrument", k, v.Name())
		}
		if names := s.names(t, target); !slices.ContainsFunc(names, func(n string) bool { return strings.HasPrefix(n, v.Value+" at ") }) {
			t.Errorf("names of the stored instrument = %v, want %s among them", names, v.Name())
		}
		if diff := cmp.Diff([]string{"isin GB00BH4HKS39", "openfigi_share_class BBG001S5XDT5"}, s.identifierNames(t, target)); diff != "" {
			t.Errorf("target's identifiers mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"cusip 92857W308"}, s.identifierNames(t, other)); diff != "" {
			t.Errorf("other instrument's identifiers mismatch (-want +got):\n%s", diff)
		}
	}
	t.Run("the write creates the instrument", func(t *testing.T) {
		s := newStack(t)
		s.enableBeta(t)
		other := s.stored(t, isin)
		v := id(types.IdentifierTypeMicTicker, "XLON", "ZZK")
		s.tickerAnswers(v, []types.Identifier{figi}, []types.Identifier{isin})
		key := s.state(t, types.AssetClassStock, "GBP", v)
		got := s.resolveOn(t, []time.Time{on(2026, 2, 2)}, key)
		if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
			t.Fatalf("resolution = %+v, want the key matched", got)
		}
		if diff := cmp.Diff([]gen.FindingKind{gen.FindingKindContradiction}, s.findings(t, key.ID)); diff != "" {
			t.Errorf("findings mismatch (-want +got):\n%s", diff)
		}
		found, err := s.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: figi.Type, Value: figi.Value})
		require.NoError(t, err)
		if found.Instr.ID == other {
			t.Fatalf("the key took the stored instrument %s, want a new one: a ticker merges nothing", other)
		}
		if diff := cmp.Diff([]string{"openfigi_share_class BBG001S5XDT5"}, s.identifierNames(t, found.Instr.ID)); diff != "" {
			t.Errorf("new instrument's identifiers mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"isin GB00BH4HKS39"}, s.identifierNames(t, other)); diff != "" {
			t.Errorf("stored instrument's identifiers mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("the write takes a stored instrument", func(t *testing.T) {
		s := newStack(t)
		s.enableBeta(t)
		target := s.stored(t, figi, isin)
		other := s.stored(t, cusip)
		v := id(types.IdentifierTypeMicTicker, "XLON", "ZZL")
		s.tickerAnswers(v, []types.Identifier{figi}, []types.Identifier{isin, cusip})
		key := s.state(t, types.AssetClassStock, "GBP", v)
		takesStored(t, s, key, v, target, other)
	})
	t.Run("a claim joining the stored instrument on a later day", func(t *testing.T) {
		s := newStack(t)
		s.cache.on = true
		target := s.stored(t, figi, isin)
		other := s.stored(t, cusip)
		v := id(types.IdentifierTypeMicTicker, "XLON", "ZZN")
		s.tickerAnswers(v, []types.Identifier{figi}, []types.Identifier{isin, cusip})
		// Alpha answers on the first day for a key whose stated currency its
		// listing contradicts. The answer is cached and the write leaves the
		// database unchanged.
		usd := s.state(t, types.AssetClassStock, "USD", v)
		if got := s.resolveOn(t, []time.Time{on(2026, 2, 2)}, usd); len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeUnrecognised {
			t.Fatalf("resolution of the USD key = %+v, want unrecognised", got)
		}
		// On the next day beta answers live and alpha replays from the cache.
		// Beta's answer has no identifier in common with alpha's, so it joins
		// the stored instrument through the ISIN.
		s.now = s.now.Add(24 * time.Hour)
		s.rerun(t)
		s.enableBeta(t)
		key := s.state(t, types.AssetClassStock, "GBP", v)
		takesStored(t, s, key, v, target, other)
	})
}
