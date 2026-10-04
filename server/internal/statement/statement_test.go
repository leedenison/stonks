package statement

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
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

// currencies is the vocabulary the fixture's store lists, with GBX in the GBP
// family.
var currencies = []gen.Currency{{Code: "EUR", Family: "EUR"}, {Code: "GBP", Family: "GBP"}, {Code: "GBX", Family: "GBP"}, {Code: "USD", Family: "USD"}}

func families() map[string]string {
	out := map[string]string{}
	for _, c := range currencies {
		out[c.Code] = c.Family
	}
	return out
}

func ident(t typev1.IdentifierType, value, domain string) *typev1.Identifier {
	return &typev1.Identifier{Type: t, Value: value, Domain: domain}
}

func cashKey(code string) *typev1.StatedKey {
	return &typev1.StatedKey{Identifiers: []*typev1.Identifier{ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, code, "")}, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Currency: &code}
}

// securityKey builds a stated key as an IBKR export states it.
func securityKey(description string, class typev1.AssetClass, currency *string, ids ...*typev1.Identifier) *typev1.StatedKey {
	if description != "" {
		ids = append(ids, ident(typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, description, "ibkr"))
	}
	return &typev1.StatedKey{Identifiers: ids, AssetClass: class, Currency: currency}
}

func described(k gen.StatedKey, description string) bool {
	for _, id := range k.Identifiers {
		if id.Type == types.IdentifierTypeBrokerDescription && id.Value == description {
			return true
		}
	}
	return false
}

func rowMsg(key *typev1.StatedKey, order, quantity string) *statementv1.Row {
	return &statementv1.Row{Key: key, OrderDate: order, SettlementDate: order, AsAt: order, Quantity: quantity}
}

type fixture struct {
	store    *MockStore
	runs     *MockRunner
	resolver *MockResolver
	svc      *Service
	spec     run.Spec
	workErr  error
	// outcomes is what the resolver answers for the keys, every key
	// unrecognised by default, and resolveErr is its error.
	outcomes   func(keys []gen.StatedKey) []gen.ResolutionKey
	resolveErr error
}

// newFixture returns a service whose store runs a transaction inline over
// itself and whose runner runs Prepare and the work inline, so a case
// observes every write in order.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl), resolver: NewMockResolver(ctrl)}
	f.outcomes = func(keys []gen.StatedKey) []gen.ResolutionKey {
		out := make([]gen.ResolutionKey, len(keys))
		for i, k := range keys {
			out[i] = gen.ResolutionKey{StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised}
		}
		return out
	}
	f.resolver.EXPECT().Resolve(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ gen.Run, keys []gen.StatedKey) ([]gen.ResolutionKey, error) {
		if f.resolveErr != nil {
			return nil, f.resolveErr
		}
		return f.outcomes(keys), nil
	}).AnyTimes()
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error { return fn(f.store) }).AnyTimes()
	f.store.EXPECT().ListCurrencies(gomock.Any()).Return(currencies, nil).AnyTimes()
	f.store.EXPECT().LockUserKeys(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.store.EXPECT().ListGroupableKeys(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	f.store.EXPECT().ClearStatedKeyGroups(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
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
	f.svc = New(f.store, f.runs, f.resolver, clock)
	return f
}

func newIngestion(t *testing.T) (*fixture, *ingestion) {
	t.Helper()
	f := newFixture(t)
	return f, &ingestion{store: f.store, runs: f.runs, resolver: f.resolver, user: userID, broker: gen.BrokerIbkr, from: from, before: until, families: families(), keys: map[uint64][]*key{}}
}

func TestCreateInvalid(t *testing.T) {
	usd, xxx := "USD", "XXX"
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
		{name: "split with an unknown currency", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &xxx), EffectiveDate: "2026-03-02", Quantity: "9"}}
		}},
		{name: "split with an inadmissible key", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", ""), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US2", "")), EffectiveDate: "2026-03-02", Quantity: "9"}}
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
	equity := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "ACME", ""))
	tests := []struct {
		name string
		row  *statementv1.Row
		want string
	}{
		{name: "accepted", row: rowMsg(equity, "2026-03-05", "10")},
		{name: "on the first day", row: rowMsg(equity, "2026-03-01", "10")},
		{name: "no key", row: &statementv1.Row{OrderDate: "2026-03-05"}, want: "no key"},
		{name: "class outside the vocabulary", row: rowMsg(securityKey("ACME", 99, &usd), "2026-03-05", "1"), want: "asset class 99 outside the vocabulary"},
		{name: "type outside the vocabulary", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(99, "x", "")), "2026-03-05", "1"), want: "identifier type 99 outside the vocabulary"},
		{name: "unspecified type", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_UNSPECIFIED, "x", "")), "2026-03-05", "1"), want: "identifier type 0 outside the vocabulary"},
		{name: "cash naming two currencies", row: rowMsg(&typev1.StatedKey{Identifiers: append(cashKey("USD").Identifiers, cashKey("EUR").Identifiers...), AssetClass: typev1.AssetClass_ASSET_CLASS_CASH}, "2026-03-05", "1"), want: "two currency identifiers, USD and EUR"},
		{name: "two isins", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", ""), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US2", "")), "2026-03-05", "1"), want: "two isin identifiers, US1 and US2"},
		{name: "two tickers at one venue", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "A", "XNYS"), ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "B", "XNYS")), "2026-03-05", "1"), want: "two mic_ticker identifiers, A and B"},
		{name: "two tickers at two venues", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "A", "XNYS"), ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "A", "XLON")), "2026-03-05", "1")},
		{name: "two descriptions", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, "ACME CORP", "ibkr")), "2026-03-05", "1")},
		{name: "nothing stated about the instrument", row: rowMsg(&typev1.StatedKey{AssetClass: typev1.AssetClass_ASSET_CLASS_EQUITY, Currency: &usd}, "2026-03-05", "1"), want: "no identifier"},
		{name: "an identifier and no description", row: rowMsg(securityKey("", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331005", "")), "2026-03-05", "1")},
		{name: "malformed order date", row: &statementv1.Row{Key: equity, OrderDate: "2026-03-40", SettlementDate: "2026-03-05", AsAt: "2026-03-05", Quantity: "1"}, want: `malformed order date "2026-03-40"`},
		{name: "malformed settlement date", row: &statementv1.Row{Key: equity, OrderDate: "2026-03-05", SettlementDate: "", AsAt: "2026-03-05", Quantity: "1"}, want: `malformed settlement date ""`},
		{name: "malformed as at", row: &statementv1.Row{Key: equity, OrderDate: "2026-03-05", SettlementDate: "2026-03-05", AsAt: "5 March", Quantity: "1"}, want: `malformed as at "5 March"`},
		{name: "malformed quantity", row: rowMsg(equity, "2026-03-05", "ten"), want: `malformed quantity "ten"`},
		{name: "before the period", row: rowMsg(equity, "2026-02-28", "1"), want: "order date outside the claimed period"},
		{name: "on the day after the period", row: rowMsg(equity, "2026-05-01", "1"), want: "order date outside the claimed period"},
		{name: "after today", row: rowMsg(equity, "2026-04-20", "1"), want: "order date after today"},
		{name: "unknown currency", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &xxx), "2026-03-05", "1"), want: `unknown currency "XXX"`},
		{name: "a currency identifier stating no currency", row: rowMsg(&typev1.StatedKey{Identifiers: cashKey("USD").Identifiers, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH}, "2026-03-05", "1")},
		{name: "a currency identifier on an equity key", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, "USD", "")), "2026-03-05", "1")},
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
	mic := func(v, domain string) *typev1.Identifier {
		return ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, v, domain)
	}
	tests := []struct {
		name string
		a, b *typev1.StatedKey
		same bool
	}{
		{name: "identifier order", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", ""), mic("Y", "XNYS")), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("Y", "XNYS"), mic("X", "")), same: true},
		{name: "domain", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", "")), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", "XNYS"))},
		{name: "currency", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, nil)},
		{name: "class", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_UNSPECIFIED, &usd)},
		{name: "description", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), b: securityKey("B", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd)},
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
	got := g.validate(0, rowMsg(securityKey("A", typev1.AssetClass_ASSET_CLASS_STOCK, &usd, mic("Y", "XNYS"), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", ""), mic("X", "")), "2026-03-05", "1"), today, map[string]bool{"USD": true})
	want := []types.Identifier{{Type: "broker_description", Domain: "ibkr", Value: "A"}, {Type: "isin", Value: "US1"}, {Type: "mic_ticker", Value: "X"}, {Type: "mic_ticker", Domain: "XNYS", Value: "Y"}}
	if diff := cmp.Diff(want, got.key.identifiers); diff != "" {
		t.Errorf("identifiers order mismatch (-want +got):\n%s", diff)
	}
	if got.key.class == nil || *got.key.class != gen.AssetClassStock {
		t.Errorf("class = %v, want stock", got.key.class)
	}
}

// TestOutcomes checks that a rejected key rejects its rows with its reason,
// and that where an unresolved key carries a reason, it keeps its rows.
func TestOutcomes(t *testing.T) {
	f := newFixture(t)
	usd := "USD"
	rejected := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331005", ""))
	unresolved := securityKey("BETA", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331006", ""))
	f.outcomes = func(keys []gen.StatedKey) []gen.ResolutionKey {
		out := make([]gen.ResolutionKey, len(keys))
		for i, k := range keys {
			out[i] = gen.ResolutionKey{StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised, Reason: ptr.To("no datasource enabled")}
			if described(k, "ACME") {
				out[i].Outcome, out[i].Reason = gen.ResolutionOutcomeRejected, ptr.To("asset class equity contradicts the instrument's cash")
			}
		}
		return out
	}
	f.store.EXPECT().CreateStatement(gomock.Any(), gomock.Any()).Return(gen.Statement{}, nil)
	f.store.EXPECT().CreateStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStatedKeyParams) (gen.StatedKey, error) {
		return gen.StatedKey{ID: arg.ID, Identifiers: arg.Identifiers}, nil
	}).Times(2)
	f.store.EXPECT().DeleteTransactions(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	var items []gen.CreateStatementItemParams
	f.store.EXPECT().CreateStatementItem(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStatementItemParams) error {
		items = append(items, arg)
		return nil
	})
	var transactions []gen.CreateTransactionParams
	f.store.EXPECT().CreateTransaction(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateTransactionParams) (gen.Transaction, error) {
		transactions = append(transactions, arg)
		return gen.Transaction{}, nil
	})
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
	msg := &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*statementv1.Row{rowMsg(rejected, "2026-03-05", "1"), rowMsg(unresolved, "2026-03-06", "2")}}
	if _, err := f.svc.Create(context.Background(), userID, msg); err != nil || f.workErr != nil {
		t.Fatalf("Create() error = %v, work error = %v", err, f.workErr)
	}
	if len(items) != 1 || items[0].Ordinal != 0 || items[0].Reason != "asset class equity contradicts the instrument's cash" {
		t.Errorf("items = %+v, want the rejected key's row with its reason", items)
	}
	if len(transactions) != 1 || transactions[0].OrderDate.Day() != 6 {
		t.Errorf("transactions = %+v, want the unresolved key's row kept", transactions)
	}
}

// TestCreateSpec checks the run Create starts: a statement run of the user,
// in the broker's lane.
func TestCreateSpec(t *testing.T) {
	f := newFixture(t)
	f.store.EXPECT().CreateStatement(gomock.Any(), gomock.Any()).Return(gen.Statement{}, nil)
	f.store.EXPECT().DeleteTransactions(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
	msg := &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01"}
	if _, err := f.svc.Create(context.Background(), userID, msg); err != nil || f.workErr != nil {
		t.Fatalf("Create() error = %v, work error = %v", err, f.workErr)
	}
	if f.spec.Kind != gen.RunKindStatement || f.spec.Lane != "ibkr" || f.spec.UserID != userID {
		t.Errorf("Start(%+v), want a statement run of the user in lane ibkr", f.spec)
	}
}

// TestCreateFailures checks that a failing Prepare is returned by Create,
// and that a failing resolution fails the work before anything is written.
func TestCreateFailures(t *testing.T) {
	msg := func() *statementv1.Statement {
		return &statementv1.Statement{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
			Rows: []*statementv1.Row{rowMsg(cashKey("USD"), "2026-03-05", "1")}}
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
		f.resolveErr = boom
		if _, err := f.svc.Create(context.Background(), userID, msg()); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if !errors.Is(f.workErr, boom) {
			t.Errorf("work error = %v, want %v", f.workErr, boom)
		}
	})
}
