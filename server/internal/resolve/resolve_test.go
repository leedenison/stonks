package resolve

import (
	"context"
	"errors"
	"log/slog"
	"slices"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
)

var (
	userID       = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	res          = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), UserID: userID, Kind: gen.RunKindResolution}
	currencyRows = []gen.Currency{{Code: "EUR", Family: "EUR"}, {Code: "GBP", Family: "GBP"}, {Code: "GBX", Family: "GBP"}, {Code: "USD", Family: "USD"}}
	entryA       = &market.Entry{Name: "a", Precedence: 10}
	entryB       = &market.Entry{Name: "b", Precedence: 20}
)

// fixture is a resolver over a store that finds nothing and runs each
// transaction inline over itself. The stored-row scenarios are in
// dbtest_test.go.
type fixture struct {
	t        *testing.T
	store    *MockStore
	fetcher  *MockFetcher
	sources  *MockSources
	resolver *Resolver

	findings []gen.CreateFindingParams
	locked   [][]string
}

func newFixture(t *testing.T, entries ...*market.Entry) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{t: t, store: NewMockStore(ctrl), fetcher: NewMockFetcher(ctrl), sources: NewMockSources(ctrl)}
	f.store.EXPECT().ListCurrencies(gomock.Any()).Return(currencyRows, nil).AnyTimes()
	f.sources.EXPECT().Enabled().Return(entries).AnyTimes()
	f.resolver = New(f.store, f.fetcher, f.sources, slog.New(slog.DiscardHandler))
	return f
}

// records has every transaction run inline and record its findings, locks
// and resolution keys.
func (f *fixture) records() {
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error {
		return fn(f.store)
	}).AnyTimes()
	f.store.EXPECT().LockIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, keys []string) error {
		f.locked = append(f.locked, keys)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateFinding(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateFindingParams) error {
		f.findings = append(f.findings, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateResolutionKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateResolutionKeyParams) (gen.ResolutionKey, error) {
		return gen.ResolutionKey{RunID: arg.RunID, UserID: arg.UserID, StatedKeyID: arg.StatedKeyID, Outcome: arg.Outcome, Reason: arg.Reason}, nil
	}).AnyTimes()
}

// findsNothing has every read of the database find no instrument.
func (f *fixture) findsNothing() {
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
}

// creates accepts every write of a created instrument.
func (f *fixture) creates() {
	f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error) {
		return gen.Instrument{ID: arg.ID, AssetClass: arg.AssetClass}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateListingParams) (gen.Listing, error) {
		return gen.Listing{ID: arg.ID, InstrumentID: arg.InstrumentID, Currency: arg.Currency}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error) {
		return gen.Identifier{ID: arg.ID, InstrumentID: arg.InstrumentID, ListingID: arg.ListingID, Type: arg.Type, Domain: arg.Domain, Value: arg.Value}, nil
	}).AnyTimes()
	f.store.EXPECT().UpsertIdentityCoverage(gomock.Any(), gomock.Any()).AnyTimes()
	f.store.EXPECT().SetFetchKeyInstrument(gomock.Any(), gomock.Any()).AnyTimes()
	f.store.EXPECT().CreateFetchIdentifier(gomock.Any(), gomock.Any()).AnyTimes()
	f.store.EXPECT().SetStatedKeyAssociation(gomock.Any(), gomock.Any()).AnyTimes()
}

// serves has e serve one result per key, in the order of the keys.
func (f *fixture) serves(e *market.Entry, results ...result) {
	f.fetcher.EXPECT().Identity(gomock.Any(), res, e, gomock.Any()).DoAndReturn(func(_ context.Context, _ gen.Run, _ *market.Entry, keys []gen.StatedKey) ([]result, error) {
		if len(keys) != len(results) {
			f.t.Errorf("%s was sent %d keys, want %d", e.Name, len(keys), len(results))
		}
		out := make([]result, len(keys))
		for i := range keys {
			out[i] = results[i]
			out[i].Source, out[i].Request = e.Name, keys[i]
		}
		return out, nil
	})
}

func (f *fixture) resolve(keys ...gen.StatedKey) []gen.ResolutionKey {
	f.t.Helper()
	out, err := f.resolver.Resolve(context.Background(), res, keys)
	if err != nil {
		f.t.Fatalf("Resolve() error = %v", err)
	}
	return out
}

func keyOf(class gen.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	k := gen.StatedKey{ID: db.NewID(), UserID: userID, Identifiers: ids}
	if class != "" {
		k.AssetClass = &class
	}
	if currency != "" {
		k.Currency = &currency
	}
	return k
}

func servedResult(sent types.Identifier, filtered []types.Identifier, cs ...market.Candidate) result {
	return result{ID: db.NewID(), Outcome: gen.FetchOutcomeServed, Sent: &sent, Response: market.IdentityResult{Filtered: filtered, Candidates: cs}}
}

// TestResolveLocks checks that a key's write locks the identifiers it states
// and every identifier its response names, in one sorted call, since the
// write may insert any of them.
func TestResolveLocks(t *testing.T) {
	f := newFixture(t, entryA)
	f.records()
	f.findsNothing()
	f.creates()
	k := keyOf(gen.AssetClassStock, "GBX", isin, xlon)
	f.serves(entryA, servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "GBX", figi, isin, comp, xlon), cand(gen.AssetClassStock, "GBX", figi, xetr)))

	got := f.resolve(k)
	if got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("Resolve = %+v, want matched", got)
	}
	want := [][]string{{
		"identifier:isin::GB00BH4HKS39",
		"identifier:mic_ticker:XETR:VODI",
		"identifier:mic_ticker:XLON:VOD",
		"identifier:openfigi_composite::BBG000C6K6G9",
		"identifier:openfigi_share_class::BBG001S5XDT5",
	}}
	if diff := cmp.Diff(want, f.locked); diff != "" {
		t.Errorf("locks mismatch (-want +got):\n%s", diff)
	}
}

// TestResolveBatches checks that the lookup reads every key's identifiers
// in one query, and that each datasource is sent every undecided key in one
// fetch.
func TestResolveBatches(t *testing.T) {
	f := newFixture(t, entryA, entryB)
	f.records()
	one, two := keyOf(gen.AssetClassStock, "USD", isin), keyOf(gen.AssetClassStock, "USD", cusip)
	var reads [][]string
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.ListInstrumentsByIdentifiersParams) ([]gen.ListInstrumentsByIdentifiersRow, error) {
		reads = append(reads, arg.Values)
		return nil, nil
	}).AnyTimes()
	unserved := result{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no"}
	f.serves(entryA, unserved, unserved)
	f.serves(entryB, unserved, unserved)

	f.resolve(one, two)
	if len(reads) == 0 || !slices.Equal(reads[0], []string{isin.Value, cusip.Value}) {
		t.Errorf("first read = %v, want both keys' identifiers in one query", reads)
	}
}

// TestResolveUnresolved checks each outcome of a key no group won, with
// its reason and findings.
func TestResolveUnresolved(t *testing.T) {
	type finding struct {
		kind   gen.FindingKind
		step   *gen.DropStep
		detail string
		fetch  bool
	}
	tests := []struct {
		name     string
		key      gen.StatedKey
		results  []result
		fetched  bool
		outcome  gen.ResolutionOutcome
		reason   string
		findings []finding
	}{
		{
			name:    "nothing recognised",
			key:     keyOf(gen.AssetClassStock, "USD", id(types.IdentifierTypeBrokerID, "ibkr", "1")),
			outcome: gen.ResolutionOutcomeUnrecognised,
			reason:  "no global identifier",
		},
		{
			name:    "no datasource served it",
			key:     keyOf(gen.AssetClassStock, "USD", isin),
			results: []result{{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no recognised identifier type"}},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnrecognised,
			reason:  "a: skipped: no recognised identifier type",
		},
		{
			name:    "a datasource failed",
			key:     keyOf(gen.AssetClassStock, "USD", isin),
			results: []result{{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "503"}},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnavailable,
			reason:  "a: failed: 503",
		},
		{
			name:    "a bare ticker is sent and never associates",
			key:     keyOf(gen.AssetClassStock, "GBP", ticker),
			results: []result{servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, xlon))},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnrecognised,
			reason:  "a: 1 candidate in 1 group, 1 group not naming mic_ticker VOD",
		},
		{
			name:    "a description that matches no instrument",
			key:     keyOf(gen.AssetClassStock, "USD", descr),
			outcome: gen.ResolutionOutcomeUnrecognised,
			reason:  "failed to match broker description",
		},
		{
			name:    "a bare ticker beside an unknown description",
			key:     keyOf(gen.AssetClassStock, "GBP", ticker, descr),
			results: []result{servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, xlon))},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnrecognised,
			reason:  "a: 1 candidate in 1 group, 1 group not naming mic_ticker VOD",
		},
		{
			name:     "every group dropped",
			key:      keyOf(gen.AssetClassStock, "GBP", isin),
			results:  []result{servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "USD", figi, xnas))},
			fetched:  true,
			outcome:  gen.ResolutionOutcomeUnrecognised,
			reason:   "a: 1 candidate in 1 group, 1 group dropped",
			findings: []finding{{kind: gen.FindingKindDropped, step: ptr.To(gen.DropStepStated), detail: "isin GB00BH4HKS39: stated GBP has no listing among USD (a)", fetch: true}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, entryA)
			f.records()
			f.findsNothing()
			if tc.fetched {
				f.serves(entryA, tc.results...)
			}
			got := f.resolve(tc.key)
			want := []gen.ResolutionKey{{RunID: res.ID, UserID: userID, StatedKeyID: tc.key.ID, Outcome: tc.outcome, Reason: &tc.reason}}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Resolve mismatch (-want +got):\n%s", diff)
			}
			var findings []finding
			for _, w := range f.findings {
				if w.RunID != res.ID || w.StatedKeyID == nil || *w.StatedKeyID != tc.key.ID {
					t.Errorf("finding %+v names res %s key %v, want the res and the key", w, w.RunID, w.StatedKeyID)
				}
				findings = append(findings, finding{kind: w.Kind, step: w.Step, detail: *w.Detail, fetch: w.FetchKeyID != nil})
			}
			if diff := cmp.Diff(tc.findings, findings, cmp.AllowUnexported(finding{})); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestResolveRetry checks that a write the database refuses for its
// interleaving with another transaction is retried, up to tries attempts,
// and that any other failure is not.
func TestResolveRetry(t *testing.T) {
	refused := func(code string) error { return &pgconn.PgError{Code: code} }
	boom := errors.New("boom")
	tests := []struct {
		name    string
		errs    []error
		calls   int
		wantErr bool
	}{
		{name: "a unique violation then success", errs: []error{refused(pgerrcode.UniqueViolation), nil}, calls: 2},
		{name: "a deadlock then success", errs: []error{refused(pgerrcode.DeadlockDetected), nil}, calls: 2},
		{name: "a serialization failure then success", errs: []error{refused(pgerrcode.SerializationFailure), nil}, calls: 2},
		{name: "refused every time", errs: []error{refused(pgerrcode.DeadlockDetected), refused(pgerrcode.DeadlockDetected), refused(pgerrcode.DeadlockDetected)}, calls: tries, wantErr: true},
		{name: "another failure", errs: []error{boom}, calls: 1, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, entryA)
			f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, nil)
			calls := 0
			f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, func(Queries) error) error {
				err := tc.errs[calls]
				calls++
				return err
			}).Times(tc.calls)
			_, err := f.resolver.Resolve(context.Background(), res, []gen.StatedKey{keyOf(gen.AssetClassStock, "USD", descr)})
			if (err != nil) != tc.wantErr {
				t.Errorf("Resolve() error = %v, wantErr %v", err, tc.wantErr)
			}
		})
	}
}

// TestResolveErrors checks that a store or fetch error fails the resolution.
func TestResolveErrors(t *testing.T) {
	boom := errors.New("boom")
	t.Run("lookup", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, boom)
		if _, err := f.resolver.Resolve(context.Background(), res, []gen.StatedKey{keyOf("", "", isin)}); !errors.Is(err, boom) {
			t.Errorf("Resolve() error = %v, want %v", err, boom)
		}
	})
	t.Run("fetch", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, nil)
		f.fetcher.EXPECT().Identity(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, boom)
		if _, err := f.resolver.Resolve(context.Background(), res, []gen.StatedKey{keyOf("", "", isin)}); !errors.Is(err, boom) {
			t.Errorf("Resolve() error = %v, want %v", err, boom)
		}
	})
}
