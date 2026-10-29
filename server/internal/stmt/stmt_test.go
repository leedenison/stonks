package stmt

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
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

// families is the currency vocabulary the fixture passes in, with GBX in the
// GBP family.
var families = currency.Families{"EUR": "EUR", "GBP": "GBP", "GBX": "GBP", "USD": "USD"}

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

func rowMsg(key *typev1.StatedKey, order, quantity string) *stmtv1.Row {
	return &stmtv1.Row{Key: key, OrderDate: order, SettlementDate: order, AsAt: order, Quantity: quantity}
}

type fixture struct {
	store    *MockStore
	runs     *MockRunner
	resolver *MockResolver
	facts    *MockFacts
	svc      *Service
	spec     run.Spec
	workErr  error
	// outcomes is what the resolver answers for the keys, every key
	// unrecognised by default, and resolveErr is its error.
	outcomes   func(keys []gen.StatedKey) []gen.ResolutionKey
	resolveErr error
	// written is what the mock resolver returns as the facts it wrote.
	written []resolve.WrittenIdentifierFacts
	// replayed records each call to follow new facts or a statement.
	replayed []replayCall
	// followed names the calls after the write and the settling of the
	// statement, in the order they came.
	followed []string
	// dates holds every key the resolver was sent, with its as-at dates.
	dates []resolve.KeyDates
	// calls names the regroup, the sweep and the completion in the order
	// the write made them.
	calls []string
}

// replayCall is one call to follow new facts or a statement. stmt is set
// for a statement.
type replayCall struct {
	stmt    gen.Run
	except  gen.Run
	written []resolve.WrittenIdentifierFacts
}

// newFixture returns a service whose store runs a transaction inline over
// itself and whose runner runs Prepare and the work inline, so a case
// observes every write in order.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{store: NewMockStore(ctrl), runs: NewMockRunner(ctrl), resolver: NewMockResolver(ctrl), facts: NewMockFacts(ctrl)}
	f.outcomes = func(keys []gen.StatedKey) []gen.ResolutionKey {
		out := make([]gen.ResolutionKey, len(keys))
		for i, k := range keys {
			out[i] = gen.ResolutionKey{StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised}
		}
		return out
	}
	f.resolver.EXPECT().Resolve(gomock.Any(), gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, _ gen.Run, keys []resolve.KeyDates) ([]gen.ResolutionKey, []resolve.WrittenIdentifierFacts, error) {
		if f.resolveErr != nil {
			return nil, nil, f.resolveErr
		}
		rows := make([]gen.StatedKey, len(keys))
		for i, k := range keys {
			rows[i] = k.Row
		}
		f.dates = append(f.dates, keys...)
		return f.outcomes(rows), f.written, nil
	}).AnyTimes()
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error { return fn(f.store) }).AnyTimes()
	f.store.EXPECT().LockUserKeys(gomock.Any(), gomock.Any()).Return(nil).AnyTimes()
	f.store.EXPECT().ListGroupableKeys(gomock.Any(), gomock.Any()).Return(nil, nil).AnyTimes()
	f.store.EXPECT().ClearStatedKeyGroups(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error {
		f.calls = append(f.calls, "regroup")
		return nil
	}).AnyTimes()
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
	f.facts.EXPECT().AfterIdentifierFacts(gomock.Any(), gomock.Any(), gomock.Any()).Do(func(_ context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts) {
		f.replayed = append(f.replayed, replayCall{except: except, written: written})
		f.followed = append(f.followed, "facts")
	}).AnyTimes()
	f.facts.EXPECT().AfterStmt(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Do(func(_ context.Context, stmt, except gen.Run, written []resolve.WrittenIdentifierFacts) {
		f.replayed = append(f.replayed, replayCall{stmt, except, written})
		f.followed = append(f.followed, "follow-ups")
	}).AnyTimes()
	f.store.EXPECT().SettleStmt(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error {
		f.followed = append(f.followed, "settled")
		return nil
	}).AnyTimes()
	f.svc = New(f.store, f.runs, f.resolver, f.facts, families, clock, slog.New(slog.DiscardHandler))
	return f
}

func newIngestion(t *testing.T) (*fixture, *ingestion) {
	t.Helper()
	f := newFixture(t)
	return f, &ingestion{store: f.store, runs: f.runs, resolver: f.resolver, user: userID, broker: gen.BrokerIbkr, from: from, before: until, keys: map[string]*key{}}
}

func TestCreateInvalid(t *testing.T) {
	usd, xxx := "USD", "XXX"
	good := func() *stmtv1.Stmt {
		return &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01"}
	}
	tests := []struct {
		name string
		edit func(m *stmtv1.Stmt)
	}{
		{name: "unspecified broker", edit: func(m *stmtv1.Stmt) { m.Broker = typev1.Broker_BROKER_UNSPECIFIED }},
		{name: "undefined broker", edit: func(m *stmtv1.Stmt) { m.Broker = 99 }},
		{name: "malformed order_from", edit: func(m *stmtv1.Stmt) { m.OrderFrom = "2026-13-01" }},
		{name: "malformed order_before", edit: func(m *stmtv1.Stmt) { m.OrderBefore = "soon" }},
		{name: "empty period", edit: func(m *stmtv1.Stmt) { m.OrderBefore = m.OrderFrom }},
		{name: "split without a key", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{{EffectiveDate: "2026-03-02", Quantity: "9"}}
		}},
		{name: "split with a bad date", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), EffectiveDate: "2026-03-40", Quantity: "9"}}
		}},
		{name: "split with a bad ratio", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd), EffectiveDate: "2026-03-02", Quantity: "9", Ratio: &stmtv1.SplitRatio{From: "1", To: "ten"}}}
		}},
		{name: "split with an unknown currency", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &xxx), EffectiveDate: "2026-03-02", Quantity: "9"}}
		}},
		{name: "split with an inadmissible key", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{{Key: securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", ""), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US2", "")), EffectiveDate: "2026-03-02", Quantity: "9"}}
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
			if f.spec.UserID != nil {
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
		row  *stmtv1.Row
		want string
	}{
		{name: "accepted", row: rowMsg(equity, "2026-03-05", "10")},
		{name: "on the first day", row: rowMsg(equity, "2026-03-01", "10")},
		{name: "no key", row: &stmtv1.Row{OrderDate: "2026-03-05"}, want: "no key"},
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
		{name: "malformed order date", row: &stmtv1.Row{Key: equity, OrderDate: "2026-03-40", SettlementDate: "2026-03-05", AsAt: "2026-03-05", Quantity: "1"}, want: `malformed order date "2026-03-40"`},
		{name: "malformed settlement date", row: &stmtv1.Row{Key: equity, OrderDate: "2026-03-05", SettlementDate: "", AsAt: "2026-03-05", Quantity: "1"}, want: `malformed settlement date ""`},
		{name: "malformed as at", row: &stmtv1.Row{Key: equity, OrderDate: "2026-03-05", SettlementDate: "2026-03-05", AsAt: "5 March", Quantity: "1"}, want: `malformed as at "5 March"`},
		{name: "malformed quantity", row: rowMsg(equity, "2026-03-05", "ten"), want: `malformed quantity "ten"`},
		{name: "before the period", row: rowMsg(equity, "2026-02-28", "1"), want: "order date outside the claimed period"},
		{name: "on the day after the period", row: rowMsg(equity, "2026-05-01", "1"), want: "order date outside the claimed period"},
		{name: "after today", row: rowMsg(equity, "2026-04-20", "1"), want: "order date after today"},
		{name: "unknown currency", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &xxx), "2026-03-05", "1"), want: `unknown currency "XXX"`},
		{name: "a currency identifier stating no currency", row: rowMsg(&typev1.StatedKey{Identifiers: cashKey("USD").Identifiers, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH}, "2026-03-05", "1")},
		{name: "a description of another broker", row: rowMsg(securityKey("", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, "ACME", "schwab")), "2026-03-05", "1"), want: `broker_description identifier of broker "schwab" in a statement of ibkr`},
		{name: "a datasource's ticker", row: rowMsg(securityKey("", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_DATASOURCE_TICKER, "VOD.LSE", "eodhd")), "2026-03-05", "1"), want: "datasource_ticker identifier in a statement"},
		{name: "a broker id of another broker", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_ID, "1", "schwab")), "2026-03-05", "1"), want: `broker_id identifier of broker "schwab" in a statement of ibkr`},
		{name: "an empty value", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "", "")), "2026-03-05", "1"), want: "isin identifier with no value"},
		{name: "a currency identifier on an equity key", row: rowMsg(securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, "USD", "")), "2026-03-05", "1")},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, g := newIngestion(t)
			// The period runs past today so the after-today check is reached.
			g.before = time.Date(2026, time.May, 1, 0, 0, 0, 0, time.UTC)
			got := g.validate(3, tc.row, today, currency.Families{"USD": "USD", "EUR": "EUR"})
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
		{name: "a repeated identifier", a: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", "XNYS"), mic("X", "XNYS")), b: securityKey("A", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, mic("X", "XNYS")), same: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, g := newIngestion(t)
			a := g.validate(0, rowMsg(tc.a, "2026-03-05", "1"), today, currency.Families{"USD": "USD"})
			b := g.validate(1, rowMsg(tc.b, "2026-03-05", "1"), today, currency.Families{"USD": "USD"})
			if a.reason != "" || b.reason != "" {
				t.Fatalf("validate rejected %q / %q", a.reason, b.reason)
			}
			if (a.key == b.key) != tc.same {
				t.Errorf("keys %s: same = %v, want %v", tc.name, a.key == b.key, tc.same)
			}
		})
	}
	_, g := newIngestion(t)
	got := g.validate(0, rowMsg(securityKey("A", typev1.AssetClass_ASSET_CLASS_STOCK, &usd, mic("Y", "XNYS"), ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US1", ""), mic("X", "")), "2026-03-05", "1"), today, currency.Families{"USD": "USD"})
	want := []types.Identifier{{Type: "broker_description", Domain: "ibkr", Value: "A"}, {Type: "isin", Value: "US1"}, {Type: "mic_ticker", Value: "X"}, {Type: "mic_ticker", Domain: "XNYS", Value: "Y"}}
	if diff := cmp.Diff(want, got.key.identifiers); diff != "" {
		t.Errorf("identifiers order mismatch (-want +got):\n%s", diff)
	}
	if got.key.class == nil || *got.key.class != types.AssetClassStock {
		t.Errorf("class = %v, want stock", got.key.class)
	}
}

// TestOutcomes checks how the resolver's answer maps back onto keys and rows.
// A rejected key rejects its rows with its reason. An unresolved key keeps its
// rows. The answer may arrive in any order, and one that leaves a key out
// fails the work.
func TestOutcomes(t *testing.T) {
	tests := []struct {
		name    string
		answer  func(out []gen.ResolutionKey) []gen.ResolutionKey
		wantErr bool
	}{
		{name: "in key order", answer: func(out []gen.ResolutionKey) []gen.ResolutionKey { return out }},
		{name: "in reverse order", answer: func(out []gen.ResolutionKey) []gen.ResolutionKey {
			slices.Reverse(out)
			return out
		}},
		{name: "a key left out", answer: func(out []gen.ResolutionKey) []gen.ResolutionKey { return out[1:] }, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			usd := "USD"
			rejected := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331005", ""))
			unresolved := securityKey("BETA", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331006", ""))
			f.outcomes = func(keys []gen.StatedKey) []gen.ResolutionKey {
				out := make([]gen.ResolutionKey, len(keys))
				for i, k := range keys {
					out[i] = gen.ResolutionKey{StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised, Reasons: []string{"no datasource enabled"}}
					if described(k, "ACME") {
						out[i].Outcome, out[i].Reasons = gen.ResolutionOutcomeRejected, []string{"asset class equity contradicts the instrument's cash"}
					}
				}
				return tc.answer(out)
			}
			f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
			f.store.EXPECT().UpsertStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.UpsertStatedKeyParams) (gen.StatedKey, error) {
				return gen.StatedKey{ID: arg.ID, Identifiers: arg.Identifiers}, nil
			}).Times(2)
			f.store.EXPECT().CreateStmtKeys(gomock.Any(), gomock.Any()).Return(int64(2), nil)
			var items []gen.CreateStmtItemsParams
			var txns []gen.CreateTxnsParams
			if !tc.wantErr {
				f.store.EXPECT().DeleteTxns(gomock.Any(), gomock.Any()).Return(int64(0), nil)
				f.store.EXPECT().CreateStmtItems(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg []gen.CreateStmtItemsParams) (int64, error) {
					items = append(items, arg...)
					return int64(len(arg)), nil
				})
				f.store.EXPECT().CreateTxns(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg []gen.CreateTxnsParams) (int64, error) {
					txns = append(txns, arg...)
					return int64(len(arg)), nil
				})
				f.store.EXPECT().SweepStatedKeys(gomock.Any(), userID).Return(int64(0), nil)
				f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
			}
			msg := &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
				Rows: []*stmtv1.Row{rowMsg(rejected, "2026-03-05", "1"), rowMsg(unresolved, "2026-03-06", "2")}}
			if _, err := f.svc.Create(context.Background(), userID, msg); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if (f.workErr != nil) != tc.wantErr {
				t.Fatalf("work error = %v, wantErr %v", f.workErr, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if len(items) != 1 || items[0].Ordinal != 0 || items[0].Reason != "asset class equity contradicts the instrument's cash" {
				t.Errorf("items = %+v, want the rejected key's row with its reason", items)
			}
			if len(txns) != 1 || txns[0].OrderDate.Day() != 6 {
				t.Errorf("transactions = %+v, want the unresolved key's row kept", txns)
			}
		})
	}
}

// TestResolvesUnassociated checks that when an earlier statement associated
// a key, the resolver is not sent it, the statement keeps its rows and links
// to it all the same, and that an answer leaving out a key the resolver was
// sent still fails the work.
func TestResolvesUnassociated(t *testing.T) {
	for _, omit := range []bool{false, true} {
		t.Run(fmt.Sprintf("answer omits the sent key %t", omit), func(t *testing.T) {
			f := newFixture(t)
			usd := "USD"
			held := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331005", ""))
			fresh := securityKey("BETA", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331006", ""))
			instr := db.NewID()
			var sent []gen.StatedKey
			f.outcomes = func(keys []gen.StatedKey) []gen.ResolutionKey {
				sent = keys
				if omit {
					return nil
				}
				out := make([]gen.ResolutionKey, len(keys))
				for i, k := range keys {
					out[i] = gen.ResolutionKey{StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeUnrecognised, Reasons: []string{"no datasource enabled"}}
				}
				return out
			}
			f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
			f.store.EXPECT().UpsertStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.UpsertStatedKeyParams) (gen.StatedKey, error) {
				row := gen.StatedKey{ID: arg.ID, Identifiers: arg.Identifiers}
				if described(row, "ACME") {
					row.InstrID = &instr
				}
				return row, nil
			}).Times(2)
			var links []gen.CreateStmtKeysParams
			f.store.EXPECT().CreateStmtKeys(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg []gen.CreateStmtKeysParams) (int64, error) {
				links = arg
				return int64(len(arg)), nil
			})
			var txns []gen.CreateTxnsParams
			if !omit {
				f.store.EXPECT().DeleteTxns(gomock.Any(), gomock.Any()).Return(int64(0), nil)
				f.store.EXPECT().CreateTxns(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg []gen.CreateTxnsParams) (int64, error) {
					txns = arg
					return int64(len(arg)), nil
				})
				f.store.EXPECT().SweepStatedKeys(gomock.Any(), userID).Return(int64(0), nil)
				f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
			}
			msg := &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
				Rows: []*stmtv1.Row{rowMsg(held, "2026-03-05", "1"), rowMsg(fresh, "2026-03-06", "2")}}
			if _, err := f.svc.Create(context.Background(), userID, msg); err != nil {
				t.Fatalf("Create() error = %v", err)
			}
			if (f.workErr != nil) != omit {
				t.Fatalf("work error = %v, want an error %t", f.workErr, omit)
			}
			if len(sent) != 1 || !described(sent[0], "BETA") {
				t.Errorf("Resolve received %+v, want the unassociated key alone", sent)
			}
			if len(links) != 2 {
				t.Errorf("links = %+v, want both keys linked", links)
			}
			if !omit && len(txns) != 2 {
				t.Errorf("transactions = %+v, want both rows kept", txns)
			}
		})
	}
}

// TestResolvesWithDates checks that a dated key is sent again with the
// distinct as-at dates of its rows, leaving out a row validation rejected,
// and that a key with a fixed association is not sent.
func TestResolvesWithDates(t *testing.T) {
	f := newFixture(t)
	usd := "USD"
	dated := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "ACME", "XNAS"))
	fixed := securityKey("BETA", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331006", ""))
	instr := db.NewID()
	f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
	f.store.EXPECT().UpsertStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.UpsertStatedKeyParams) (gen.StatedKey, error) {
		row := gen.StatedKey{ID: arg.ID, Identifiers: arg.Identifiers}
		if described(row, "ACME") {
			row.ViaTickerID, row.ViaMarket, row.ViaVenue = ptr.To(db.NewID()), ptr.To("US"), ptr.To("XNAS")
		} else {
			row.InstrID = &instr
		}
		return row, nil
	}).Times(2)
	f.store.EXPECT().CreateStmtKeys(gomock.Any(), gomock.Any()).Return(int64(2), nil)
	f.store.EXPECT().DeleteTxns(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.store.EXPECT().CreateTxns(gomock.Any(), gomock.Any()).Return(int64(3), nil)
	f.store.EXPECT().CreateStmtItems(gomock.Any(), gomock.Any()).Return(int64(1), nil)
	f.store.EXPECT().SweepStatedKeys(gomock.Any(), userID).Return(int64(0), nil)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
	msg := &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*stmtv1.Row{
			rowMsg(dated, "2026-03-07", "1"), rowMsg(dated, "2026-03-05", "2"), rowMsg(dated, "2026-03-07", "3"),
			rowMsg(dated, "2026-03-09", "many"), rowMsg(fixed, "2026-03-06", "4"),
		}}
	if _, err := f.svc.Create(context.Background(), userID, msg); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if f.workErr != nil {
		t.Fatalf("work error = %v", f.workErr)
	}
	if len(f.dates) != 1 || !described(f.dates[0].Row, "ACME") {
		t.Fatalf("Resolve received %+v, want the dated key alone", f.dates)
	}
	want := []time.Time{time.Date(2026, 3, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 5, 0, 0, 0, 0, time.UTC)}
	if diff := cmp.Diff(want, f.dates[0].Dates); diff != "" {
		t.Errorf("order dates sent mismatch (-want +got):\n%s", diff)
	}
}

// TestReplaysNewFacts checks that ingestion replays the keys that state an
// identifier of its resolution's new identifier facts, once the write has
// completed the ingest statement run.
func TestReplaysNewFacts(t *testing.T) {
	f := newFixture(t)
	usd := "USD"
	stock := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, "ACME", "XNAS"))
	f.written = []resolve.WrittenIdentifierFacts{{Run: gen.Run{ID: db.NewID()}, Tickers: []types.MarketTicker{{Market: "US", Value: "ACME"}}}}
	f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
	f.store.EXPECT().UpsertStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.UpsertStatedKeyParams) (gen.StatedKey, error) {
		return gen.StatedKey{ID: arg.ID, Identifiers: arg.Identifiers}, nil
	})
	f.store.EXPECT().CreateStmtKeys(gomock.Any(), gomock.Any()).Return(int64(1), nil)
	f.store.EXPECT().DeleteTxns(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.store.EXPECT().CreateTxns(gomock.Any(), gomock.Any()).Return(int64(1), nil)
	f.store.EXPECT().SweepStatedKeys(gomock.Any(), userID).Return(int64(0), nil)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error {
		if len(f.replayed) != 0 {
			t.Error("the replay ran before the ingest statement run completed")
		}
		return nil
	})
	msg := &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows: []*stmtv1.Row{rowMsg(stock, "2026-03-05", "1")}}
	if _, err := f.svc.Create(context.Background(), userID, msg); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if f.workErr != nil {
		t.Errorf("work error = %v, want none", f.workErr)
	}
	if len(f.replayed) != 1 || f.replayed[0].stmt.Kind != gen.RunKindIngestStmt || f.replayed[0].except.Kind != gen.RunKindResolveStmtKeys || !cmp.Equal(f.replayed[0].written, f.written) {
		t.Errorf("follow-ups = %+v, want one of the ingest statement run, excepting the resolution, with its facts", f.replayed)
	}
	if diff := cmp.Diff([]string{"follow-ups", "settled"}, f.followed); diff != "" {
		t.Errorf("calls after the write mismatch (-want +got):\n%s", diff)
	}
}

// TestPrepareTakesRow checks that the links, the split and the transaction
// name the key the upsert returned, whose id differs from the one minted for
// it when another statement stated the key first.
func TestPrepareTakesRow(t *testing.T) {
	f := newFixture(t)
	usd := "USD"
	acme := securityKey("ACME", typev1.AssetClass_ASSET_CLASS_EQUITY, &usd, ident(typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, "US0378331005", ""))
	held := db.NewID()
	f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
	f.store.EXPECT().UpsertStatedKey(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.UpsertStatedKeyParams) (gen.StatedKey, error) {
		if arg.ID == held {
			t.Errorf("UpsertStatedKey minted %s, the id of the row another statement holds", held)
		}
		return gen.StatedKey{ID: held, Identifiers: arg.Identifiers}, nil
	})
	var links []gen.CreateStmtKeysParams
	f.store.EXPECT().CreateStmtKeys(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg []gen.CreateStmtKeysParams) (int64, error) {
		links = arg
		return int64(len(arg)), nil
	})
	var split gen.CreateStmtSplitParams
	f.store.EXPECT().CreateStmtSplit(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg gen.CreateStmtSplitParams) error {
		split = arg
		return nil
	})
	f.store.EXPECT().DeleteTxns(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	var txns []gen.CreateTxnsParams
	f.store.EXPECT().CreateTxns(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, arg []gen.CreateTxnsParams) (int64, error) {
		txns = arg
		return int64(len(arg)), nil
	})
	f.store.EXPECT().SweepStatedKeys(gomock.Any(), userID).Return(int64(0), nil)
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).Return(nil)
	msg := &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
		Rows:   []*stmtv1.Row{rowMsg(acme, "2026-03-05", "1")},
		Splits: []*stmtv1.StatedSplit{{Key: acme, EffectiveDate: "2026-03-02", Quantity: "9"}}}
	if _, err := f.svc.Create(context.Background(), userID, msg); err != nil || f.workErr != nil {
		t.Fatalf("Create() error = %v, work error = %v", err, f.workErr)
	}
	if len(links) != 1 || links[0].StatedKeyID != held || split.StatedKeyID != held || len(txns) != 1 || txns[0].StatedKeyID != held {
		t.Errorf("links %+v, split %+v, transactions %+v: want each to name the key %s the upsert returned", links, split, txns, held)
	}
}

// TestCreateSpec checks the run Create starts: an ingest statement run of the
// user, in the broker's lane, whose write regroups, then sweeps, then
// completes.
func TestCreateSpec(t *testing.T) {
	f := newFixture(t)
	f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
	f.store.EXPECT().DeleteTxns(gomock.Any(), gomock.Any()).Return(int64(0), nil)
	f.store.EXPECT().SweepStatedKeys(gomock.Any(), userID).DoAndReturn(func(context.Context, uuid.UUID) (int64, error) {
		f.calls = append(f.calls, "sweep")
		return 0, nil
	})
	f.store.EXPECT().CompleteRun(gomock.Any(), gomock.Any()).DoAndReturn(func(context.Context, uuid.UUID) error {
		f.calls = append(f.calls, "complete")
		return nil
	})
	msg := &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01"}
	if _, err := f.svc.Create(context.Background(), userID, msg); err != nil || f.workErr != nil {
		t.Fatalf("Create() error = %v, work error = %v", err, f.workErr)
	}
	if f.spec.Kind != gen.RunKindIngestStmt || f.spec.Lane != "ibkr" || f.spec.UserID == nil || *f.spec.UserID != userID {
		t.Errorf("Start(%+v), want an ingest statement run of the user in lane ibkr", f.spec)
	}
	if diff := cmp.Diff([]string{"regroup", "sweep", "complete"}, f.calls); diff != "" {
		t.Errorf("write order mismatch (-want +got):\n%s", diff)
	}
}

// TestCreateFailures checks that a failing Prepare is returned by Create,
// and that a failing resolution fails the work before anything is written.
func TestCreateFailures(t *testing.T) {
	msg := func() *stmtv1.Stmt {
		return &stmtv1.Stmt{Broker: typev1.Broker_BROKER_SCHWAB, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01",
			Rows: []*stmtv1.Row{rowMsg(cashKey("USD"), "2026-03-05", "1")}}
	}
	boom := errors.New("boom")
	t.Run("prepare", func(t *testing.T) {
		f := newFixture(t)
		f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, boom)
		if _, err := f.svc.Create(context.Background(), userID, msg()); !errors.Is(err, boom) {
			t.Errorf("Create() error = %v, want %v", err, boom)
		}
	})
	t.Run("resolution", func(t *testing.T) {
		f := newFixture(t)
		f.store.EXPECT().CreateStmt(gomock.Any(), gomock.Any()).Return(gen.Stmt{}, nil)
		f.store.EXPECT().UpsertStatedKey(gomock.Any(), gomock.Any()).Return(gen.StatedKey{}, nil)
		f.store.EXPECT().CreateStmtKeys(gomock.Any(), gomock.Any()).Return(int64(1), nil)
		f.resolveErr = boom
		if _, err := f.svc.Create(context.Background(), userID, msg()); err != nil {
			t.Fatalf("Create() error = %v", err)
		}
		if !errors.Is(f.workErr, boom) {
			t.Errorf("work error = %v, want %v", f.workErr, boom)
		}
	})
}

func TestLive(t *testing.T) {
	settled := time.Date(2026, time.April, 10, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name    string
		state   gen.RunState
		settled *time.Time
		want    bool
	}{
		{name: "pending", state: gen.RunStatePending, want: true},
		{name: "running", state: gen.RunStateRunning, want: true},
		{name: "completed and unsettled", state: gen.RunStateCompleted, want: true},
		{name: "completed and settled", state: gen.RunStateCompleted, settled: &settled, want: false},
		{name: "failed and unsettled", state: gen.RunStateFailed, want: false},
		{name: "interrupted and unsettled", state: gen.RunStateInterrupted, want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := Live(gen.Stmt{SettledAt: tc.settled}, gen.Run{State: tc.state}); got != tc.want {
				t.Errorf("Live(settled at %v, run %s) = %v, want %v", tc.settled, tc.state, got, tc.want)
			}
		})
	}
}
