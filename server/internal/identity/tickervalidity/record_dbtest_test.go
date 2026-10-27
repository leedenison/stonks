//go:build dbtest

package tickervalidity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

type eventKey = market.FetchKey[market.IdentifierEventRequest, market.IdentifierEventResult]

// served writes a served fetch key of alpha under the identifier sent, and
// returns it with the answer r.
func (f *fixture) served(t *testing.T, sent types.Identifier, r market.IdentifierEventResult) eventKey {
	t.Helper()
	k := eventKey{ID: db.NewID(), Source: f.alpha, Outcome: gen.FetchOutcomeServed, SentID: &sent, Response: r}
	arg := gen.CreateFetchKeyParams{
		ID: k.ID, FetchID: f.fetches[f.alpha], UserID: &f.user.ID, Outcome: k.Outcome, Attempts: 1,
		SentType: &sent.Type, SentDomain: sent.Domain, AnsweredAt: ptr.To(day(20)),
	}
	if !sent.IsDomain() {
		arg.SentValue = &sent.Value
	}
	require.NoError(t, f.q.CreateFetchKey(context.Background(), arg))
	return k
}

func (f *fixture) families(t *testing.T) currency.Families {
	t.Helper()
	fams, _, err := currency.Load(context.Background(), f.q)
	require.NoError(t, err)
	return fams
}

func (f *fixture) mics(t *testing.T) mic.MICMap {
	t.Helper()
	m, err := mic.Load(context.Background(), f.q)
	require.NoError(t, err)
	return m
}

// identify gives instrument a stored ISIN, which an assertion can name.
func (f *fixture) identify(t *testing.T, instr uuid.UUID, isin string) types.Identifier {
	t.Helper()
	id := types.Identifier{Type: types.IdentifierTypeIsin, Value: isin}
	_, err := f.q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrID: instr, Type: id.Type, Value: id.Value})
	require.NoError(t, err)
	return id
}

// TestRecord checks that a rename, the assertions of the entity's chain and
// ticker coverage are recorded from a served key, and derive the chain's ticker
// validity. A failed key and an assertion that states no stable identifier
// write nothing.
func TestRecord(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	isin := f.identify(t, f.a, "US"+uuid.NewString()[:8])
	old := types.MarketTicker{Market: "US", Value: "OLD" + uuid.NewString()[:4]}
	k := f.served(t, isin, market.IdentifierEventResult{
		Events: []market.IdentifierEvent{{Ticker: old, Action: gen.IdentifierEventActionRenamed, ToValue: f.foo.Value, On: day(10)}},
		Assertions: []market.Assertion{
			{Ticker: old, Of: market.Listing{Identifiers: []types.Identifier{isin}}, At: day(1)},
			{Ticker: f.foo, Of: market.Listing{Identifiers: []types.Identifier{isin}}, At: day(10)},
			{Ticker: f.foo, Of: market.Listing{Identifiers: []types.Identifier{f.at}}, At: day(12)},
		},
		Coverage: []market.Coverage{
			{Market: "US", Ticker: &old, Period: types.Period{From: day(1), Before: day(10)}},
			{Market: "US", Ticker: &f.foo, Period: types.Period{From: day(10), Before: day(20)}},
		},
	})
	failed := eventKey{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Response: market.IdentifierEventResult{
		Assertions: []market.Assertion{{Ticker: f.foo, Of: market.Listing{Identifiers: []types.Identifier{isin}}, At: day(15)}},
	}}
	require.NoError(t, Record(ctx, f.q, f.mics(t), f.families(t), []eventKey{k, failed}))

	want := []Interval{confirmed(at(10), at(20), f.a), provisional(at(20), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of(new ticker) mismatch (-want +got):\n%s", diff)
	}
	oldOf, err := Of(ctx, f.q, old)
	require.NoError(t, err)
	want = []Interval{provisional(nil, at(1), f.a), confirmed(at(1), at(10), f.a)}
	if diff := cmp.Diff(want, oldOf); diff != "" {
		t.Errorf("Of(old ticker) mismatch (-want +got):\n%s", diff)
	}
	asserted, err := f.q.ListFetchKeyAssertions(ctx, &k.ID)
	require.NoError(t, err)
	if len(asserted) != 2 {
		t.Errorf("assertions = %+v, want the two that state a stable identifier", asserted)
	}
}

// TestRecordThroughStable checks that an assertion names the instrument its
// stable identifier names, not the one carrying its ticker row.
func TestRecordThroughStable(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	isin := f.identify(t, f.a, "US"+uuid.NewString()[:8])
	listing, err := f.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrID: f.b, Currency: "USD"})
	require.NoError(t, err)
	_, err = f.q.CreateIdentifier(ctx, gen.CreateIdentifierParams{ID: db.NewID(), InstrID: f.b, ListingID: &listing.ID, Type: f.at.Type, Domain: f.at.Domain, Value: f.at.Value})
	require.NoError(t, err)
	k := f.served(t, f.at, market.IdentifierEventResult{Assertions: []market.Assertion{{Ticker: f.foo, Of: market.Listing{Identifiers: []types.Identifier{f.at, isin}}, At: day(3)}}})
	require.NoError(t, Record(ctx, f.q, f.mics(t), f.families(t), []eventKey{k}))
	asserted, err := f.q.ListFetchKeyAssertions(ctx, &k.ID)
	require.NoError(t, err)
	if len(asserted) != 1 || asserted[0].InstrID == nil || *asserted[0].InstrID != f.a {
		t.Errorf("assertions = %+v, want one naming %s through the ISIN, not %s through the ticker row", asserted, f.a, f.b)
	}
}

// TestRecordCreates checks that an assertion of an entity the system lacks
// creates the instrument with its stable identifiers and no ticker row, that
// a later assertion of the entity names it, and that a listing identifier is
// left out where the currency is unknown.
func TestRecordCreates(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	share := types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG" + uuid.NewString()[:9]}
	composite := types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG" + uuid.NewString()[:9]}
	of := []types.Identifier{share, composite, f.at}
	k := f.served(t, f.at, market.IdentifierEventResult{Assertions: []market.Assertion{
		{Ticker: f.foo, Of: market.Listing{Identifiers: of, Class: types.AssetClassEtf, Currency: "USD"}, At: day(3)},
		{Ticker: f.foo, Of: market.Listing{Identifiers: of, Class: types.AssetClassEtf, Currency: "USD"}, At: day(4)},
	}})
	require.NoError(t, Record(ctx, f.q, f.mics(t), f.families(t), []eventKey{k}))
	found, err := f.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: composite.Type, Value: composite.Value})
	require.NoError(t, err)
	instr := found.Instr
	if instr.AssetClass != types.AssetClassEtf || instr.FetchKeyID == nil || *instr.FetchKeyID != k.ID || found.Identifier.ListingID == nil {
		t.Errorf("created %+v through %+v, want an ETF with the fetch key as provenance and the composite on a listing", instr, found.Identifier)
	}
	if _, err := f.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: share.Type, Value: share.Value}); err != nil {
		t.Errorf("share class FIGI: %v, want it on the created instrument", err)
	}
	if _, err := f.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: f.at.Type, Domain: f.at.Domain, Value: f.at.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("ticker row: err = %v, want none for a past answer", err)
	}
	asserted, err := f.q.ListFetchKeyAssertions(ctx, &k.ID)
	require.NoError(t, err)
	if len(asserted) != 2 || *asserted[0].InstrID != instr.ID || *asserted[1].InstrID != instr.ID {
		t.Errorf("assertions = %+v, want both naming the one created instrument", asserted)
	}

	other := types.Identifier{Type: types.IdentifierTypeOpenfigiShareClass, Value: "BBG" + uuid.NewString()[:9]}
	loose := types.Identifier{Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG" + uuid.NewString()[:9]}
	k = f.served(t, f.at, market.IdentifierEventResult{Assertions: []market.Assertion{{Ticker: f.foo, Of: market.Listing{Identifiers: []types.Identifier{other, loose}, Currency: "ZZZ"}, At: day(5)}}})
	require.NoError(t, Record(ctx, f.q, f.mics(t), f.families(t), []eventKey{k}))
	if _, err := f.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: loose.Type, Value: loose.Value}); !errors.Is(err, db.ErrNotFound) {
		t.Errorf("composite with an unknown currency: err = %v, want it left out", err)
	}
	created, err := f.q.FindIdentifier(ctx, gen.FindIdentifierParams{Type: other.Type, Value: other.Value})
	require.NoError(t, err)
	if created.Instr.AssetClass != types.AssetClassUnknown {
		t.Errorf("class = %s, want unknown where the answer states none", created.Instr.AssetClass)
	}
}

// TestRecordMarketCoverage checks that coverage of a market confirms a
// ticker of it that another fetch asserted.
func TestRecordMarketCoverage(t *testing.T) {
	f := newFixture(t)
	f.asserts(t, f.alpha, 5, &f.a)
	nyse := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS"}
	k := f.served(t, nyse, market.IdentifierEventResult{Coverage: []market.Coverage{{Market: "US", Period: types.Period{From: day(1), Before: day(20)}}}})
	require.NoError(t, Record(context.Background(), f.q, f.mics(t), f.families(t), []eventKey{k}))
	want := []Interval{provisional(nil, at(1), f.a), confirmed(at(1), at(20), f.a), provisional(at(20), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestRecordSpelling checks that a ticker or a market an integration did not
// normalise fails the call before anything is written.
func TestRecordSpelling(t *testing.T) {
	tests := []struct {
		name   string
		ticker types.MarketTicker
	}{
		{name: "a class separator other than a dot", ticker: types.MarketTicker{Market: "US", Value: "BRK-B"}},
		{name: "a venue in place of its market", ticker: types.MarketTicker{Market: "XNYS", Value: "SPY"}},
		{name: "an unknown market", ticker: types.MarketTicker{Market: "NYSE", Value: "IBM"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			good := f.served(t, f.at, market.IdentifierEventResult{Coverage: []market.Coverage{{Market: "US", Ticker: &f.foo, Period: types.Period{From: day(1), Before: day(2)}}}})
			bad := f.served(t, f.at, market.IdentifierEventResult{Events: []market.IdentifierEvent{{Ticker: tc.ticker, Action: gen.IdentifierEventActionRetired, On: day(3)}}})
			if err := Record(ctx, f.q, f.mics(t), f.families(t), []eventKey{good, bad}); !errors.Is(err, tickers.ErrSpelling) {
				t.Fatalf("Record() error = %v, want ErrSpelling", err)
			}
			var n int
			require.NoError(t, f.tx.QueryRow(ctx, "SELECT count(*) FROM identifier_event_coverage WHERE fetch_key_id = $1", good.ID).Scan(&n))
			if n != 0 {
				t.Errorf("the good key wrote %d coverage rows, want none", n)
			}
		})
	}
}

// TestAttest checks an administrator's positive and negative assertions, the
// refusal of a day after today, and that an identifier attestation on the
// earliest transaction's day confirms ticker validity up to the provider's
// first assertion.
func TestAttest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run, err := f.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	mics := f.mics(t)
	today := day(25)
	f.asserts(t, f.alpha, 20, &f.a)

	require.NoError(t, Attest(ctx, f.q, mics, run, f.attestedBy.ID, f.foo, &f.a, day(2), today))
	want := []Interval{provisional(nil, at(2), f.a), confirmed(at(2), at(21), f.a), provisional(at(21), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of after attesting day 2 mismatch (-want +got):\n%s", diff)
	}

	require.NoError(t, Attest(ctx, f.q, mics, run, f.attestedBy.ID, f.foo, nil, today, today))
	want = []Interval{provisional(nil, at(2), f.a), confirmed(at(2), at(21), f.a), provisional(at(21), at(25), f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of after attesting that it names nothing mismatch (-want +got):\n%s", diff)
	}

	later, err := f.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator})
	require.NoError(t, err)
	require.NoError(t, Attest(ctx, f.q, mics, later, f.attestedBy.ID, f.foo, &f.b, day(2), today))
	want = []Interval{provisional(nil, at(2), f.b), confirmed(at(2), at(3), f.b), provisional(at(3), at(20), f.b), confirmed(at(20), at(21), f.a), provisional(at(21), at(25), f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of after a later identifier attestation of day 2 mismatch (-want +got):\n%s", diff)
	}

	if err := Attest(ctx, f.q, mics, run, f.attestedBy.ID, f.foo, &f.a, day(26), today); !errors.Is(err, ErrFuture) {
		t.Errorf("Attest(tomorrow) error = %v, want ErrFuture", err)
	}
	if err := Attest(ctx, f.q, mics, f.run, f.attestedBy.ID, f.foo, &f.a, day(2), today); err == nil {
		t.Error("Attest beneath a user's run succeeded, want an error")
	}
}
