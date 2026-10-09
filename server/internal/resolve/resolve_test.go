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
	// associations is every association written, and rows is what each
	// write reports it changed.
	associations []gen.SetStatedKeyAssociationParams
	rows         int64
	// arbitrated is the user's keys the user arbitrated.
	arbitrated []gen.StatedKey
	// instruments is every instrument created, attached every fetch key
	// attached, and resolutions how many resolution keys were written.
	instruments []gen.CreateInstrumentParams
	attached    []uuid.UUID
	resolutions int
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
		return gen.StatedKey{ID: arg.ID, UserID: arg.UserID}, nil
	}).AnyTimes()
	f.store.EXPECT().ListStatedKeysOfGroups(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	f.store.EXPECT().CreateResolutionKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateResolutionKeyParams) (gen.ResolutionKey, error) {
		f.resolutions++
		return gen.ResolutionKey{RunID: arg.RunID, UserID: arg.UserID, StatedKeyID: arg.StatedKeyID, Outcome: arg.Outcome, Reasons: arg.Reasons}, nil
	}).AnyTimes()
}

// findsNothing has every read of the database find no instrument.
func (f *fixture) findsNothing() {
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
}

// creates accepts every write of a created instrument.
func (f *fixture) creates() {
	f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error) {
		f.instruments = append(f.instruments, arg)
		return gen.Instrument{ID: arg.ID, AssetClass: arg.AssetClass, FetchKeyID: arg.FetchKeyID}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateListingParams) (gen.Listing, error) {
		return gen.Listing{ID: arg.ID, InstrumentID: arg.InstrumentID, Currency: arg.Currency}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error) {
		return gen.Identifier{ID: arg.ID, InstrumentID: arg.InstrumentID, ListingID: arg.ListingID, Type: arg.Type, Domain: arg.Domain, Value: arg.Value}, nil
	}).AnyTimes()
	f.store.EXPECT().UpsertIdentityCoverage(gomock.Any(), gomock.Any()).AnyTimes()
	f.store.EXPECT().SetFetchKeyInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.SetFetchKeyInstrumentParams) error {
		f.attached = append(f.attached, arg.ID)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateFetchIdentifier(gomock.Any(), gomock.Any()).AnyTimes()
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
		reasons  []string
		findings []finding
	}{
		{
			name:    "nothing recognised",
			key:     keyOf(gen.AssetClassStock, "USD", id(types.IdentifierTypeBrokerID, "ibkr", "1")),
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"no global identifier"},
		},
		{
			name:    "no datasource served it",
			key:     keyOf(gen.AssetClassStock, "USD", isin),
			results: []result{{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no recognised identifier type"}},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"a: skipped: no recognised identifier type"},
		},
		{
			name:    "a datasource failed",
			key:     keyOf(gen.AssetClassStock, "USD", isin),
			results: []result{{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "503"}},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnavailable,
			reasons: []string{"a: failed: 503"},
		},
		{
			name:    "a bare ticker is sent and never associates",
			key:     keyOf(gen.AssetClassStock, "GBP", ticker),
			results: []result{servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, xlon))},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"a: 1 candidate in 1 group, 1 group not naming mic_ticker VOD"},
		},
		{
			name:    "a description that matches no instrument",
			key:     keyOf(gen.AssetClassStock, "USD", descr),
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"failed to match broker description"},
		},
		{
			name:    "a bare ticker beside an unknown description",
			key:     keyOf(gen.AssetClassStock, "GBP", ticker, descr),
			results: []result{servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, xlon))},
			fetched: true,
			outcome: gen.ResolutionOutcomeUnrecognised,
			reasons: []string{"a: 1 candidate in 1 group, 1 group not naming mic_ticker VOD"},
		},
		{
			name:     "every group dropped",
			key:      keyOf(gen.AssetClassStock, "GBP", isin),
			results:  []result{servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "USD", figi, xnas))},
			fetched:  true,
			outcome:  gen.ResolutionOutcomeUnrecognised,
			reasons:  []string{"a: 1 candidate in 1 group, 1 group dropped"},
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
			want := []gen.ResolutionKey{{RunID: res.ID, UserID: userID, StatedKeyID: tc.key.ID, Outcome: tc.outcome, Reasons: tc.reasons}}
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

// TestArbiter checks the arbiter each association records, and that a key
// the user arbitrates during the run keeps that association and is
// recorded matched.
func TestArbiter(t *testing.T) {
	inst := gen.Instrument{ID: db.NewID(), AssetClass: gen.AssetClassStock}
	isinRow := gen.Identifier{ID: db.NewID(), InstrumentID: inst.ID, Type: isin.Type, Value: isin.Value}
	t.Run("a fetched winner is the datasource's", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.findsNothing()
		f.creates()
		f.serves(entryA, servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "GBX", figi, isin, xlon)))
		got := f.resolve(keyOf(gen.AssetClassStock, "GBX", isin))
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
		got := f.resolve(keyOf(gen.AssetClassStock, "", isin))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.associations) != 1 || f.associations[0].Arbiter != gen.ArbiterStated {
			t.Errorf("Resolve = %+v with associations %+v, want matched with the stated data as arbiter", got, f.associations)
		}
	})
	t.Run("an association the user arbitrated stands", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.findsNothing()
		f.creates()
		f.rows = 0
		f.serves(entryA, servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "GBX", figi, isin, xlon)))
		got := f.resolve(keyOf(gen.AssetClassStock, "GBX", isin))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.instruments) != 1 || len(f.attached) != 1 {
			t.Errorf("Resolve = %+v, instruments %d, attached %d: want matched with the instrument data written", got, len(f.instruments), len(f.attached))
		}
	})
}

// TestInherit checks that a key sharing a joining identifier with a key the
// user arbitrated takes its association before any datasource is asked.
func TestInherit(t *testing.T) {
	descr2 := id(types.IdentifierTypeBrokerDescription, "ibkr", "ACME CORP 2")
	instX, instY := db.NewID(), db.NewID()
	listing, via := db.NewID(), db.NewID()
	confirmed := func(instrument uuid.UUID, ids ...types.Identifier) gen.StatedKey {
		k := keyOf(gen.AssetClassStock, "GBP", ids...)
		k.InstrumentID, k.ListingID, k.ViaID = &instrument, &listing, &via
		k.Validity, k.Arbiter = ptr.To(gen.ValidityProvisional), ptr.To(gen.ArbiterUser)
		return k
	}
	t.Run("a key stating a confirmed key's description inherits", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.findsNothing()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr)}
		got := f.resolve(keyOf(gen.AssetClassStock, "GBP", ticker, descr))
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
		f.findsNothing()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr)}
		got := f.resolve(keyOf(gen.AssetClassStock, "USD", ticker, descr))
		if got[0].Outcome != gen.ResolutionOutcomeMatched || len(f.associations) != 1 || f.associations[0].ListingID != nil {
			t.Errorf("Resolve = %+v with associations %+v, want matched on the instrument with no listing", got, f.associations)
		}
	})
	t.Run("a bare ticker alone joins nothing", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.findsNothing()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr)}
		f.serves(entryA, result{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no"})
		got := f.resolve(keyOf(gen.AssetClassStock, "GBP", ticker))
		if got[0].Outcome != gen.ResolutionOutcomeUnrecognised || len(f.associations) != 0 {
			t.Errorf("Resolve = %+v with associations %+v, want unrecognised after asking the datasource", got, f.associations)
		}
	})
	t.Run("confirmed keys naming different instruments leave the key to the datasources", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.records()
		f.findsNothing()
		f.arbitrated = []gen.StatedKey{confirmed(instX, ticker, descr), confirmed(instY, ticker, descr2)}
		f.serves(entryA, result{ID: db.NewID(), Outcome: gen.FetchOutcomeNotServed, Reason: "no"})
		got := f.resolve(keyOf(gen.AssetClassStock, "GBP", ticker, descr, descr2))
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
	gbp, usd := "GBP", "USD"
	tests := []struct {
		name     string
		entries  []*market.Entry
		key      gen.StatedKey
		results  map[*market.Entry]result
		want     Offer
		findings []string
	}{
		{
			name:    "a bare ticker lists every group, in precedence then order",
			entries: []*market.Entry{entryA, entryB},
			key:     keyOf(gen.AssetClassStock, "GBP", ticker),
			results: map[*market.Entry]result{
				entryA: servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, xlon), cand(gen.AssetClassStock, "GBP", figi2, xetr)),
				entryB: servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBX", isin, xlon)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: figi, AssetClass: gen.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: []CandidateListing{{Currency: &gbp, Identifiers: []types.Identifier{xlon}}}},
				{Datasource: "a", Strongest: figi2, AssetClass: gen.AssetClassStock, Identifiers: []types.Identifier{figi2}, Listings: []CandidateListing{{Currency: &gbp, Identifiers: []types.Identifier{xetr}}}},
				{Datasource: "b", Strongest: isin, AssetClass: gen.AssetClassStock, Identifiers: []types.Identifier{isin}, Listings: []CandidateListing{{Currency: &gbp, Identifiers: []types.Identifier{xlon}}}},
			}},
		},
		{
			name:    "an ISIN lists only the groups naming it",
			entries: []*market.Entry{entryA},
			key:     keyOf(gen.AssetClassStock, "", isin),
			results: map[*market.Entry]result{
				entryA: servedResult(isin, nil, cand(gen.AssetClassStock, "GBP", figi2, xetr), cand(gen.AssetClassStock, "GBP", figi, isin, xlon)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: figi, AssetClass: gen.AssetClassStock, Identifiers: []types.Identifier{figi, isin}, Listings: []CandidateListing{{Currency: &gbp, Identifiers: []types.Identifier{xlon}}}},
			}},
		},
		{
			name:    "a group contradicting the stated currency is dropped with a finding",
			entries: []*market.Entry{entryA},
			key:     keyOf(gen.AssetClassStock, "GBP", ticker),
			results: map[*market.Entry]result{
				entryA: servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "USD", figi, xnas)),
			},
			want:     Offer{},
			findings: []string{"dropped stated: openfigi_share_class BBG001S5XDT5: stated GBP has no listing among USD (a)"},
		},
		{
			name:    "a venue ticker alone names its group, and no family is no currency",
			entries: []*market.Entry{entryA},
			key:     keyOf(gen.AssetClassStock, "", ticker),
			results: map[*market.Entry]result{
				entryA: servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "", xlon), cand(gen.AssetClassStock, "USD", figi, xnas)),
			},
			want: Offer{Candidates: []Candidate{
				{Datasource: "a", Strongest: xlon, AssetClass: gen.AssetClassStock, Listings: []CandidateListing{{Identifiers: []types.Identifier{xlon}}}},
				{Datasource: "a", Strongest: figi, AssetClass: gen.AssetClassStock, Identifiers: []types.Identifier{figi}, Listings: []CandidateListing{{Currency: &usd, Identifiers: []types.Identifier{xnas}}}},
			}},
		},
		{
			name:    "a datasource that served nothing gives a reason",
			entries: []*market.Entry{entryA, entryB},
			key:     keyOf(gen.AssetClassStock, "GBP", ticker),
			results: map[*market.Entry]result{
				entryA: {ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "paused until tomorrow"},
				entryB: servedResult(ticker, []types.Identifier{ticker}),
			},
			want: Offer{Reasons: []string{"a: failed: paused until tomorrow"}},
		},
		{
			name:    "a key no datasource is asked for",
			entries: []*market.Entry{entryA},
			key:     keyOf(gen.AssetClassStock, "USD", id(types.IdentifierTypeBrokerID, "ibkr", "1")),
			want:    Offer{Reasons: []string{"no global identifier"}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, tc.entries...)
			f.records()
			f.findsNothing()
			for _, e := range tc.entries {
				if r, ok := tc.results[e]; ok {
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
		})
	}
}

// TestConfirm checks that a confirmation takes the pick as the winner,
// attaches the other datasources by the usual rules, and fails where the
// answer lacks the pick or the pick fails a check.
func TestConfirm(t *testing.T) {
	tests := []struct {
		name string
		key  gen.StatedKey
		a, b result
		pick Pick
		// wantAttached is the sources attached, the winner first; empty
		// for a confirmation that fails.
		wantAttached []string
		wantFinding  string
	}{
		{
			name:         "the pick in the lowest precedence datasource wins and a higher one attaches",
			key:          keyOf(gen.AssetClassStock, "GBP", ticker),
			a:            servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, xlon)),
			b:            servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi, isin, xlon)),
			pick:         Pick{Datasource: "b", Identifier: isin},
			wantAttached: []string{"b", "a"},
		},
		{
			name:         "a higher datasource sharing no stable identifier is dropped",
			key:          keyOf(gen.AssetClassStock, "GBP", ticker),
			a:            servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", figi2, xetr)),
			b:            servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", isin, xlon)),
			pick:         Pick{Datasource: "b", Identifier: isin},
			wantAttached: []string{"b"},
			wantFinding:  "dropped corroboration: openfigi_share_class BBG001S5XDT6: shares no stable identifier with the candidate identified by isin GB00BH4HKS39 (b)",
		},
		{
			name:         "a pick named by a listing identifier is found",
			key:          keyOf(gen.AssetClassStock, "GBP", ticker),
			a:            servedResult(ticker, []types.Identifier{ticker}),
			b:            servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", isin, xlon)),
			pick:         Pick{Datasource: "b", Identifier: xlon},
			wantAttached: []string{"b"},
		},
		{
			name: "an identifier no group carries",
			key:  keyOf(gen.AssetClassStock, "GBP", ticker),
			a:    servedResult(ticker, []types.Identifier{ticker}),
			b:    servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "GBP", isin, xlon)),
			pick: Pick{Datasource: "b", Identifier: cusip},
		},
		{
			name: "a datasource that served nothing",
			key:  keyOf(gen.AssetClassStock, "GBP", ticker),
			a:    servedResult(ticker, []types.Identifier{ticker}),
			b:    result{ID: db.NewID(), Outcome: gen.FetchOutcomeFailedTemporary, Reason: "503"},
			pick: Pick{Datasource: "b", Identifier: isin},
		},
		{
			name: "a datasource that was not asked",
			key:  keyOf(gen.AssetClassStock, "GBP", ticker),
			a:    servedResult(ticker, []types.Identifier{ticker}),
			b:    servedResult(ticker, []types.Identifier{ticker}),
			pick: Pick{Datasource: "c", Identifier: isin},
		},
		{
			name: "a pick whose listing is in no stated family",
			key:  keyOf(gen.AssetClassStock, "GBP", ticker),
			a:    servedResult(ticker, []types.Identifier{ticker}),
			b:    servedResult(ticker, []types.Identifier{ticker}, cand(gen.AssetClassStock, "USD", isin, xnas)),
			pick: Pick{Datasource: "b", Identifier: isin},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, entryA, entryB)
			f.records()
			f.findsNothing()
			f.creates()
			f.serves(entryA, tc.a)
			f.serves(entryB, tc.b)
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
			byID := map[uuid.UUID]string{tc.a.ID: "a", tc.b.ID: "b"}
			var attached []string
			for _, id := range f.attached {
				attached = append(attached, byID[id])
			}
			if diff := cmp.Diff(tc.wantAttached, attached); diff != "" {
				t.Errorf("attached mismatch (-want +got):\n%s", diff)
			}
			if len(f.instruments) != 1 || f.instruments[0].FetchKeyID == nil || byID[*f.instruments[0].FetchKeyID] != tc.wantAttached[0] {
				t.Errorf("instruments = %+v, want one with the winner's fetch key as provenance", f.instruments)
			}
			if len(f.associations) != 1 || f.associations[0].Arbiter != gen.ArbiterUser || *f.associations[0].Validity != gen.ValidityProvisional {
				t.Errorf("associations = %+v, want one by the user, provisional through the venue ticker", f.associations)
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
