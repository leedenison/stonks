package resolve

import (
	"context"
	"errors"
	"fmt"
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
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	userID       = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	res          = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), UserID: userID, Kind: gen.RunKindResolution}
	currencyRows = []gen.Currency{{Code: "EUR", Family: "EUR"}, {Code: "GBP", Family: "GBP"}, {Code: "GBX", Family: "GBP"}, {Code: "USD", Family: "USD"}}
	entryA       = &market.Entry{Name: "a", Precedence: 10}
	entryB       = &market.Entry{Name: "b", Precedence: 20}
	entryC       = &market.Entry{Name: "c", Precedence: 30}
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
	// associations is every association written, and rows is what each
	// write reports it changed.
	associations []gen.SetStatedKeyAssociationParams
	rows         int64
	// arbitrated is the user's keys the user arbitrated, and mates the keys of
	// a confirmed key's group.
	arbitrated []gen.StatedKey
	mates      []gen.StatedKey
	// stored is the rows that a read of the database by identifier returns.
	stored []gen.ListInstrumentsByIdentifiersRow
	// instruments is every instrument created, listings every listing
	// created, primaries every primary venue set on a listing, identifiers
	// every identifier created, attached every fetch key attached,
	// resolutions how many resolution keys were written, and txs how many
	// transactions were run.
	instruments []gen.CreateInstrumentParams
	listings    []gen.CreateListingParams
	primaries   []gen.SetListingPrimaryParams
	identifiers []gen.CreateIdentifierParams
	attached    []uuid.UUID
	resolutions int
	txs         int
}

func newFixture(t *testing.T, entries ...*market.Entry) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{t: t, store: NewMockStore(ctrl), fetcher: NewMockFetcher(ctrl), sources: NewMockSources(ctrl), rows: 1}
	f.store.EXPECT().ListCurrencies(gomock.Any()).Return(currencyRows, nil).AnyTimes()
	f.store.EXPECT().ListUserArbitratedKeys(gomock.Any(), userID).DoAndReturn(func(context.Context, uuid.UUID) ([]gen.StatedKey, error) {
		return f.arbitrated, nil
	}).AnyTimes()
	f.sources.EXPECT().Enabled().Return(entries).AnyTimes()
	f.resolver = New(f.store, f.fetcher, f.sources, slog.New(slog.DiscardHandler))
	return f
}

// records has every transaction run inline and record its findings, locks
// and resolution keys.
func (f *fixture) records() {
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error {
		f.txs++
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
	f.store.EXPECT().SetStatedKeyAssociation(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.SetStatedKeyAssociationParams) (int64, error) {
		f.associations = append(f.associations, arg)
		return f.rows, nil
	}).AnyTimes()
	f.store.EXPECT().LockUserKeys(gomock.Any(), userID).Return(nil).AnyTimes()
	f.store.EXPECT().GetStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.GetStatedKeyParams) (gen.StatedKey, error) {
		k := gen.StatedKey{ID: arg.ID, UserID: arg.UserID}
		if len(f.mates) > 0 {
			k.GroupID = &k.ID
		}
		return k, nil
	}).AnyTimes()
	f.store.EXPECT().ListStatedKeysOfGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, gen.ListStatedKeysOfGroupsParams) ([]gen.StatedKey, error) {
		return f.mates, nil
	}).AnyTimes()
	f.store.EXPECT().CreateResolutionKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateResolutionKeyParams) (gen.ResolutionKey, error) {
		f.resolutions++
		return gen.ResolutionKey{RunID: arg.RunID, UserID: arg.UserID, StatedKeyID: arg.StatedKeyID, Outcome: arg.Outcome, Reasons: arg.Reasons, Offered: arg.Offered}, nil
	}).AnyTimes()
}

// finds serves the database's reads from f.stored. With f.stored empty, a
// read finds nothing.
func (f *fixture) finds() {
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.ListInstrumentsByIdentifiersParams) ([]gen.ListInstrumentsByIdentifiersRow, error) {
		var out []gen.ListInstrumentsByIdentifiersRow
		for _, row := range f.stored {
			for i := range arg.Types {
				if arg.Types[i] == string(row.Identifier.Type) && arg.Domains[i] == row.Identifier.Domain && arg.Values[i] == row.Identifier.Value {
					out = append(out, row)
				}
			}
		}
		return out, nil
	}).AnyTimes()
	f.store.EXPECT().ListIdentifiersOf(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ids []uuid.UUID) ([]gen.Identifier, error) {
		var out []gen.Identifier
		for _, row := range f.stored {
			if slices.Contains(ids, row.Instrument.ID) {
				out = append(out, row.Identifier)
			}
		}
		return out, nil
	}).AnyTimes()
	f.store.EXPECT().ListListingsOf(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	f.store.EXPECT().ListIdentityCoverage(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
}

// creates accepts every write of a created instrument.
func (f *fixture) creates() {
	f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error) {
		f.instruments = append(f.instruments, arg)
		return gen.Instrument{ID: arg.ID, AssetClass: arg.AssetClass, FetchKeyID: arg.FetchKeyID}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateListingParams) (gen.Listing, error) {
		f.listings = append(f.listings, arg)
		return gen.Listing{ID: arg.ID, InstrumentID: arg.InstrumentID, Currency: arg.Currency, PrimaryMic: arg.PrimaryMic}, nil
	}).AnyTimes()
	f.store.EXPECT().SetListingPrimary(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.SetListingPrimaryParams) error {
		f.primaries = append(f.primaries, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error) {
		f.identifiers = append(f.identifiers, arg)
		return gen.Identifier{ID: arg.ID, InstrumentID: arg.InstrumentID, ListingID: arg.ListingID, Type: arg.Type, Domain: arg.Domain, Value: arg.Value}, nil
	}).AnyTimes()
	f.store.EXPECT().UpsertIdentityCoverage(gomock.Any(), gomock.Any()).AnyTimes()
	f.store.EXPECT().SetFetchKeyInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.SetFetchKeyInstrumentParams) error {
		f.attached = append(f.attached, arg.ID)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateFetchIdentifier(gomock.Any(), gomock.Any()).AnyTimes()
}

// serves has e serve one fetch key per key, in the order of the keys.
func (f *fixture) serves(e *market.Entry, fetchKeys ...fetchKey) {
	f.fetcher.EXPECT().Identity(gomock.Any(), res, e, gomock.Any()).DoAndReturn(func(_ context.Context, _ gen.Run, _ *market.Entry, keys []gen.StatedKey) ([]fetchKey, error) {
		if len(keys) != len(fetchKeys) {
			f.t.Errorf("%s was sent %d keys, want %d", e.Name, len(keys), len(fetchKeys))
		}
		out := make([]fetchKey, len(keys))
		for i := range keys {
			out[i] = fetchKeys[i]
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

func keyOf(class types.AssetClass, currency string, ids ...types.Identifier) gen.StatedKey {
	k := gen.StatedKey{ID: db.NewID(), UserID: userID, Identifiers: ids}
	if class != "" {
		k.AssetClass = &class
	}
	if currency != "" {
		k.Currency = &currency
	}
	return k
}

func servedFetchKey(sent types.Identifier, filtered []types.Identifier, cs ...market.Listing) fetchKey {
	return fetchKey{ID: db.NewID(), Outcome: gen.FetchOutcomeServed, SentID: &sent, Response: market.IdentityResult{Filtered: filtered, Listings: cs}}
}

// limitedFetchKey marks fk as an answer from a provider that lists only some of
// an instrument's listings.
func limitedFetchKey(fk fetchKey) fetchKey {
	fk.Response.Limited = true
	return fk
}

// TestResolveLocks checks that a key's write locks the identifiers it states
// and every identifier its response names, in one sorted call, since the
// write may insert any of them.
func TestResolveLocks(t *testing.T) {
	f := newFixture(t, entryA)
	f.records()
	f.finds()
	f.creates()
	k := keyOf(types.AssetClassStock, "GBX", isin, xlon)
	f.serves(entryA, servedFetchKey(isin, []types.Identifier{isin}, listing(types.AssetClassStock, "GBX", figi, isin, comp, xlon), listing(types.AssetClassStock, "GBX", figi, xetr)))

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
	one, two := keyOf(types.AssetClassStock, "USD", isin), keyOf(types.AssetClassStock, "USD", cusip)
	var reads [][]string
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.ListInstrumentsByIdentifiersParams) ([]gen.ListInstrumentsByIdentifiersRow, error) {
		reads = append(reads, arg.Values)
		return nil, nil
	}).AnyTimes()
	unserved := fetchKey{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no"}
	f.serves(entryA, unserved, unserved)
	f.serves(entryB, unserved, unserved)

	f.resolve(one, two)
	if len(reads) == 0 || !slices.Equal(reads[0], []string{isin.Value, cusip.Value}) {
		t.Errorf("first read = %v, want both keys' identifiers in one query", reads)
	}
}

// TestResolveUnresolved checks each outcome of a key no claim won, with its
// reason and findings.
func TestResolveUnresolved(t *testing.T) {
	type finding struct {
		kind   gen.FindingKind
		step   *gen.DropStep
		detail string
		fetch  bool
	}
	tests := []struct {
		name      string
		key       gen.StatedKey
		fetchKeys []fetchKey
		// b is what a second datasource serves, where it is set.
		b        []fetchKey
		fetched  bool
		outcome  gen.ResolutionOutcome
		reasons  []string
		offered  int32
		findings []finding
	}{
		{
			name:    "nothing recognised",
			key:     keyOf(types.AssetClassStock, "USD", id(types.IdentifierTypeBrokerID, "ibkr", "1")),
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"no global identifier"},
		},
		{
			name:      "no datasource served it",
			key:       keyOf(types.AssetClassStock, "USD", isin),
			fetchKeys: []fetchKey{{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no recognised identifier type"}},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnrecognised,
			reasons:   []string{"a: skipped: no recognised identifier type"},
		},
		{
			name:      "a datasource failed",
			key:       keyOf(types.AssetClassStock, "USD", isin),
			fetchKeys: []fetchKey{{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "503"}},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnavailable,
			reasons:   []string{"a: failed: 503"},
		},
		{
			name:      "a bare ticker is sent and never associates",
			key:       keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: []fetchKey{servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon))},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnrecognised,
			reasons:   []string{"a: 1 listing in 1 set of identifiers, 1 set of identifiers not naming mic_ticker VOD"},
			offered:   1,
		},
		{
			name:      "two datasources describing one security under a bare ticker offer one candidate",
			key:       keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: []fetchKey{servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon))},
			b:         []fetchKey{servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon))},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnrecognised,
			reasons: []string{
				"a: 1 listing in 1 set of identifiers, 1 set of identifiers not naming mic_ticker VOD",
				"b: 1 listing in 1 set of identifiers, 1 set of identifiers not naming mic_ticker VOD",
			},
			offered: 1,
		},
		{
			name:      "two datasources offering the survivors of a mis-stated venue count each security once",
			key:       keyOf(types.AssetClassStock, "GBP", xlon),
			fetchKeys: []fetchKey{servedFetchKey(xlon, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "GBP", figi2, xnas))},
			b:         []fetchKey{servedFetchKey(xlon, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "GBP", figi2, xnas))},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnrecognised,
			reasons:   []string{"a: 2 listings in 2 sets of identifiers", "b: 2 listings in 2 sets of identifiers"},
			offered:   2,
		},
		{
			name:    "a description that matches no instrument",
			key:     keyOf(types.AssetClassStock, "USD", descr),
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"failed to match broker description"},
		},
		{
			name:      "a bare ticker beside an unknown description",
			key:       keyOf(types.AssetClassStock, "GBP", ticker, descr),
			fetchKeys: []fetchKey{servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon))},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnrecognised,
			reasons:   []string{"a: 1 listing in 1 set of identifiers, 1 set of identifiers not naming mic_ticker VOD"},
			offered:   1,
		},
		{
			name:      "every claim dropped",
			key:       keyOf(types.AssetClassStock, "GBP", isin),
			fetchKeys: []fetchKey{servedFetchKey(isin, []types.Identifier{isin}, listing(types.AssetClassStock, "USD", figi, xnas))},
			fetched:   true,
			outcome:   gen.ResolutionOutcomeUnrecognised,
			reasons:   []string{"a: 1 listing in 1 set of identifiers, 1 set of identifiers dropped"},
			findings:  []finding{{kind: gen.FindingKindDropped, step: ptr.To(gen.DropStepStated), detail: "isin GB00BH4HKS39: stated GBP has no listing among USD (a)", fetch: true}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := []*market.Entry{entryA}
			if tc.b != nil {
				entries = append(entries, entryB)
			}
			f := newFixture(t, entries...)
			f.records()
			f.finds()
			if tc.fetched {
				f.serves(entryA, tc.fetchKeys...)
			}
			if tc.b != nil {
				f.serves(entryB, tc.b...)
			}
			got := f.resolve(tc.key)
			want := []gen.ResolutionKey{{RunID: res.ID, UserID: userID, StatedKeyID: tc.key.ID, Outcome: tc.outcome, Reasons: tc.reasons, Offered: tc.offered}}
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
// primary returns c stating mic as its listing's primary venue.
func primary(c market.Listing, mic string) market.Listing {
	c.Primary = mic
	return c
}

// TestPrimaryVenue checks that a listing takes the primary venue of the
// highest precedence datasource stating one, and that a lower one fills a
// listing the higher left blank.
func TestPrimaryVenue(t *testing.T) {
	nyse, nasdaq := "XNYS", "XNAS"
	tests := []struct {
		name    string
		a, b    fetchKey
		created *string
		set     []string
	}{
		{
			name:    "a lower datasource fills the venue a higher left blank",
			a:       servedFetchKey(isin, nil, listing(types.AssetClassStock, "USD", figi, isin, xnas)),
			b:       servedFetchKey(isin, nil, primary(listing(types.AssetClassStock, "USD", figi, isin, xnas), nasdaq)),
			created: nil,
			set:     []string{nasdaq},
		},
		{
			name:    "the higher datasource's venue stands",
			a:       servedFetchKey(isin, nil, primary(listing(types.AssetClassStock, "USD", figi, isin, xnas), nyse)),
			b:       servedFetchKey(isin, nil, primary(listing(types.AssetClassStock, "USD", figi, isin, xnas), nasdaq)),
			created: &nyse,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, entryA, entryB)
			f.records()
			f.finds()
			f.creates()
			f.serves(entryA, tc.a)
			f.serves(entryB, tc.b)
			got := f.resolve(keyOf(types.AssetClassStock, "USD", isin))
			if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched {
				t.Fatalf("Resolve() = %+v, want one matched key", got)
			}
			if len(f.listings) != 1 || !ptr.Equal(f.listings[0].PrimaryMic, tc.created) {
				t.Errorf("created listings = %+v, want one with primary venue %v", f.listings, tc.created)
			}
			var set []string
			for _, p := range f.primaries {
				set = append(set, *p.PrimaryMic)
			}
			if diff := cmp.Diff(tc.set, set); diff != "" {
				t.Errorf("primary venues set mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

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
			_, err := f.resolver.Resolve(context.Background(), res, []gen.StatedKey{keyOf(types.AssetClassStock, "USD", descr)})
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

// TestArbiter checks the arbiter each association records, and that a key
// the user arbitrates during the run keeps that association and is
// recorded matched.
func TestArbiter(t *testing.T) {
	inst := gen.Instrument{ID: db.NewID(), AssetClass: types.AssetClassStock}
	isinRow := gen.Identifier{ID: db.NewID(), InstrumentID: inst.ID, Type: isin.Type, Value: isin.Value}
	t.Run("a fetched winner is the datasource's", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.finds()
		f.creates()
		f.serves(entryA, servedFetchKey(isin, []types.Identifier{isin}, listing(types.AssetClassStock, "GBX", figi, isin, xlon)))
		got := f.resolve(keyOf(types.AssetClassStock, "GBX", isin))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.associations) != 1 || f.associations[0].Arbiter != gen.ArbiterDatasource {
			t.Errorf("Resolve = %+v with associations %+v, want matched with the datasource as arbiter", got, f.associations)
		}
	})
	t.Run("an instrument the database names is the stated data's", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.creates()
		f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return([]gen.ListInstrumentsByIdentifiersRow{{Instrument: inst, Identifier: isinRow}}, nil).AnyTimes()
		f.store.EXPECT().ListIdentifiersOf(gomock.Any(), gomock.Any()).Return([]gen.Identifier{isinRow}, nil).AnyTimes()
		f.store.EXPECT().ListListingsOf(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		f.store.EXPECT().ListIdentityCoverage(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
		got := f.resolve(keyOf(types.AssetClassStock, "", isin))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.associations) != 1 || f.associations[0].Arbiter != gen.ArbiterStated {
			t.Errorf("Resolve = %+v with associations %+v, want matched with the stated data as arbiter", got, f.associations)
		}
	})
	t.Run("an association the user arbitrated stands", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.finds()
		f.creates()
		f.rows = 0
		f.serves(entryA, servedFetchKey(isin, []types.Identifier{isin}, listing(types.AssetClassStock, "GBX", figi, isin, xlon)))
		got := f.resolve(keyOf(types.AssetClassStock, "GBX", isin))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.instruments) != 1 || len(f.attached) != 1 {
			t.Errorf("Resolve = %+v, instruments %d, attached %d: want matched with the instrument data written", got, len(f.instruments), len(f.attached))
		}
	})
}

// TestArbitratedKey checks that a key sharing a joining identifier with a key
// the user arbitrated takes its association before any datasource is asked.
func TestArbitratedKey(t *testing.T) {
	descr2 := id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORP 2")
	instX, instY := db.NewID(), db.NewID()
	listing, via := db.NewID(), db.NewID()
	confirmed := func(instrument uuid.UUID, ids ...types.Identifier) gen.StatedKey {
		k := keyOf(types.AssetClassStock, "GBP", ids...)
		k.InstrumentID, k.ListingID, k.ViaID = &instrument, &listing, &via
		k.Validity, k.Arbiter = ptr.To(gen.ValidityProvisional), ptr.To(gen.ArbiterUser)
		return k
	}
	t.Run("a key stating a confirmed key's description takes its association", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.finds()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr)}
		got := f.resolve(keyOf(types.AssetClassStock, "GBP", ticker, descr))
		want := gen.SetStatedKeyAssociationParams{ID: got[0].StatedKeyID, UserID: userID, InstrumentID: &instX, ListingID: &listing, ViaID: &via, Validity: ptr.To(gen.ValidityProvisional), Arbiter: gen.ArbiterUser}
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.associations) != 1 {
			t.Fatalf("Resolve = %+v with associations %+v, want matched with one association", got, f.associations)
		}
		if diff := cmp.Diff(want, f.associations[0]); diff != "" {
			t.Errorf("association mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("a different currency family takes no listing", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.finds()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr)}
		got := f.resolve(keyOf(types.AssetClassStock, "USD", ticker, descr))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.associations) != 1 || f.associations[0].ListingID != nil {
			t.Errorf("Resolve = %+v with associations %+v, want matched on the instrument with no listing", got, f.associations)
		}
	})
	t.Run("a bare ticker alone joins nothing", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.finds()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr)}
		f.serves(entryA, fetchKey{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no"})
		got := f.resolve(keyOf(types.AssetClassStock, "GBP", ticker))
		if got[0].Outcome != gen.ResolutionOutcomeUnrecognised || len(f.associations) != 0 {
			t.Errorf("Resolve = %+v with associations %+v, want unrecognised after asking the datasource", got, f.associations)
		}
	})
	t.Run("confirmed keys naming different instruments leave the key to the datasources", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.finds()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr), confirmed(instY, ticker, descr2)}
		f.serves(entryA, fetchKey{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no"})
		got := f.resolve(keyOf(types.AssetClassStock, "GBP", ticker, descr, descr2))
		if got[0].Outcome != gen.ResolutionOutcomeUnrecognised || len(f.associations) != 0 {
			t.Errorf("Resolve = %+v with associations %+v, want unrecognised after asking the datasource", got, f.associations)
		}
		want := fmt.Sprintf("confirmed keys name different instruments: broker_description ibkr:ACME CORP names %s and broker_description ibkr:ACME CORP 2 names %s", instX, instY)
		if len(f.findings) != 1 || f.findings[0].Kind != gen.FindingKindContradiction || *f.findings[0].Detail != want {
			t.Errorf("findings = %+v, want one contradiction naming the two instruments", f.findings)
		}
	})
}

// TestCandidates checks what a synchronous resolution offers for a key.
func TestCandidates(t *testing.T) {
	cusip2 := id(types.IdentifierTypeCusip, "", "92857W309")
	tests := []struct {
		name      string
		entries   []*market.Entry
		key       gen.StatedKey
		fetchKeys map[*market.Entry]fetchKey
		want      Offer
		findings  []string
		// won reports that a run chooses a winner for the key, so the count it
		// records is zero.
		won bool
	}{
		{
			name:    "a bare ticker lists every set of identifiers, in precedence then order",
			entries: []*market.Entry{entryA, entryB},
			key:     keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon), listing(types.AssetClassStock, "GBP", figi2, xetr)),
				entryB: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBX", isin, xlon)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}}},
				{Datasource: "a", Strongest: figi2, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi2}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xetr}}}}},
				{Datasource: "b", Strongest: isin, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}}},
			}},
		},
		{
			name:    "an ISIN lists only the sets of identifiers naming it",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "", isin),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(isin, nil, listing(types.AssetClassStock, "GBP", figi2, xetr), listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}}},
			}},
			won: true,
		},
		{
			name:    "a set of identifiers contradicting the stated currency under a bare ticker is not offered",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "USD", figi, xnas)),
			},
			want: Offer{},
		},
		{
			name:    "a set of identifiers contradicting the stated currency under an ISIN is dropped with a finding",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "GBP", isin),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(isin, nil, listing(types.AssetClassStock, "USD", figi, isin, xnas)),
			},
			want:     Offer{},
			findings: []string{"dropped stated: openfigi_share_class BBG001S5XDT5: stated GBP has no listing among USD (a)"},
		},
		{
			name:    "a venue ticker no set of identifiers names lists the sets of identifiers at other venues",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "GBP", xlon),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(xlon, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "GBP", figi2, xnas)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xetr}}}}},
				{Datasource: "a", Strongest: figi2, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi2}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xnas}}}}},
			}},
		},
		{
			name:    "a venue ticker alone names its set of identifiers, and no family is no currency",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "", xlon), listing(types.AssetClassStock, "USD", figi, xnas)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: xlon, Instrument: types.Instrument{Class: types.AssetClassStock, Listings: map[string]types.Listing{"": {Identifiers: []types.Identifier{xlon}}}}},
				{Datasource: "a", Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: map[string]types.Listing{"USD": {Identifiers: []types.Identifier{xnas}}}}},
			}},
		},
		{
			name:    "two datasources describing one security give one candidate, with the head's class",
			entries: []*market.Entry{entryA, entryB},
			key:     keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassUnknown, "GBP", figi, xlon)),
				entryB: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBX", figi, isin, xetr)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Attached: []string{"b"}, Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassUnknown, Identifiers: []types.Identifier{figi, isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon, xetr}}}}},
			}},
		},
		{
			name:    "a set agreeing with two candidates that contradict each other joins both",
			entries: []*market.Entry{entryA, entryB, entryC},
			key:     keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, cusip, xlon)),
				entryB: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, cusip2, xlon)),
				entryC: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Attached: []string{"c"}, Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, cusip, isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}}},
				{Datasource: "b", Attached: []string{"c"}, Strongest: figi, Instrument: types.Instrument{Class: types.AssetClassStock, Identifiers: []types.Identifier{figi, cusip2, isin}, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}}},
			}},
		},
		{
			name:    "two candidates of one datasource naming only one venue ticker are both offered",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", xlon), listing(types.AssetClassStock, "USD", xlon)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: xlon, Instrument: types.Instrument{Class: types.AssetClassStock, Listings: map[string]types.Listing{"GBP": {Identifiers: []types.Identifier{xlon}}}}},
				{Datasource: "a", Strongest: xlon, Instrument: types.Instrument{Class: types.AssetClassStock, Listings: map[string]types.Listing{"USD": {Identifiers: []types.Identifier{xlon}}}}},
			}},
		},
		{
			name:    "a datasource that served nothing gives a reason",
			entries: []*market.Entry{entryA, entryB},
			key:     keyOf(types.AssetClassStock, "GBP", ticker),
			fetchKeys: map[*market.Entry]fetchKey{
				entryA: {ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "paused until tomorrow"},
				entryB: servedFetchKey(ticker, []types.Identifier{ticker}),
			},
			want: Offer{Reasons: []string{"a: failed: paused until tomorrow"}},
		},
		{
			name:    "a key no datasource is asked for",
			entries: []*market.Entry{entryA},
			key:     keyOf(types.AssetClassStock, "USD", id(types.IdentifierTypeBrokerID, "ibkr", "1")),
			want:    Offer{Reasons: []string{"no global identifier"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.entries...)
			f.records()
			f.finds()
			for _, e := range tc.entries {
				if r, ok := tc.fetchKeys[e]; ok {
					f.serves(e, r)
				}
			}
			got, err := f.resolver.Candidates(context.Background(), res, tc.key)
			if err != nil {
				t.Fatalf("Candidates() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Candidates() mismatch (-want +got):\n%s", diff)
			}
			var findings []string
			for _, w := range f.findings {
				if w.RunID != res.ID || w.StatedKeyID == nil || *w.StatedKeyID != tc.key.ID || w.FetchKeyID == nil {
					t.Errorf("finding %+v, want it against the run, the key and a fetch key", w)
				}
				findings = append(findings, fmt.Sprintf("%s %s: %s", w.Kind, *w.Step, *w.Detail))
			}
			if diff := cmp.Diff(tc.findings, findings); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
			if f.resolutions != 0 {
				t.Errorf("%d resolution keys written, want none: a listing stops before choosing", f.resolutions)
			}
			if tc.won {
				return
			}
			// A run over the same answers records the count of the
			// candidates offered.
			f = newFixture(t, tc.entries...)
			f.records()
			f.finds()
			for _, e := range tc.entries {
				if r, ok := tc.fetchKeys[e]; ok {
					f.serves(e, r)
				}
			}
			if rk := f.resolve(tc.key); int(rk[0].Offered) != len(got.Candidates) {
				t.Errorf("a run recorded %d offered, want %d, the count of the candidates", rk[0].Offered, len(got.Candidates))
			}
		})
	}
}

// TestConfirm checks that a confirmation takes the head of the candidate
// the pick names as the winner, attaches its members, carries the
// association to the key's group, lands on an instrument a member
// identifies, and fails where the answers no longer offer the candidate or
// it fails a check.
func TestConfirm(t *testing.T) {
	gbpMate, usdMate := keyOf(types.AssetClassStock, "GBP", descr), keyOf(types.AssetClassStock, "USD", descr)
	other := gen.Instrument{ID: db.NewID(), AssetClass: types.AssetClassStock}
	cusip2 := id(types.IdentifierTypeCusip, "", "92857W309")
	tests := []struct {
		name string
		key  gen.StatedKey
		a, b fetchKey
		// c is served by a third datasource where it is set.
		c    *fetchKey
		pick Pick
		// mates is the key's group, and wantListed whether each mate takes
		// the listing.
		mates      []gen.StatedKey
		wantListed []bool
		// stored is what the database holds; the confirmation lands on it
		// where it is set.
		stored []gen.ListInstrumentsByIdentifiersRow
		// wantAttached is the sources attached, the winner first; empty
		// for a confirmation that fails.
		wantAttached []string
		wantFinding  string
		// wantClass is the class of the instrument created, and wantFamily
		// the family of its first listing, where they are set.
		wantClass  types.AssetClass
		wantFamily string
	}{
		{
			name:         "a mate takes the listing where it states its currency",
			key:          keyOf(types.AssetClassStock, "GBP", ticker, descr),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			pick:         Pick{Datasource: "a", Identifier: figi},
			mates:        []gen.StatedKey{gbpMate, usdMate},
			wantListed:   []bool{true, false},
			wantAttached: []string{"a", "b"},
		},
		{
			name:         "a member identifies an instrument, and the confirmation lands on it",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", isin, xlon)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			pick:         Pick{Datasource: "a", Identifier: isin},
			stored:       []gen.ListInstrumentsByIdentifiersRow{{Instrument: other, Identifier: gen.Identifier{ID: db.NewID(), InstrumentID: other.ID, Type: figi.Type, Value: figi.Value}}},
			wantAttached: []string{"a", "b"},
		},
		{
			name:         "the head wins and the members attach",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			pick:         Pick{Datasource: "a", Identifier: figi},
			wantAttached: []string{"a", "b"},
		},
		{
			name:         "a head of class unknown creates an instrument of class unknown",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassUnknown, "GBP", figi, xlon)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			pick:         Pick{Datasource: "a", Identifier: figi},
			wantAttached: []string{"a", "b"},
			wantClass:    types.AssetClassUnknown,
		},
		{
			name: "a pick naming a member that is not the head",
			key:  keyOf(types.AssetClassStock, "GBP", ticker),
			a:    servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xlon)),
			b:    servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			pick: Pick{Datasource: "b", Identifier: isin},
		},
		{
			name:         "of two candidates that contradict each other, the second takes the set both gather and drops the first",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, cusip, xlon)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, cusip2, xlon)),
			c:            ptr.To(servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon))),
			pick:         Pick{Datasource: "b", Identifier: figi},
			wantAttached: []string{"b", "c"},
			wantFinding:  "dropped precedence: openfigi_share_class BBG001S5XDT5: cusip 92857W308 contradicts cusip 92857W309 (b)",
		},
		{
			name:         "a set that a better ranked set of its datasource displaced is written alone",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, isin, xlon)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi, xetr), listing(types.AssetClassStock, "GBP", isin, xlon)),
			pick:         Pick{Datasource: "b", Identifier: isin},
			wantAttached: []string{"b"},
		},
		{
			name:         "of two heads that one venue ticker identifies, the first is written",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}),
			b:            limitedFetchKey(servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", xlon), listing(types.AssetClassStock, "USD", xlon))),
			pick:         Pick{Datasource: "b", Identifier: xlon},
			wantAttached: []string{"b"},
			wantFamily:   "GBP",
		},
		{
			name:         "a higher datasource sharing no stable identifier is dropped",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", figi2, xetr)),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", isin, xlon)),
			pick:         Pick{Datasource: "b", Identifier: isin},
			wantAttached: []string{"b"},
			wantFinding:  "dropped corroboration: openfigi_share_class BBG001S5XDT6: shares no stable identifier with the candidate identified by isin GB00BH4HKS39 (b)",
		},
		{
			name:         "a pick named by a listing identifier is found",
			key:          keyOf(types.AssetClassStock, "GBP", ticker),
			a:            servedFetchKey(ticker, []types.Identifier{ticker}),
			b:            servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", isin, xlon)),
			pick:         Pick{Datasource: "b", Identifier: xlon},
			wantAttached: []string{"b"},
		},
		{
			name: "an identifier no set of identifiers carries",
			key:  keyOf(types.AssetClassStock, "GBP", ticker),
			a:    servedFetchKey(ticker, []types.Identifier{ticker}),
			b:    servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "GBP", isin, xlon)),
			pick: Pick{Datasource: "b", Identifier: cusip},
		},
		{
			name: "a datasource that served nothing",
			key:  keyOf(types.AssetClassStock, "GBP", ticker),
			a:    servedFetchKey(ticker, []types.Identifier{ticker}),
			b:    fetchKey{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "503"},
			pick: Pick{Datasource: "b", Identifier: isin},
		},
		{
			name: "a datasource that was not asked",
			key:  keyOf(types.AssetClassStock, "GBP", ticker),
			a:    servedFetchKey(ticker, []types.Identifier{ticker}),
			b:    servedFetchKey(ticker, []types.Identifier{ticker}),
			pick: Pick{Datasource: "c", Identifier: isin},
		},
		{
			name: "a pick whose listing is in no stated family",
			key:  keyOf(types.AssetClassStock, "GBP", ticker),
			a:    servedFetchKey(ticker, []types.Identifier{ticker}),
			b:    servedFetchKey(ticker, []types.Identifier{ticker}, listing(types.AssetClassStock, "USD", isin, xnas)),
			pick: Pick{Datasource: "b", Identifier: isin},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			entries := []*market.Entry{entryA, entryB}
			if tc.c != nil {
				entries = append(entries, entryC)
			}
			f := newFixture(t, entries...)
			f.mates, f.stored = tc.mates, tc.stored
			f.records()
			f.finds()
			f.creates()
			f.serves(entryA, tc.a)
			f.serves(entryB, tc.b)
			if tc.c != nil {
				f.serves(entryC, *tc.c)
			}
			got, err := f.resolver.Confirm(context.Background(), res, tc.key, tc.pick)
			if len(tc.wantAttached) == 0 {
				if !errors.Is(err, ErrNoCandidate) {
					t.Fatalf("Confirm() = %+v, %v, want ErrNoCandidate", got, err)
				}
				if len(f.associations) != 0 || len(f.instruments) != 0 || f.resolutions != 0 {
					t.Errorf("a failed confirmation wrote %d associations, %d instruments and %d resolutions, want none", len(f.associations), len(f.instruments), f.resolutions)
				}
				return
			}
			if err != nil {
				t.Fatalf("Confirm() error = %v", err)
			}
			if got.Outcome != gen.ResolutionOutcomeMatched || got.StatedKeyID != tc.key.ID {
				t.Errorf("Confirm() = %+v, want the key matched", got)
			}
			if f.txs != 1 {
				t.Errorf("Confirm() ran %d transactions, want 1", f.txs)
			}
			byID := map[uuid.UUID]string{tc.a.ID: "a", tc.b.ID: "b"}
			if tc.c != nil {
				byID[tc.c.ID] = "c"
			}
			var attached []string
			for _, id := range f.attached {
				attached = append(attached, byID[id])
			}
			if diff := cmp.Diff(tc.wantAttached, attached); diff != "" {
				t.Errorf("attached mismatch (-want +got):\n%s", diff)
			}
			switch {
			case len(tc.stored) > 0:
				if len(f.instruments) != 0 || len(f.associations) == 0 || *f.associations[0].InstrumentID != tc.stored[0].Instrument.ID {
					t.Errorf("instruments = %+v, associations = %+v, want the key on the instrument found and none created", f.instruments, f.associations)
				}
				for _, id := range f.identifiers {
					if id.Type == tc.stored[0].Identifier.Type && id.Value == tc.stored[0].Identifier.Value {
						t.Errorf("created %s again on the instrument that has it", id.Value)
					}
				}
			case len(f.instruments) != 1 || f.instruments[0].FetchKeyID == nil || byID[*f.instruments[0].FetchKeyID] != tc.wantAttached[0]:
				t.Errorf("instruments = %+v, want one with the winner's fetch key as provenance", f.instruments)
			case tc.wantClass != "" && f.instruments[0].AssetClass != tc.wantClass:
				t.Errorf("instrument class = %s, want %s", f.instruments[0].AssetClass, tc.wantClass)
			}
			if tc.wantFamily != "" && (len(f.listings) == 0 || f.listings[0].Currency != tc.wantFamily) {
				t.Errorf("listings = %+v, want the first in %s", f.listings, tc.wantFamily)
			}
			if len(f.associations) != 1+len(tc.mates) || f.associations[0].ID != tc.key.ID || f.associations[0].Arbiter != gen.ArbiterUser || *f.associations[0].Validity != gen.ValidityProvisional || f.associations[0].ListingID == nil {
				t.Errorf("associations = %+v, want the key's by the user, provisional through the venue ticker, on the listing, then one per mate", f.associations)
			}
			for i, m := range tc.mates {
				if i+1 >= len(f.associations) {
					break
				}
				if a := f.associations[i+1]; a.ID != m.ID || a.InstrumentID == nil || *a.InstrumentID != *f.associations[0].InstrumentID || (a.ListingID != nil) != tc.wantListed[i] || a.Arbiter != gen.ArbiterUser {
					t.Errorf("mate %d association = %+v, want the key's instrument by the user, listed %t", i, a, tc.wantListed[i])
				}
			}
			if f.resolutions != 1+len(tc.mates) {
				t.Errorf("wrote %d resolution keys, want one for the key and one per mate", f.resolutions)
			}
			var findings []string
			for _, w := range f.findings {
				findings = append(findings, fmt.Sprintf("%s %s: %s", w.Kind, *w.Step, *w.Detail))
			}
			var want []string
			if tc.wantFinding != "" {
				want = []string{tc.wantFinding}
			}
			if diff := cmp.Diff(want, findings); diff != "" {
				t.Errorf("findings mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
