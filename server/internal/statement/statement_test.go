package statement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgerrcode"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/mock/gomock"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/run"
)

var (
	userID = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	today  = time.Date(2026, time.April, 10, 0, 0, 0, 0, time.UTC)
	// clock is a moment on today in a zone where the date is already the
	// 11th, so a UTC today is what the check uses.
	clock = func() time.Time { return time.Date(2026, time.April, 11, 1, 0, 0, 0, time.FixedZone("east", 3*3600)) }
	from  = time.Date(2026, time.March, 1, 0, 0, 0, 0, time.UTC)
	until = time.Date(2026, time.April, 1, 0, 0, 0, 0, time.UTC)
)

func ident(t typev1.IdentifierType, value string, domain *string) *typev1.Identifier {
	return &typev1.Identifier{Type: t, Value: value, Domain: domain}
}

func cashKey(code string) *typev1.StatedKey {
	return &typev1.StatedKey{Identifiers: []*typev1.Identifier{ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, code, nil)}, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Currency: &code}
}

func securityKey(description string, class typev1.AssetClass, currency *string, ids ...*typev1.Identifier) *typev1.StatedKey {
	return &typev1.StatedKey{Identifiers: ids, AssetClass: class, Currency: currency, Description: &description}
}

func rowMsg(key *typev1.StatedKey, order, quantity string) *statementv1.Row {
	return &statementv1.Row{Key: key, OrderDate: order, SettlementDate: order, AsAt: order, Quantity: quantity}
}

type fixture struct {
	store   *MockStore
	runs    *MockRunner
	svc     *Service
	spec    run.Spec
	workErr error
}

// newFixture returns a service whose store runs a transaction inline over
// itself and whose runner runs Prepare and the work inline, so a case
// observes every write in order.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl)}
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error { return fn(f.store) }).AnyTimes()
	f.store.EXPECT().ListCurrencies(gomock.Any()).Return([]string{"EUR", "GBP", "USD"}, nil).AnyTimes()
	f.runs.EXPECT().Start(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, spec run.Spec, work run.Work) (gen.Run, error) {
		f.spec = spec
		row := gen.Run{ID: db.NewID(), UserID: spec.UserID, Kind: spec.Kind, Trigger: gen.RunTriggerUser, State: gen.RunStatePending}
		if spec.Prepare != nil {
			if err := spec.Prepare(ctx, row); err != nil {
				return row, err
			}
		}
		f.workErr = work(ctx, row)
		return row, nil
	}).AnyTimes()
	f.runs.EXPECT().Child(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(ctx context.Context, parent gen.Run, kind gen.RunKind, work run.Work) (gen.Run, error) {
		row := gen.Run{ID: db.NewID(), UserID: parent.UserID, Kind: kind, Trigger: gen.RunTriggerRun, ParentID: &parent.ID}
		return row, work(ctx, row)
	}).AnyTimes()
	f.svc = New(f.store, f.runs, clock)
	return f
}

func newIngestion(t *testing.T) (*fixture, *ingestion) {
	t.Helper()
	f := newFixture(t)
	return f, &ingestion{store: f.store, runs: f.runs, user: userID, broker: gen.BrokerIbkr, from: from, before: until, keys: map[uint64][]*key{}}
}

func TestCreateInvalid(t *testing.T) {
	usd := "USD"
	good := func() *statementv1.Statement {
		return &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01"}
	}
	tests := []struct {
		name string
		edit func(m *statementv1.Statement)
	}{
		{name: "unspecified broker", edit: func(m *statementv1.Statement) { m.Broker = typev1.Broker_BROKER_UNSPECIFIED }},
		{name: "undefined broker", edit: func(m *statementv1.Statement) { m.Broker = 99 }},
		{name: "malformed order_from", edit: func(m *statementv1.Statement) { m.OrderFrom = "2026-13-01" }},
		{name: "malformed order_before", edit: func(m *statementv1.Statement) { m.OrderBefore = "soon" }},
		{name: "empty period", edit: func(m *statementv1.Statement) { m.OrderBefore = m.OrderFrom }},
		{name: "split without a key", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{EffectiveDate: "2026-03-02", Quantity: "9"}}
		}},
		{name: "split with a bad date", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), EffectiveDate: "2026-03-40", Quantity: "9"}}
		}},
		{name: "split with a bad ratio", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), EffectiveDate: "2026-03-02", Quantity: "9", Ratio: &statementv1.SplitRatio{From: "1", To: "ten"}}}
		}},
		{name: "split with an inadmissible key", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", nil), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US2", nil)), EffectiveDate: "2026-03-02", Quantity: "9"}}
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			m := good()
			tc.edit(m)
			_, err := f.svc.Create(context.Background(), userID, m)
			if !errors.Is(err, ErrInvalid) {
				t.Errorf("Create(%s) error = %v, want ErrInvalid", tc.name, err)
			}
			if f.spec.UserID != uuid.Nil {
				t.Errorf("Create(%s) started a run", tc.name)
			}
		})
	}
}

func TestValidate(t *testing.T) {
	usd, xxx := "USD", "XXX"
	equity := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "ACME", nil))
	tests := []struct {
		name string
		row  *statementv1.Row
		want string
	}{
		{name: "accepted", row: rowMsg(equity, "2026-03-05", "10")},
		{name: "on the first day", row: rowMsg(equity, "2026-03-01", "10")},
		{name: "no key", row: &statementv1.Row{OrderDate: "2026-03-05"}, want: "no key"},
		{name: "class outside the vocabulary", row: rowMsg(securityKey("ACME", 99, &usd), "2026-03-05", "1"), want: "asset class 99 outside the vocabulary"},
		{name: "type outside the vocabulary", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(99, "x", nil)), "2026-03-05", "1"), want: "identifier type 99 outside the vocabulary"},
		{name: "unspecified type", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED, "x", nil)), "2026-03-05", "1"), want: "identifier type 0 outside the vocabulary"},
		{name: "cash naming two currencies", row: rowMsg(&typev1.StatedKey{Identifiers: append(cashKey("USD").Identifiers, cashKey("EUR").Identifiers...), AssetClass: typev1.AssetClass_ASSET_CLASS_CASH}, "2026-03-05", "1"), want: "two currency identifiers, USD and EUR"},
		{name: "two isins", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", nil), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US2", nil)), "2026-03-05", "1"), want: "two isin identifiers, US1 and US2"},
		{name: "two tickers at one venue", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "A", ptr.To("XNYS")), ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "B", ptr.To("XNYS"))), "2026-03-05", "1"), want: "two mic_ticker identifiers, A and B"},
		{name: "two tickers at two venues", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "A", ptr.To("XNYS")), ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "A", ptr.To("XLON"))), "2026-03-05", "1")},
		{name: "no admissible identifier", row: rowMsg(&typev1.StatedKey{AssetClass: typev1.AssetClass_ASSET_CLASS_EQUITY, Currency: &usd}, "2026-03-05", "1"), want: "no admissible identifier"},
		{name: "empty description", row: rowMsg(securityKey("", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), "2026-03-05", "1"), want: "no admissible identifier"},
		{name: "malformed order date", row: &statementv1.Row{Key: equity, OrderDate: "2026-03-40", SettlementDate: "2026-03-05", AsAt: "2026-03-05", Quantity: "1"}, want: `malformed order date "2026-03-40"`},
		{name: "malformed settlement date", row: &statementv1.Row{Key: equity, OrderDate: "2026-03-05", SettlementDate: "", AsAt: "2026-03-05", Quantity: "1"}, want: `malformed settlement date ""`},
		{name: "malformed as at", row: &statementv1.Row{Key: equity, OrderDate: "2026-03-05", SettlementDate: "2026-03-05", AsAt: "5 March", Quantity: "1"}, want: `malformed as at "5 March"`},
		{name: "malformed quantity", row: rowMsg(equity, "2026-03-05", "ten"), want: `malformed quantity "ten"`},
		{name: "before the period", row: rowMsg(equity, "2026-02-28", "1"), want: "order date 2026-02-28 outside the claimed period"},
		{name: "on the day after the period", row: rowMsg(equity, "2026-05-01", "1"), want: "order date 2026-05-01 outside the claimed period"},
		{name: "after today", row: rowMsg(equity, "2026-04-20", "1"), want: "order date 2026-04-20 after today"},
		{name: "unknown currency", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &xxx), "2026-03-05", "1"), want: `unknown currency "XXX"`},
		{name: "a currency identifier stating no currency", row: rowMsg(&typev1.StatedKey{Identifiers: cashKey("USD").Identifiers, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH}, "2026-03-05", "1")},
		{name: "a currency identifier on an equity key", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, "USD", nil)), "2026-03-05", "1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, g := newIngestion(t)
			// The period runs past today so the after-today check is reached.
			g.before = time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
			got := g.validate(3, tc.row, today, map[string]bool{"USD": true, "EUR": true})
			if got.reason != tc.want {
				t.Errorf("validate(%s) reason = %q, want %q", tc.name, got.reason, tc.want)
			}
			if (got.key == nil) != (tc.want != "") || got.ordinal != 3 {
				t.Errorf("validate(%s) = %+v, want a key only when accepted", tc.name, got)
			}
			if tc.want != "" && len(g.keys) != 0 {
				t.Errorf("validate(%s) interned a key for a rejected row", tc.name)
			}
		})
	}
}

// TestKeyForm checks which stated keys are one key.
func TestKeyForm(t *testing.T) {
	usd := "USD"
	mic := func(v string, domain *string) *typev1.Identifier {
		return ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, v, domain)
	}
	tests := []struct {
		name string
		a, b *typev1.StatedKey
		same bool
	}{
		{name: "identifier order", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", nil), mic("Y", ptr.To("XNYS"))), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("Y", ptr.To("XNYS")), mic("X", nil)), same: true},
		{name: "domain", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", nil)), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", ptr.To("XNYS")))},
		{name: "currency", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, nil)},
		{name: "class", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_UNSPECIFIED, &usd)},
		{name: "description", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), b: securityKey("B", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd)},
		{name: "empty description", a: cashKey("USD"), b: func() *typev1.StatedKey { k := cashKey("USD"); k.Description = ptr.To(""); return k }(), same: true},
		{name: "empty description and none", a: &typev1.StatedKey{Identifiers: cashKey("USD").Identifiers, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Description: ptr.To("")}, b: cashKey("USD")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, g := newIngestion(t)
			a := g.validate(0, rowMsg(tc.a, "2026-03-05", "1"), today, map[string]bool{"USD": true})
			b := g.validate(1, rowMsg(tc.b, "2026-03-05", "1"), today, map[string]bool{"USD": true})
			if a.reason != "" || b.reason != "" {
				t.Fatalf("validate rejected %q / %q", a.reason, b.reason)
			}
			if (a.key == b.key) != tc.same {
				t.Errorf("keys %s: same = %v, want %v", tc.name, a.key == b.key, tc.same)
			}
		})
	}
	_, g := newIngestion(t)
	got := g.validate(0, rowMsg(securityKey("A", typev1.AssetClass_ASSET_CLASS_STOCK, &usd, mic("Y", ptr.To("XNYS")), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", nil), mic("X", nil)), "2026-03-05", "1"), today, map[string]bool{"USD": true})
	want := []types.StatedIdentifier{{Type: "isin", Value: "US1"}, {Type: "mic_ticker", Value: "X"}, {Type: "mic_ticker", Domain: ptr.To("XNYS"), Value: "Y"}}
	if diff := cmp.Diff(want, got.key.identifiers); diff != "" {
		t.Errorf("identifiers order mismatch (-want +got):\n%s", diff)
	}
	if got.key.class == nil || *got.key.class != gen.AssetClassStock {
		t.Errorf("class = %v, want stock", got.key.class)
	}
}

// TestOrderedKeys checks that keys stating a currency come first, and that
// first appearance orders the rest.
func TestOrderedKeys(t *testing.T) {
	usd := "USD"
	_, g := newIngestion(t)
	for i, k := range []*typev1.StatedKey{
		securityKey("C", typev1.AssetClass_ASSET_CLASS_EQUITY, nil),
		securityKey("B", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd),
		securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, nil),
		securityKey("D", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd),
		securityKey("B", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd),
	} {
		if r := g.validate(int32(i), rowMsg(k, "2026-03-05", "1"), today, map[string]bool{"USD": true}); r.reason != "" {
			t.Fatalf("validate(%d) rejected: %s", i, r.reason)
		}
	}
	var got []string
	for _, k := range g.orderedKeys() {
		got = append(got, *k.description)
	}
	if diff := cmp.Diff([]string{"B", "D", "C", "A"}, got); diff != "" {
		t.Errorf("orderedKeys mismatch (-want +got):\n%s", diff)
	}
}

func listing(class gen.AssetClass, currency string, owner *uuid.UUID) gen.GetListingByIdentifierRow {
	return gen.GetListingByIdentifierRow{Listing: gen.Listing{ID: db.NewID(), InstrumentID: db.NewID(), Currency: currency, OwnerID: owner}, AssetClass: class}
}

func conflict() error {
	return &pgconn.PgError{Code: pgerrcode.UniqueViolation}
}

func TestResolveKey(t *testing.T) {
	usd, eur := "USD", "EUR"
	dom := "ibkr/upload"
	byDescription := gen.GetListingByIdentifierParams{OwnerID: &userID, Type: gen.IdentifierTypeBrokerDescription, Domain: &dom, Value: "ACME"}
	byCurrency := func(code string) gen.GetInstrumentByIdentifierParams {
		return gen.GetInstrumentByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: code}
	}
	cashInst := gen.Instrument{ID: db.NewID(), AssetClass: gen.AssetClassCash}
	cashLine := func(f *fixture, currency string, err error) gen.GetListingByIdentifierRow {
		l := gen.Listing{ID: db.NewID(), InstrumentID: cashInst.ID, Currency: currency}
		f.store.EXPECT().GetInstrumentByIdentifier(gomock.Any(), byCurrency("USD")).Return(cashInst, nil)
		f.store.EXPECT().GetListing(gomock.Any(), gen.GetListingParams{InstrumentID: cashInst.ID, Currency: currency}).Return(l, err)
		return gen.GetListingByIdentifierRow{Listing: l, AssetClass: cashInst.AssetClass}
	}
	tests := []struct {
		name      string
		key       *typev1.StatedKey
		expect    func(f *fixture) gen.GetListingByIdentifierRow
		outcome   gen.ResolutionOutcome
		reason    string
		created   *gen.CreateInstrumentParams
		noListing bool
	}{
		{name: "cash", key: cashKey("USD"), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			return cashLine(f, "USD", nil)
		}, outcome: gen.ResolutionOutcomeMatched},
		{name: "cash in a currency it has no line in", key: &typev1.StatedKey{Identifiers: cashKey("USD").Identifiers, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Currency: &eur}, expect: func(f *fixture) gen.GetListingByIdentifierRow {
			cashLine(f, "EUR", db.ErrNotFound)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeRejected, reason: "no listing of USD in EUR"},
		{name: "currency of no instrument", key: cashKey("XXX"), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetInstrumentByIdentifier(gomock.Any(), byCurrency("XXX")).Return(gen.Instrument{}, db.ErrNotFound)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeRejected, reason: "no currency XXX"},
		{name: "currency identifier on an equity key", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, "USD", nil)), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetInstrumentByIdentifier(gomock.Any(), byCurrency("USD")).Return(cashInst, nil)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeRejected, reason: "asset class equity contradicts the instrument's cash"},
		{name: "currency identifier stating no currency", key: &typev1.StatedKey{Identifiers: cashKey("USD").Identifiers, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH}, expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetInstrumentByIdentifier(gomock.Any(), byCurrency("USD")).Return(cashInst, nil)
			return gen.GetListingByIdentifierRow{Listing: gen.Listing{InstrumentID: cashInst.ID}}
		}, outcome: gen.ResolutionOutcomeMatched, noListing: true},
		{name: "description names a listing", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_STOCK, &usd), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			l := listing(gen.AssetClassEquity, "USD", &userID)
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(l, nil)
			return l
		}, outcome: gen.ResolutionOutcomeMatched},
		{name: "description names a listing of a wider class", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_SECURITY, &usd), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			l := listing(gen.AssetClassEquity, "USD", &userID)
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(l, nil)
			return l
		}, outcome: gen.ResolutionOutcomeMatched},
		{name: "no currency names a listing", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, nil), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			l := listing(gen.AssetClassEquity, "USD", &userID)
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(l, nil)
			return l
		}, outcome: gen.ResolutionOutcomeMatched},
		{name: "currency contradicts the listing", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &eur), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(listing(gen.AssetClassEquity, "USD", &userID), nil)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeRejected, reason: "currency EUR contradicts the listing named, quoted in USD"},
		{name: "class contradicts the listing", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_OPTION, &usd), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(listing(gen.AssetClassEquity, "USD", &userID), nil)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeRejected, reason: "asset class option contradicts the listing's equity"},
		{name: "no currency and nothing named", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, nil), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(gen.GetListingByIdentifierRow{}, db.ErrNotFound)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeRejected, reason: "no currency"},
		{name: "created", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(gen.GetListingByIdentifierRow{}, db.ErrNotFound)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeCreated, created: &gen.CreateInstrumentParams{AssetClass: gen.AssetClassEquity, OwnerID: &userID}},
		{name: "created from an unstated class", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_UNSPECIFIED, &usd), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(gen.GetListingByIdentifierRow{}, db.ErrNotFound)
			return gen.GetListingByIdentifierRow{}
		}, outcome: gen.ResolutionOutcomeCreated, created: &gen.CreateInstrumentParams{AssetClass: gen.AssetClassUnknown, OwnerID: &userID}},
		{name: "creation lost a race", key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), expect: func(f *fixture) gen.GetListingByIdentifierRow {
			l := listing(gen.AssetClassEquity, "USD", &userID)
			gomock.InOrder(
				f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(gen.GetListingByIdentifierRow{}, db.ErrNotFound),
				f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).Return(gen.Instrument{}, nil),
				f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).Return(gen.Listing{}, nil),
				f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).Return(gen.Identifier{}, conflict()),
				f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(l, nil),
			)
			return l
		}, outcome: gen.ResolutionOutcomeMatched},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f, g := newIngestion(t)
			var want gen.GetListingByIdentifierRow
			if tc.expect != nil {
				want = tc.expect(f)
			}
			var made gen.Listing
			if tc.created != nil {
				f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateInstrumentParams) (gen.Instrument, error) {
					if diff := cmp.Diff(*tc.created, arg, cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".ID" }, cmp.Ignore())); diff != "" {
						t.Errorf("CreateInstrument mismatch (-want +got):\n%s", diff)
					}
					return gen.Instrument{ID: arg.ID, AssetClass: arg.AssetClass, OwnerID: arg.OwnerID}, nil
				})
				f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateListingParams) (gen.Listing, error) {
					made = gen.Listing{ID: arg.ID, InstrumentID: arg.InstrumentID, Currency: arg.Currency, OwnerID: arg.OwnerID}
					if arg.Currency != "USD" || arg.OwnerID == nil || *arg.OwnerID != userID {
						t.Errorf("CreateListing(%+v), want USD owned by the user", arg)
					}
					return made, nil
				})
				f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateIdentifierParams) (gen.Identifier, error) {
					if arg.Type != gen.IdentifierTypeBrokerDescription || arg.Domain == nil || *arg.Domain != dom || arg.Value != "ACME" || arg.ListingID == nil || *arg.ListingID != made.ID || arg.OwnerID == nil || *arg.OwnerID != userID {
						t.Errorf("CreateIdentifier(%+v), want the broker description on the listing, owned by the user", arg)
					}
					return gen.Identifier{}, nil
				})
			}
			k, err := keyOf(tc.key)
			if err != nil {
				t.Fatalf("keyOf() error = %v", err)
			}
			if err := g.resolveKey(context.Background(), k); err != nil {
				t.Fatalf("resolveKey() error = %v", err)
			}
			if k.outcome != tc.outcome || k.reason != tc.reason {
				t.Errorf("resolveKey(%s) = %s %q, want %s %q", tc.name, k.outcome, k.reason, tc.outcome, tc.reason)
			}
			if tc.created != nil {
				want.Listing = made
			}
			switch {
			case tc.outcome == gen.ResolutionOutcomeRejected:
			case tc.noListing:
				if k.listing != nil || k.instrument == nil || *k.instrument != want.Listing.InstrumentID {
					t.Errorf("resolveKey(%s) named listing %v of %v, want no listing of %s", tc.name, k.listing, k.instrument, want.Listing.InstrumentID)
				}
			case k.listing == nil || *k.listing != want.Listing.ID || *k.instrument != want.Listing.InstrumentID:
				t.Errorf("resolveKey(%s) named listing %v of %v, want %s of %s", tc.name, k.listing, k.instrument, want.Listing.ID, want.Listing.InstrumentID)
			}
		})
	}
}

// TestResolveKeyError checks that a store failure fails the resolution
// rather than rejecting the key.
func TestResolveKeyError(t *testing.T) {
	f, g := newIngestion(t)
	boom := errors.New("boom")
	f.store.EXPECT().GetInstrumentByIdentifier(gomock.Any(), gomock.Any()).Return(gen.Instrument{}, boom)
	k, err := keyOf(cashKey("USD"))
	if err != nil {
		t.Fatal(err)
	}
	if err := g.resolveKey(context.Background(), k); !errors.Is(err, boom) {
		t.Errorf("resolveKey() error = %v, want %v", err, boom)
	}
}

// TestCreate runs one statement through receipt, resolution and the write
// against the mocks, and checks what each transaction wrote.
func TestCreate(t *testing.T) {
	f := newFixture(t)
	usd, eur := "USD", "EUR"
	acme := securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0000000001", nil))
	msg := &statementv1.Statement{
		Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{
			rowMsg(acme, "2026-03-05", "10"),
			rowMsg(cashKey("USD"), "2026-03-05", "-1000"),
			rowMsg(acme, "2026-04-20", "1"),
			rowMsg(securityKey("ACME CORP", typev1.AssetClass_ASSET_CLASS_EQUITY, &eur), "2026-03-06", "1"),
			rowMsg(acme, "2026-03-07", "5"),
		},
		Splits: []*statementv1.StatedSplit{{Key: acme, EffectiveDate: "2026-03-10", Quantity: "9", Ratio: &statementv1.SplitRatio{From: "1", To: "10"}}},
	}
	dom := "ibkr/upload"
	cashInst := gen.Instrument{ID: db.NewID(), AssetClass: gen.AssetClassCash}
	cash := gen.GetListingByIdentifierRow{Listing: gen.Listing{ID: db.NewID(), InstrumentID: cashInst.ID, Currency: "USD"}, AssetClass: gen.AssetClassCash}
	created := gen.Listing{ID: db.NewID(), InstrumentID: db.NewID(), Currency: "USD", OwnerID: &userID}

	var statements []gen.CreateStatementParams
	var keys []gen.CreateStatedKeyParams
	var splits []gen.CreateStatementSplitParams
	var resolved []gen.CreateResolutionKeyParams
	var deleted []gen.DeleteTransactionsParams
	var txs []gen.CreateTransactionParams
	var items []gen.CreateStatementItemParams
	var completed []uuid.UUID
	f.store.EXPECT().CreateStatement(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStatementParams) (gen.Statement, error) {
		statements = append(statements, arg)
		return gen.Statement{}, nil
	})
	f.store.EXPECT().CreateStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStatedKeyParams) (gen.StatedKey, error) {
		keys = append(keys, arg)
		return gen.StatedKey{}, nil
	}).Times(3)
	f.store.EXPECT().CreateStatementSplit(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStatementSplitParams) error {
		splits = append(splits, arg)
		return nil
	})
	f.store.EXPECT().GetInstrumentByIdentifier(gomock.Any(), gen.GetInstrumentByIdentifierParams{Type: gen.IdentifierTypeCurrency, Value: "USD"}).Return(cashInst, nil)
	f.store.EXPECT().GetListing(gomock.Any(), gen.GetListingParams{InstrumentID: cashInst.ID, Currency: "USD"}).Return(cash.Listing, nil)
	byDescription := gen.GetListingByIdentifierParams{OwnerID: &userID, Type: gen.IdentifierTypeBrokerDescription, Domain: &dom, Value: "ACME CORP"}
	gomock.InOrder(
		f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(gen.GetListingByIdentifierRow{}, db.ErrNotFound),
		f.store.EXPECT().CreateInstrument(gomock.Any(), gomock.Any()).Return(gen.Instrument{ID: created.InstrumentID}, nil),
		f.store.EXPECT().CreateListing(gomock.Any(), gomock.Any()).Return(created, nil),
		f.store.EXPECT().CreateIdentifier(gomock.Any(), gomock.Any()).Return(gen.Identifier{}, nil),
		f.store.EXPECT().GetListingByIdentifier(gomock.Any(), byDescription).Return(gen.GetListingByIdentifierRow{Listing: created, AssetClass: gen.AssetClassEquity}, nil),
	)
	f.store.EXPECT().CreateResolutionKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateResolutionKeyParams) error {
		resolved = append(resolved, arg)
		return nil
	}).Times(3)
	f.store.EXPECT().DeleteTransactions(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.DeleteTransactionsParams) (int64, error) {
		deleted = append(deleted, arg)
		return 0, nil
	})
	f.store.EXPECT().CreateTransaction(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateTransactionParams) (gen.Transaction, error) {
		txs = append(txs, arg)
		return gen.Transaction{}, nil
	}).Times(3)
	f.store.EXPECT().CreateStatementItem(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStatementItemParams) error {
		items = append(items, arg)
		return nil
	}).Times(2)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id uuid.UUID) error {
		completed = append(completed, id)
		return nil
	})

	row, err := f.svc.Create(context.Background(), userID, msg)
	if err != nil || f.workErr != nil {
		t.Fatalf("Create() error = %v, work error = %v", err, f.workErr)
	}
	if f.spec.Kind != gen.RunKindStatement || f.spec.Lane != "ibkr" || f.spec.UserID != userID {
		t.Errorf("Start(%+v), want a statement run of the user in lane ibkr", f.spec)
	}
	wantStatement := []gen.CreateStatementParams{{ID: row.ID, UserID: userID, Broker: gen.BrokerIbkr, OrderFrom: from, OrderBefore: until, RowCount: 5}}
	if diff := cmp.Diff(wantStatement, statements); diff != "" {
		t.Errorf("CreateStatement mismatch (-want +got):\n%s", diff)
	}
	if len(keys) != 3 || len(splits) != 1 || splits[0].StatedKeyID != keys[0].ID && splits[0].StatedKeyID != keys[1].ID && splits[0].StatedKeyID != keys[2].ID {
		t.Errorf("Prepare wrote %d keys and %d splits, want 3 keys and 1 split of one of them", len(keys), len(splits))
	}
	for _, k := range keys {
		if k.StatementID != row.ID || k.UserID != userID {
			t.Errorf("CreateStatedKey(%+v), want the statement's and the user's", k)
		}
	}
	outcomes := map[gen.ResolutionOutcome]int{}
	for _, r := range resolved {
		outcomes[r.Outcome]++
	}
	if diff := cmp.Diff(map[gen.ResolutionOutcome]int{gen.ResolutionOutcomeMatched: 1, gen.ResolutionOutcomeCreated: 1, gen.ResolutionOutcomeRejected: 1}, outcomes); diff != "" {
		t.Errorf("resolution outcomes mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]gen.DeleteTransactionsParams{{UserID: userID, Broker: gen.BrokerIbkr, OrderFrom: from, OrderBefore: until}}, deleted); diff != "" {
		t.Errorf("DeleteTransactions mismatch (-want +got):\n%s", diff)
	}
	var gotTxs []string
	for _, tx := range txs {
		gotTxs = append(gotTxs, tx.OrderDate.Format(time.DateOnly)+" "+tx.Quantity.String()+" "+tx.InstrumentID.String())
		if tx.StatementID != row.ID || tx.UserID != userID || tx.Broker != gen.BrokerIbkr || tx.ListingID == nil {
			t.Errorf("CreateTransaction(%+v), want the statement's, the user's and the broker's with a listing", tx)
		}
	}
	wantTxs := []string{"2026-03-05 10 " + created.InstrumentID.String(), "2026-03-05 -1000 " + cash.Listing.InstrumentID.String(), "2026-03-07 5 " + created.InstrumentID.String()}
	if diff := cmp.Diff(wantTxs, gotTxs); diff != "" {
		t.Errorf("transactions mismatch (-want +got):\n%s", diff)
	}
	var gotItems []string
	for _, it := range items {
		gotItems = append(gotItems, string(rune('0'+it.Ordinal))+" "+it.Reason)
		if it.StatementID != row.ID || len(it.Stated) == 0 {
			t.Errorf("CreateStatementItem(%+v), want the statement's with the row", it)
		}
	}
	wantItems := []string{"2 order date 2026-04-20 outside the claimed period", "3 currency EUR contradicts the listing named, quoted in USD"}
	if diff := cmp.Diff(wantItems, gotItems); diff != "" {
		t.Errorf("items mismatch (-want +got):\n%s", diff)
	}
	if diff := cmp.Diff([]uuid.UUID{row.ID}, completed); diff != "" {
		t.Errorf("CompleteRun mismatch (-want +got):\n%s", diff)
	}
}

// TestCreateFailures checks that a failing Prepare is returned by Create,
// and that a failing resolution fails the work before anything is written.
func TestCreateFailures(t *testing.T) {
	usd := "USD"
	msg := func() *statementv1.Statement {
		return &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
			Rows: []*statementv1.Row{rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_SECURITY, &usd), "2026-03-05", "1")}}
	}
	boom := errors.New("boom")
	t.Run("prepare", func(t *testing.T) {
		f := newFixture(t)
		f.store.EXPECT().CreateStatement(gomock.Any(), gomock.Any()).Return(gen.Statement{}, boom)
		if _, err := f.svc.Create(context.Background(), userID, msg()); !errors.Is(err, boom) {
			t.Errorf("Create() error = %v, want %v", err, boom)
		}
	})
	t.Run("resolution", func(t *testing.T) {
		f := newFixture(t)
		f.store.EXPECT().CreateStatement(gomock.Any(), gomock.Any()).Return(gen.Statement{}, nil)
		f.store.EXPECT().CreateStatedKey(gomock.Any(), gomock.Any()).Return(gen.StatedKey{}, nil)
		f.store.EXPECT().GetListingByIdentifier(gomock.Any(), gomock.Any()).Return(gen.GetListingByIdentifierRow{}, boom)
		if _, err := f.svc.Create(context.Background(), userID, msg()); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if !errors.Is(f.workErr, boom) {
			t.Errorf("work error = %v, want %v", f.workErr, boom)
		}
	})
}

func TestDisjoint(t *testing.T) {
	tests := []struct {
		a, b gen.AssetClass
		want bool
	}{
		{gen.AssetClassStock, gen.AssetClassEquity, false},
		{gen.AssetClassEquity, gen.AssetClassStock, false},
		{gen.AssetClassUnknown, gen.AssetClassOption, false},
		{gen.AssetClassStock, gen.AssetClassStock, false},
		{gen.AssetClassStock, gen.AssetClassEtf, true},
		{gen.AssetClassOption, gen.AssetClassEquity, true},
		{gen.AssetClassCash, gen.AssetClassSecurity, true},
	}
	for _, tc := range tests {
		if got := disjoint(tc.a, tc.b); got != tc.want {
			t.Errorf("disjoint(%s, %s) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
