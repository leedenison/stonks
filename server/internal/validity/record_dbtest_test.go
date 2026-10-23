//go:build dbtest

package validity

import (
	"context"
	"errors"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/ptr"
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

func (f *fixture) mics(t *testing.T) mic.Table {
	t.Helper()
	m, err := mic.Load(context.Background(), f.q)
	require.NoError(t, err)
	return m
}

// identify gives instrument a stored ISIN, which an assertion can name.
func (f *fixture) identify(t *testing.T, instrument uuid.UUID, isin string) types.Identifier {
	t.Helper()
	id := types.Identifier{Type: types.IdentifierTypeIsin, Value: isin}
	_, err := f.q.CreateIdentifier(context.Background(), gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: instrument, Type: id.Type, Value: id.Value})
	require.NoError(t, err)
	return id
}

// TestRecord checks that a rename, the assertions of the entity's chain and
// value coverage are recorded from a served key, and derive the chain's
// validity. A failed key and an assertion of an unknown entity write nothing.
func TestRecord(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	isin := f.identify(t, f.a, "US"+uuid.NewString()[:8])
	old := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "OLD" + uuid.NewString()[:4]}
	k := f.served(t, isin, market.IdentifierEventResult{
		Events: []market.IdentifierEvent{{Value: old, Action: gen.IdentifierEventActionRenamed, ToValue: f.foo.Value, On: day(10)}},
		Assertions: []market.Assertion{
			{Value: old, Of: []types.Identifier{isin}, At: day(1)},
			{Value: f.foo, Of: []types.Identifier{isin}, At: day(10)},
			{Value: f.foo, Of: []types.Identifier{{Type: types.IdentifierTypeIsin, Value: "US0000000000"}}, At: day(12)},
		},
		Coverage: []market.Coverage{
			{Identifier: old, Period: types.Period{From: day(1), Before: day(10)}},
			{Identifier: f.foo, Period: types.Period{From: day(10), Before: day(20)}},
		},
	})
	failed := eventKey{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Response: market.IdentifierEventResult{
		Assertions: []market.Assertion{{Value: f.foo, Of: []types.Identifier{isin}, At: day(15)}},
	}}
	require.NoError(t, Record(ctx, f.q, f.mics(t), []eventKey{k, failed}))

	want := []Interval{confirmed(at(10), at(20), f.a), provisional(at(20), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of(new value) mismatch (-want +got):\n%s", diff)
	}
	oldOf, err := Of(ctx, f.q, old)
	require.NoError(t, err)
	want = []Interval{provisional(nil, at(1), f.a), confirmed(at(1), at(10), f.a)}
	if diff := cmp.Diff(want, oldOf); diff != "" {
		t.Errorf("Of(old value) mismatch (-want +got):\n%s", diff)
	}
	asserted, err := f.q.ListFetchKeyAssertions(ctx, &k.ID)
	require.NoError(t, err)
	if len(asserted) != 2 {
		t.Errorf("assertions = %+v, want the two whose entity is stored", asserted)
	}
}

// TestRecordDomainCoverage checks that coverage of a domain confirms a value
// of it that another fetch asserted.
func TestRecordDomainCoverage(t *testing.T) {
	f := newFixture(t)
	f.asserts(t, f.alpha, 5, &f.a)
	nyse := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS"}
	k := f.served(t, nyse, market.IdentifierEventResult{Coverage: []market.Coverage{{Identifier: nyse, Period: types.Period{From: day(1), Before: day(20)}}}})
	require.NoError(t, Record(context.Background(), f.q, f.mics(t), []eventKey{k}))
	want := []Interval{provisional(nil, at(1), f.a), confirmed(at(1), at(20), f.a), provisional(at(20), nil, f.a)}
	if diff := cmp.Diff(want, f.of(t)); diff != "" {
		t.Errorf("Of mismatch (-want +got):\n%s", diff)
	}
}

// TestRecordSpelling checks that a value or a domain an integration did not
// normalise fails the call before anything is written.
func TestRecordSpelling(t *testing.T) {
	tests := []struct {
		name  string
		value types.Identifier
	}{
		{name: "a class separator other than a dot", value: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "BRK-B"}},
		{name: "a venue that is not an operating MIC", value: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "ARCX", Value: "SPY"}},
		{name: "a venue that is no MIC", value: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "NYSE", Value: "IBM"}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			ctx := context.Background()
			good := f.served(t, f.foo, market.IdentifierEventResult{Coverage: []market.Coverage{{Identifier: f.foo, Period: types.Period{From: day(1), Before: day(2)}}}})
			bad := f.served(t, f.foo, market.IdentifierEventResult{Events: []market.IdentifierEvent{{Value: tc.value, Action: gen.IdentifierEventActionRetired, On: day(3)}}})
			if err := Record(ctx, f.q, f.mics(t), []eventKey{good, bad}); !errors.Is(err, ErrSpelling) {
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
// refusal of a day after today, and that an attestation on the earliest
// transaction's day confirms validity up to the provider's first assertion.
func TestAttest(t *testing.T) {
	f := newFixture(t)
	ctx := context.Background()
	run, err := f.q.CreateRun(ctx, gen.CreateRunParams{ID: db.NewID(), Kind: gen.RunKindAttestation, Trigger: gen.RunTriggerAdministrator})
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

	if err := Attest(ctx, f.q, mics, run, f.attestedBy.ID, f.foo, &f.a, day(26), today); !errors.Is(err, ErrFuture) {
		t.Errorf("Attest(tomorrow) error = %v, want ErrFuture", err)
	}
	if err := Attest(ctx, f.q, mics, f.run, f.attestedBy.ID, f.foo, &f.a, day(2), today); err == nil {
		t.Error("Attest beneath a user's run succeeded, want an error")
	}
}
