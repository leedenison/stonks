package resolve

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"time"

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
	userID     = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	res        = gen.Run{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), UserID: userID, Kind: gen.RunKindResolution}
	currencies = []gen.Currency{{Code: "EUR", Family: "EUR"}, {Code: "GBP", Family: "GBP"}, {Code: "GBX", Family: "GBP"}, {Code: "USD", Family: "USD"}}
	entryA     = &market.Entry{Name: "a", Precedence: 10}
	entryB     = &market.Entry{Name: "b", Precedence: 20}
)

// fixture is a resolver whose store runs a transaction inline over itself,
// recording every row written.
type fixture struct {
	t        *testing.T
	store    *MockStore
	fetcher  *MockFetcher
	sources  *MockSources
	resolver *Resolver

	instruments []gen.CreateInstrumentParams
	listings    []gen.CreateListingParams
	identifiers []gen.CreateIdentifierParams
	coverage    []gen.UpsertIdentityCoverageParams
	attached    []gen.SetFetchKeyInstrumentParams
	asserted    []gen.CreateFetchIdentifierParams
	findings    []gen.CreateFindingParams
	associated  []gen.SetStatedKeyAssociationParams
	resolved    []gen.CreateResolutionKeyParams
	locked      [][]string
	// conflicts is how many transactions are refused as a concurrent insert
	// would, after their writes.
	conflicts int
	// stored is what the store has, and merges the merge steps run against it.
	stored []stored
	merges []string
}

func newFixture(t *testing.T, entries ...*market.Entry) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{t: t, store: NewMockStore(ctrl), fetcher: NewMockFetcher(ctrl), sources: NewMockSources(ctrl)}
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error {
		err := fn(f.store)
		if err == nil && f.conflicts > 0 {
			f.conflicts--
			return &pgconn.PgError{Code: pgerrcode.UniqueViolation}
		}
		return err
	}).AnyTimes()
	f.store.EXPECT().ListCurrencies(gomock.Any()).Return(currencies, nil).AnyTimes()
	f.store.EXPECT().LockIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, keys []string) error {
		f.locked = append(f.locked, keys)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error) {
		f.instruments = append(f.instruments, arg)
		return gen.Instrument{ID: arg.ID, AssetClass: arg.AssetClass, FetchKeyID: arg.FetchKeyID}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateListingParams) (gen.Listing, error) {
		f.listings = append(f.listings, arg)
		return gen.Listing{ID: arg.ID, InstrumentID: arg.InstrumentID, Currency: arg.Currency, FetchKeyID: arg.FetchKeyID}, nil
	}).AnyTimes()
	f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error) {
		f.identifiers = append(f.identifiers, arg)
		return gen.Identifier{ID: arg.ID, InstrumentID: arg.InstrumentID, ListingID: arg.ListingID, Type: arg.Type, Domain: arg.Domain, Value: arg.Value}, nil
	}).AnyTimes()
	f.store.EXPECT().UpsertIdentityCoverage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.UpsertIdentityCoverageParams) error {
		f.coverage = append(f.coverage, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().SetFetchKeyInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.SetFetchKeyInstrumentParams) error {
		f.attached = append(f.attached, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateFetchIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateFetchIdentifierParams) error {
		f.asserted = append(f.asserted, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateFinding(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateFindingParams) error {
		f.findings = append(f.findings, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().SetStatedKeyAssociation(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.SetStatedKeyAssociationParams) error {
		f.associated = append(f.associated, arg)
		return nil
	}).AnyTimes()
	f.store.EXPECT().CreateResolutionKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateResolutionKeyParams) error {
		f.resolved = append(f.resolved, arg)
		return nil
	}).AnyTimes()
	f.sources.EXPECT().Enabled().Return(entries).AnyTimes()
	merge := func(step string) func(context.Context, any) error {
		return func(_ context.Context, arg any) error {
			f.merges = append(f.merges, fmt.Sprintf("%s %v", step, arg))
			return nil
		}
	}
	f.store.EXPECT().DeferConstraints(gomock.Any()).DoAndReturn(func(context.Context) error {
		f.merges = append(f.merges, "defer")
		return nil
	}).AnyTimes()
	f.store.EXPECT().MoveListing(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.MoveListingParams) error {
		return merge("move listing")(context.Background(), arg.ID)
	}).AnyTimes()
	f.store.EXPECT().RelinkIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.RelinkIdentifiersParams) error {
		var survivor, loser *stored
		for i := range f.stored {
			switch f.stored[i].instrument.ID {
			case arg.Survivor:
				survivor = &f.stored[i]
			case arg.Loser:
				loser = &f.stored[i]
			}
		}
		for _, id := range loser.identifiers {
			id.InstrumentID = survivor.instrument.ID
			survivor.identifiers = append(survivor.identifiers, id)
		}
		loser.identifiers = nil
		return merge("relink identifiers")(context.Background(), arg.Loser)
	}).AnyTimes()
	f.store.EXPECT().RelinkStatedKeys(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.RelinkStatedKeysParams) error {
		return merge("relink stated keys")(context.Background(), arg.Loser)
	}).AnyTimes()
	f.store.EXPECT().RelinkFetchKeys(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.RelinkFetchKeysParams) error {
		return merge("relink fetch keys")(context.Background(), arg.Loser)
	}).AnyTimes()
	f.store.EXPECT().MoveIdentityCoverage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.MoveIdentityCoverageParams) error {
		return merge("move coverage")(context.Background(), arg.Loser)
	}).AnyTimes()
	f.store.EXPECT().DeleteIdentityCoverage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) error {
		return merge("delete coverage")(context.Background(), id)
	}).AnyTimes()
	f.store.EXPECT().DeleteListings(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) error {
		return merge("delete listings")(context.Background(), id)
	}).AnyTimes()
	f.store.EXPECT().DeleteInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) error {
		return merge("delete instrument")(context.Background(), id)
	}).AnyTimes()
	f.resolver = New(f.store, f.fetcher, f.sources, slog.New(slog.DiscardHandler))
	return f
}

// notFound has the store find none of the identifiers of the lookup and
// the re-read.
func (f *fixture) notFound() {
	f.store.EXPECT().FindIdentifier(gomock.Any(), gomock.Any()).Return(gen.FindIdentifierRow{}, db.ErrNotFound).AnyTimes()
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
}

// stored is an instrument the store has, found by any of its identifiers
// in the lookup and the re-read.
type stored struct {
	instrument  gen.Instrument
	identifiers []gen.Identifier
	listings    []gen.Listing
	covered     []string
}

// stores adds h to what the store has. The reads consult every stored
// instrument, so a later call adds a second.
func (f *fixture) stores(h stored) {
	f.stored = append(f.stored, h)
	if len(f.stored) > 1 {
		return
	}
	f.store.EXPECT().FindIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error) {
		for _, h := range f.stored {
			for _, id := range h.identifiers {
				if id.Type == arg.Type && id.Domain == arg.Domain && id.Value == arg.Value {
					return gen.FindIdentifierRow{Identifier: id, Instrument: h.instrument}, nil
				}
			}
		}
		return gen.FindIdentifierRow{}, db.ErrNotFound
	}).AnyTimes()
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.ListInstrumentsByIdentifiersParams) ([]gen.ListInstrumentsByIdentifiersRow, error) {
		var rows []gen.ListInstrumentsByIdentifiersRow
		for _, h := range f.stored {
			for i := range arg.Types {
				for _, id := range h.identifiers {
					if string(id.Type) == arg.Types[i] && id.Domain == arg.Domains[i] && id.Value == arg.Values[i] {
						rows = append(rows, gen.ListInstrumentsByIdentifiersRow{Identifier: id, Instrument: h.instrument})
					}
				}
			}
		}
		return rows, nil
	}).AnyTimes()
	f.store.EXPECT().ListIdentifiers(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) ([]gen.Identifier, error) {
		return f.storedBy(id).identifiers, nil
	}).AnyTimes()
	f.store.EXPECT().ListListings(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) ([]gen.Listing, error) {
		return f.storedBy(id).listings, nil
	}).AnyTimes()
	f.store.EXPECT().ListIdentityCoverage(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, ids []uuid.UUID) ([]gen.IdentityCoverage, error) {
		var coverage []gen.IdentityCoverage
		for _, id := range ids {
			for _, c := range f.storedBy(id).covered {
				coverage = append(coverage, gen.IdentityCoverage{InstrumentID: id, Datasource: c})
			}
		}
		return coverage, nil
	}).AnyTimes()
}

func (f *fixture) storedBy(id uuid.UUID) stored {
	for _, h := range f.stored {
		if h.instrument.ID == id {
			return h
		}
	}
	return stored{}
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

// TestResolveCurrency checks the currency path: a cash key resolves against
// the seed alone, confirmed through the currency identifier, and is
// rejected where it contradicts the seed.
func TestResolveCurrency(t *testing.T) {
	cashID, listingID, viaID := db.NewID(), db.NewID(), db.NewID()
	usd := stored{
		instrument:  gen.Instrument{ID: cashID, AssetClass: gen.AssetClassCash},
		identifiers: []gen.Identifier{{ID: viaID, InstrumentID: cashID, Type: types.IdentifierTypeCurrency, Value: "USD"}},
		listings:    []gen.Listing{{ID: listingID, InstrumentID: cashID, Currency: "USD"}},
	}
	currency := id(types.IdentifierTypeCurrency, "", "USD")
	tests := []struct {
		name    string
		key     gen.StatedKey
		outcome gen.ResolutionOutcome
		reason  string
	}{
		{name: "matched", key: keyOf(gen.AssetClassCash, "USD", currency), outcome: gen.ResolutionOutcomeMatched},
		{name: "no currency", key: keyOf(gen.AssetClassCash, "XXX", id(types.IdentifierTypeCurrency, "", "XXX")), outcome: gen.ResolutionOutcomeRejected, reason: "no currency XXX"},
		{name: "class", key: keyOf(gen.AssetClassEquity, "USD", currency), outcome: gen.ResolutionOutcomeRejected, reason: "asset class equity contradicts the instrument's cash"},
		{name: "no listing", key: keyOf(gen.AssetClassCash, "EUR", currency), outcome: gen.ResolutionOutcomeRejected, reason: "no listing of USD in EUR"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, entryA)
			f.stores(usd)
			got := f.resolve(tc.key)
			want := []gen.ResolutionKey{{RunID: res.ID, UserID: userID, StatedKeyID: tc.key.ID, Outcome: tc.outcome}}
			if tc.reason != "" {
				want[0].Reason = &tc.reason
			}
			if diff := cmp.Diff(want, got); diff != "" {
				t.Errorf("Resolve mismatch (-want +got):\n%s", diff)
			}
			if tc.outcome != gen.ResolutionOutcomeMatched {
				if len(f.associated) != 0 {
					t.Errorf("associated %+v, want nothing", f.associated)
				}
				return
			}
			confirmed := gen.ValidityConfirmed
			wantAssoc := []gen.SetStatedKeyAssociationParams{{ID: tc.key.ID, UserID: userID, InstrumentID: &cashID, ListingID: &listingID, ViaID: &viaID, Validity: &confirmed}}
			if diff := cmp.Diff(wantAssoc, f.associated); diff != "" {
				t.Errorf("association mismatch (-want +got):\n%s", diff)
			}
			if len(f.instruments)+len(f.identifiers)+len(f.coverage) != 0 {
				t.Errorf("a currency key wrote %d instruments, %d identifiers, %d coverage rows, want none", len(f.instruments), len(f.identifiers), len(f.coverage))
			}
		})
	}
}

// TestResolveCreate checks that a key nothing names is created from the
// winner with its listing, identifiers, provenance and coverage, and is
// associated through the strongest identifier it stated.
func TestResolveCreate(t *testing.T) {
	f := newFixture(t, entryA)
	f.notFound()
	k := keyOf(gen.AssetClassStock, "GBX", isin, xlon)
	r := servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "GBX", figi, isin, comp, xlon), cand(gen.AssetClassStock, "GBX", figi, xetr))
	f.serves(entryA, r)

	got := f.resolve(k)
	if len(got) != 1 || got[0].Outcome != gen.ResolutionOutcomeMatched || got[0].Reason != nil {
		t.Fatalf("Resolve = %+v, want matched with no reason", got)
	}
	if diff := cmp.Diff([][]string{{"identifier:isin::GB00BH4HKS39", "identifier:mic_ticker:XLON:VOD"}}, f.locked); diff != "" {
		t.Errorf("locks mismatch (-want +got):\n%s", diff)
	}
	if len(f.instruments) != 1 || f.instruments[0].AssetClass != gen.AssetClassStock || *f.instruments[0].FetchKeyID != r.ID {
		t.Fatalf("instruments = %+v, want one stock from the fetch key", f.instruments)
	}
	inst := f.instruments[0].ID
	if len(f.listings) != 1 || f.listings[0].Currency != "GBP" || f.listings[0].InstrumentID != inst {
		t.Fatalf("listings = %+v, want one GBP listing on the instrument", f.listings)
	}
	listing := f.listings[0].ID
	var ids []string
	var viaID uuid.UUID
	for _, c := range f.identifiers {
		where := "instrument"
		if c.ListingID != nil {
			if *c.ListingID != listing {
				t.Errorf("identifier %s on listing %s, want %s", c.Value, *c.ListingID, listing)
			}
			where = "listing"
		}
		ids = append(ids, where+" "+name(types.Identifier{Type: c.Type, Domain: c.Domain, Value: c.Value}))
		if c.Type == types.IdentifierTypeIsin {
			viaID = c.ID
		}
		if *c.FetchKeyID != r.ID {
			t.Errorf("identifier %s from fetch key %s, want %s", c.Value, *c.FetchKeyID, r.ID)
		}
	}
	wantIDs := []string{"instrument isin GB00BH4HKS39", "instrument openfigi_share_class BBG001S5XDT5", "listing openfigi_composite BBG000C6K6G9", "listing mic_ticker XLON:VOD", "listing mic_ticker XETR:VODI"}
	if diff := cmp.Diff(wantIDs, ids); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	confirmed := gen.ValidityConfirmed
	wantAssoc := []gen.SetStatedKeyAssociationParams{{ID: k.ID, UserID: userID, InstrumentID: &inst, ListingID: &listing, ViaID: &viaID, Validity: &confirmed}}
	if diff := cmp.Diff(wantAssoc, f.associated); diff != "" {
		t.Errorf("association mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.UpsertIdentityCoverageParams{{InstrumentID: inst, Datasource: "a", FetchKeyID: r.ID}}, f.coverage); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.SetFetchKeyInstrumentParams{{ID: r.ID, UserID: userID, InstrumentID: &inst}}, f.attached); diff != "" {
		t.Errorf("fetch key mismatch (-want +got):\n%s", diff)
	}
	if len(f.asserted) != 5 {
		t.Errorf("%d fetch identifiers, want the 5 the group names", len(f.asserted))
	}
}

// TestResolveCovered checks that where a key is found in the database, it is
// requested only from the datasources that have not covered its instrument,
// and that their response fills what the instrument lacks without replacing
// it.
func TestResolveCovered(t *testing.T) {
	f := newFixture(t, entryA, entryB)
	instID, isinID, gbpID := db.NewID(), db.NewID(), db.NewID()
	f.stores(stored{
		instrument:  gen.Instrument{ID: instID, AssetClass: gen.AssetClassStock, FetchKeyID: ptr.To(db.NewID())},
		identifiers: []gen.Identifier{{ID: isinID, InstrumentID: instID, Type: types.IdentifierTypeIsin, Value: isin.Value}},
		listings:    []gen.Listing{{ID: gbpID, InstrumentID: instID, Currency: "GBP"}},
		covered:     []string{"a"},
	})
	k := keyOf(gen.AssetClassStock, "USD", isin)
	r := servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "USD", figi, isin, xnas))
	f.serves(entryB, r)

	got := f.resolve(k)
	if got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("Resolve = %+v, want matched", got)
	}
	if len(f.instruments) != 0 {
		t.Errorf("created %d instruments, want none: the key was found", len(f.instruments))
	}
	if len(f.listings) != 1 || f.listings[0].Currency != "USD" || *f.listings[0].FetchKeyID != r.ID {
		t.Errorf("listings = %+v, want the USD listing the instrument lacked, from b's fetch key", f.listings)
	}
	var ids []string
	for _, c := range f.identifiers {
		ids = append(ids, name(types.Identifier{Type: c.Type, Domain: c.Domain, Value: c.Value}))
	}
	if diff := cmp.Diff([]string{"openfigi_share_class BBG001S5XDT5", "mic_ticker XNAS:VOD"}, ids); diff != "" {
		t.Errorf("identifiers mismatch (-want +got):\n%s", diff)
	}
	confirmed := gen.ValidityConfirmed
	wantAssoc := []gen.SetStatedKeyAssociationParams{{ID: k.ID, UserID: userID, InstrumentID: &instID, ListingID: &f.listings[0].ID, ViaID: &isinID, Validity: &confirmed}}
	if diff := cmp.Diff(wantAssoc, f.associated); diff != "" {
		t.Errorf("association mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.UpsertIdentityCoverageParams{{InstrumentID: instID, Datasource: "b", FetchKeyID: r.ID}}, f.coverage); diff != "" {
		t.Errorf("coverage mismatch (-want +got):\n%s", diff)
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
			reason:  "a: 1 candidates, 1 not naming mic_ticker VOD",
		},
		{
			name:     "every group dropped",
			key:      keyOf(gen.AssetClassStock, "GBP", isin),
			results:  []result{servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "USD", figi, xnas))},
			fetched:  true,
			outcome:  gen.ResolutionOutcomeUnrecognised,
			reason:   "a: 1 candidates, 1 dropped",
			findings: []finding{{kind: gen.FindingKindDropped, step: ptr.To(gen.DropStepStated), detail: "listings in USD, none in the stated GBP", fetch: true}},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, entryA)
			f.notFound()
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
			if len(f.instruments)+len(f.associated) != 0 {
				t.Errorf("wrote %d instruments and %d associations, want none", len(f.instruments), len(f.associated))
			}
		})
	}
}

// TestResolveHeld checks the keys the lookup decides against what the
// database names: two stated identifiers naming different instruments, and
// a stated class disjoint from the instrument's.
func TestResolveHeld(t *testing.T) {
	t.Run("contradiction", func(t *testing.T) {
		f := newFixture(t, entryA)
		one, two := db.NewID(), db.NewID()
		f.store.EXPECT().FindIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.FindIdentifierParams) (gen.FindIdentifierRow, error) {
			inst := one
			if arg.Type == types.IdentifierTypeCusip {
				inst = two
			}
			return gen.FindIdentifierRow{Identifier: gen.Identifier{InstrumentID: inst, Type: arg.Type, Value: arg.Value}, Instrument: gen.Instrument{ID: inst, AssetClass: gen.AssetClassStock}}, nil
		}).Times(2)
		k := keyOf(gen.AssetClassStock, "USD", isin, cusip)
		got := f.resolve(k)
		reason := "isin GB00BH4HKS39 and cusip 92857W308 name different instruments"
		want := []gen.ResolutionKey{{RunID: res.ID, UserID: userID, StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised, Reason: &reason}}
		if diff := cmp.Diff(want, got); diff != "" {
			t.Errorf("Resolve mismatch (-want +got):\n%s", diff)
		}
		if len(f.findings) != 1 || f.findings[0].Kind != gen.FindingKindContradiction || f.findings[0].FetchKeyID != nil || f.findings[0].Step != nil || *f.findings[0].Detail != reason {
			t.Errorf("findings = %+v, want one contradiction with no fetch key and no step", f.findings)
		}
	})
	t.Run("disjoint class", func(t *testing.T) {
		f := newFixture(t, entryA)
		instID := db.NewID()
		f.stores(stored{
			instrument:  gen.Instrument{ID: instID, AssetClass: gen.AssetClassEtf},
			identifiers: []gen.Identifier{{ID: db.NewID(), InstrumentID: instID, Type: types.IdentifierTypeIsin, Value: isin.Value}},
		})
		k := keyOf(gen.AssetClassStock, "USD", isin)
		got := f.resolve(k)
		reason := "asset class stock contradicts the instrument's etf"
		if got[0].Outcome != gen.ResolutionOutcomeUnrecognised || got[0].Reason == nil || *got[0].Reason != reason {
			t.Errorf("Resolve = %+v, want unrecognised: %s", got, reason)
		}
	})
}

// TestResolveRetry checks that when a concurrent insert refuses a write, the
// write is retried, and the re-read then attaches to the instrument inserted.
func TestResolveRetry(t *testing.T) {
	f := newFixture(t, entryA)
	instID, isinID := db.NewID(), db.NewID()
	h := stored{
		instrument:  gen.Instrument{ID: instID, AssetClass: gen.AssetClassStock},
		identifiers: []gen.Identifier{{ID: isinID, InstrumentID: instID, Type: types.IdentifierTypeIsin, Value: isin.Value}},
	}
	f.store.EXPECT().FindIdentifier(gomock.Any(), gomock.Any()).Return(gen.FindIdentifierRow{}, db.ErrNotFound).AnyTimes()
	first := f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return(nil, nil)
	f.store.EXPECT().ListInstrumentsByIdentifiers(gomock.Any(), gomock.Any()).Return([]gen.ListInstrumentsByIdentifiersRow{{Identifier: h.identifiers[0], Instrument: h.instrument}}, nil).After(first)
	f.store.EXPECT().ListIdentifiers(gomock.Any(), instID).Return(h.identifiers, nil)
	f.store.EXPECT().ListListings(gomock.Any(), instID).Return(nil, nil)
	f.store.EXPECT().ListIdentityCoverage(gomock.Any(), gomock.Any()).Return(nil, nil)
	f.serves(entryA, servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "", figi, isin)))
	f.conflicts = 1

	got := f.resolve(keyOf(gen.AssetClassStock, "", isin))
	if got[0].Outcome != gen.ResolutionOutcomeMatched {
		t.Fatalf("Resolve = %+v, want matched", got)
	}
	if len(f.associated) != 2 || *f.associated[1].InstrumentID != instID || *f.associated[1].ViaID != isinID {
		t.Errorf("associated %+v, want the second attempt on the instrument found", f.associated)
	}
}

// TestResolveErrors checks that a store or fetch error fails the res.
func TestResolveErrors(t *testing.T) {
	boom := errors.New("boom")
	t.Run("lookup", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.store.EXPECT().FindIdentifier(gomock.Any(), gomock.Any()).Return(gen.FindIdentifierRow{}, boom)
		if _, err := f.resolver.Resolve(context.Background(), res, []gen.StatedKey{keyOf("", "", isin)}); !errors.Is(err, boom) {
			t.Errorf("Resolve() error = %v, want %v", err, boom)
		}
	})
	t.Run("fetch", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.notFound()
		f.fetcher.EXPECT().Identity(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(nil, boom)
		if _, err := f.resolver.Resolve(context.Background(), res, []gen.StatedKey{keyOf("", "", isin)}); !errors.Is(err, boom) {
			t.Errorf("Resolve() error = %v, want %v", err, boom)
		}
	})
}

// TestResolveMerge checks that when a response identifies two instruments,
// the merge folds the later created into the earlier, and that where two
// instruments disagree on an identifier, they are left apart with a
// contradiction finding.
func TestResolveMerge(t *testing.T) {
	earlier, later := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, time.September, 2, 0, 0, 0, 0, time.UTC)
	aID, bID, figiID, cusipID := db.NewID(), db.NewID(), db.NewID(), db.NewID()
	byFIGI := stored{
		instrument:  gen.Instrument{ID: aID, AssetClass: gen.AssetClassStock, CreatedAt: earlier, FetchKeyID: ptr.To(db.NewID())},
		identifiers: []gen.Identifier{{ID: figiID, InstrumentID: aID, Type: figi.Type, Value: figi.Value}},
	}
	gbp := gen.Listing{ID: db.NewID(), InstrumentID: bID, Currency: "GBP"}
	byCUSIP := stored{
		instrument:  gen.Instrument{ID: bID, AssetClass: gen.AssetClassStock, CreatedAt: later, FetchKeyID: ptr.To(db.NewID())},
		identifiers: []gen.Identifier{{ID: cusipID, InstrumentID: bID, Type: cusip.Type, Value: cusip.Value}},
		listings:    []gen.Listing{gbp},
	}
	t.Run("folded", func(t *testing.T) {
		f := newFixture(t, entryA)
		f.stores(byFIGI)
		f.stores(byCUSIP)
		r := servedResult(isin, []types.Identifier{isin}, cand(gen.AssetClassStock, "GBP", figi, isin, cusip, xlon))
		f.serves(entryA, r)
		got := f.resolve(keyOf(gen.AssetClassStock, "GBP", isin))
		if got[0].Outcome != gen.ResolutionOutcomeMatched {
			t.Fatalf("Resolve = %+v, want matched", got)
		}
		want := []string{"defer", "move listing " + gbp.ID.String(), "relink identifiers " + bID.String(), "relink stated keys " + bID.String(),
			"relink fetch keys " + bID.String(), "move coverage " + bID.String(), "delete coverage " + bID.String(), "delete listings " + bID.String(), "delete instrument " + bID.String()}
		if diff := cmp.Diff(want, f.merges); diff != "" {
			t.Errorf("merge steps mismatch (-want +got):\n%s", diff)
		}
		if len(f.findings) != 1 || f.findings[0].Kind != gen.FindingKindMerged || *f.findings[0].FetchKeyID != r.ID || *f.findings[0].Detail != fmt.Sprintf("folded instrument %s into %s, both identified: cusip 92857W308", bID, aID) {
			t.Errorf("findings = %+v, want one merged finding naming the fold", f.findings)
		}
		if len(f.associated) != 1 || *f.associated[0].InstrumentID != aID {
			t.Errorf("associated %+v, want the survivor", f.associated)
		}
		for _, c := range f.identifiers {
			if c.Type == cusip.Type {
				t.Errorf("created %s again after relinking it", c.Value)
			}
		}
	})
	t.Run("refused", func(t *testing.T) {
		f := newFixture(t, entryA)
		other := id(types.IdentifierTypeIsin, "", "GB00BH4HKS40")
		a := byFIGI
		a.identifiers = append([]gen.Identifier{{ID: db.NewID(), InstrumentID: aID, Type: isin.Type, Value: isin.Value}}, a.identifiers...)
		b := byCUSIP
		b.identifiers = append([]gen.Identifier{{ID: db.NewID(), InstrumentID: bID, Type: other.Type, Value: other.Value}}, b.identifiers...)
		f.stores(a)
		f.stores(b)
		r := servedResult(cusip, []types.Identifier{cusip}, cand(gen.AssetClassStock, "GBP", figi, cusip, xlon))
		f.serves(entryA, r)
		got := f.resolve(keyOf(gen.AssetClassStock, "GBP", cusip))
		if got[0].Outcome != gen.ResolutionOutcomeMatched {
			t.Fatalf("Resolve = %+v, want matched", got)
		}
		if len(f.merges) != 0 {
			t.Errorf("merge steps %v, want none", f.merges)
		}
		detail := fmt.Sprintf("isin GB00BH4HKS39 identifies instrument %s and isin GB00BH4HKS40 identifies %s; not merged", aID, bID)
		if len(f.findings) != 1 || f.findings[0].Kind != gen.FindingKindContradiction || *f.findings[0].Detail != detail {
			t.Errorf("findings = %+v, want one contradiction: %s", f.findings, detail)
		}
		if len(f.associated) != 1 || *f.associated[0].InstrumentID != bID || *f.associated[0].ViaID != cusipID {
			t.Errorf("associated %+v, want the instrument the stated CUSIP identifies", f.associated)
		}
		for _, c := range f.identifiers {
			if c.Type == figi.Type {
				t.Errorf("created %s, which the other instrument carries", c.Value)
			}
		}
	})
}
