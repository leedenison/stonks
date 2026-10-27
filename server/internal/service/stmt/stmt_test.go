package stmt

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	runv1 "github.com/leedenison/stonks/proto/run/v1"
	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	"github.com/leedenison/stonks/proto/stmt/v1/stmtv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/service/stmt/mock"
	"github.com/leedenison/stonks/server/internal/stmt"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	runID     = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	created   = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	from      = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	before    = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: servicetest.Session}
	run       = gen.Run{ID: runID, UserID: &userID, Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser, State: gen.RunStatePending, CreatedAt: created}
	runMsg    = &runv1.Run{Id: runID.String(), Kind: runv1.RunKind_RUN_KIND_INGEST_STMT, Trigger: runv1.RunTrigger_RUN_TRIGGER_USER, State: runv1.RunState_RUN_STATE_PENDING, CreatedAt: timestamppb.New(created)}
	stmtRow   = gen.Stmt{ID: runID, UserID: userID, Broker: gen.BrokerIbkr, OrderFrom: from, OrderBefore: before, RowCount: 3, CreatedAt: created}
	summ      = &stmtv1.StmtSummary{Run: runMsg, Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01", Rows: 3, Rejected: 1}
)

type fixture struct {
	ingester  *mock.MockIngester
	confirmer *mock.MockConfirmer
	reader    *mock.MockReader
	authn     *servicemock.MockAuthenticator
	client    stmtv1connect.StmtServiceClient
}

// newFixture mounts a Server with the real handler chain and a client that
// carries a session cookie.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{ingester: mock.NewMockIngester(ctrl), confirmer: mock.NewMockConfirmer(ctrl), reader: mock.NewMockReader(ctrl), authn: servicemock.NewMockAuthenticator(ctrl)}
	opts := servicetest.Options(t, f.authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(stmtv1connect.NewStmtServiceHandler(New(f.ingester, f.confirmer, f.reader, mic.MICMap{"XNAS": {Operating: "XNAS", Market: "US"}}), opts...))
	})
	f.client = stmtv1connect.NewStmtServiceClient(srv.Client, srv.URL)
	return f
}

func stmtMsg() *stmtv1.Stmt {
	return &stmtv1.Stmt{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01"}
}

// splitMsg returns a split of key that passes every check but those on the key.
func splitMsg(key *typev1.StatedKey) *stmtv1.StatedSplit {
	return &stmtv1.StatedSplit{Key: key, EffectiveDate: "2026-03-05", Quantity: "10"}
}

func TestCreateStmt(t *testing.T) {
	tests := []struct {
		name     string
		msg      *stmtv1.Stmt
		edit     func(*stmtv1.Stmt)
		authErr  error
		err      error
		called   bool
		wantCode connect.Code
	}{
		{name: "created", called: true},
		{name: "empty period", edit: func(m *stmtv1.Stmt) { m.OrderBefore = m.OrderFrom }, wantCode: connect.CodeInvalidArgument},
		{name: "no broker", edit: func(m *stmtv1.Stmt) { m.Broker = typev1.Broker_BROKER_UNSPECIFIED }, wantCode: connect.CodeInvalidArgument},
		{name: "malformed split", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{{Key: &typev1.StatedKey{}, EffectiveDate: "2026-03-05", Quantity: "ten"}}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split currency not a code", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{splitMsg(&typev1.StatedKey{Currency: ptr.To("usd")})}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split identifier without a type", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{splitMsg(&typev1.StatedKey{Identifiers: []*typev1.Identifier{{Value: "ACME"}}})}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split identifier without a value", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{splitMsg(&typev1.StatedKey{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN}}})}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split key with a currency and identifiers", edit: func(m *stmtv1.Stmt) {
			m.Splits = []*stmtv1.StatedSplit{splitMsg(&typev1.StatedKey{Currency: ptr.To("USD"), Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000001"}}})}
		}, called: true},
		{name: "too many rows", edit: func(m *stmtv1.Stmt) {
			m.Rows = make([]*stmtv1.Row, 50001)
			for i := range m.Rows {
				m.Rows[i] = &stmtv1.Row{}
			}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "too many splits", edit: func(m *stmtv1.Stmt) {
			m.Splits = make([]*stmtv1.StatedSplit, 1001)
			for i := range m.Splits {
				m.Splits[i] = splitMsg(&typev1.StatedKey{})
			}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "unreadable", err: fmt.Errorf("%w: split 0: no key", stmt.ErrInvalid), called: true, wantCode: connect.CodeInvalidArgument},
		{name: "failure", err: errors.New("boom"), called: true, wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			msg := stmtMsg()
			if tc.edit != nil {
				tc.edit(msg)
			}
			if tc.called {
				f.ingester.EXPECT().Create(gomock.Any(), userID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, got *stmtv1.Stmt) (gen.Run, error) {
					if diff := cmp.Diff(msg, got, protocmp.Transform()); diff != "" {
						t.Errorf("Create statement mismatch (-want +got):\n%s", diff)
					}
					return run, tc.err
				})
			}
			res, err := f.client.CreateStmt(context.Background(), connect.NewRequest(&stmtv1.CreateStmtRequest{Stmt: msg}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("CreateStmt code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(&stmtv1.CreateStmtResponse{Run: runMsg}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("CreateStmt mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListStmts(t *testing.T) {
	tests := []struct {
		name     string
		authErr  error
		rows     []gen.ListStmtsRow
		err      error
		want     *stmtv1.ListStmtsResponse
		wantCode connect.Code
	}{
		{name: "none", want: &stmtv1.ListStmtsResponse{}},
		{name: "one", rows: []gen.ListStmtsRow{{Stmt: stmtRow, Run: run, Rejected: 1}}, want: &stmtv1.ListStmtsResponse{Stmts: []*stmtv1.StmtSummary{summ}}},
		{name: "failure", err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.authErr == nil {
				f.reader.EXPECT().ListStmts(gomock.Any(), userID).Return(tc.rows, tc.err)
			}
			res, err := f.client.ListStmts(context.Background(), connect.NewRequest(&stmtv1.ListStmtsRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListStmts code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListStmts mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetStmt(t *testing.T) {
	acme := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ACME"}
	rowMsg := &stmtv1.Row{Key: &typev1.StatedKey{Identifiers: []*typev1.Identifier{acme}}, OrderDate: "2026-03-05", SettlementDate: "2026-03-07", AsAt: "2026-03-05", Quantity: "10"}
	stated, err := protojson.Marshal(rowMsg)
	if err != nil {
		t.Fatal(err)
	}
	items := []gen.StmtItem{{StmtID: runID, UserID: userID, Ordinal: 2, Reason: "no admissible identifier", Stated: stated}}
	// Three keys the statement stated: one matched by its latest resolution
	// after an earlier one left it unavailable, one left unrecognised, and
	// one no resolution has reached.
	matchedID, unrecognisedID, unreachedID := uuid.MustParse("00000000-0000-0000-0000-000000000021"), uuid.MustParse("00000000-0000-0000-0000-000000000022"), uuid.MustParse("00000000-0000-0000-0000-000000000023")
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	keys := []gen.StatedKey{
		{ID: matchedID, UserID: userID, Identifiers: []types.Identifier{isin}, Arbiter: ptr.To(gen.ArbiterUser)},
		{ID: unrecognisedID, UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME"}}},
		{ID: unreachedID, UserID: userID, Currency: ptr.To("GBP"), Identifiers: []types.Identifier{}},
	}
	latest := []gen.ResolutionKey{
		{StatedKeyID: matchedID, Outcome: gen.ResolutionOutcomeMatched},
		{StatedKeyID: unrecognisedID, Outcome: gen.ResolutionOutcomeUnrecognised, Reasons: []string{"no global identifier"}, Offered: 2},
	}
	keyMsgs := []*typev1.ResolutionItem{
		{StatedKey: &typev1.StatedKey{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0378331005"}}}, StatedKeyId: matchedID.String(), Outcome: typev1.ResolutionOutcome_RESOLUTION_OUTCOME_MATCHED, Arbiter: typev1.Arbiter_ARBITER_USER},
		{StatedKey: &typev1.StatedKey{Identifiers: []*typev1.Identifier{acme}}, StatedKeyId: unrecognisedID.String(), Outcome: typev1.ResolutionOutcome_RESOLUTION_OUTCOME_UNRECOGNISED, Reasons: []string{"no global identifier"}, Offered: 2},
		{StatedKey: &typev1.StatedKey{Currency: ptr.To("GBP")}, StatedKeyId: unreachedID.String()},
	}
	// Three transactions: two of a key dated through META, one before the
	// rename and one on the asserted day, and one of a key fixed through a
	// broker description.
	datedID, fixedID := uuid.MustParse("00000000-0000-0000-0000-000000000024"), uuid.MustParse("00000000-0000-0000-0000-000000000025")
	meta := gen.Ticker{ID: uuid.MustParse("00000000-0000-0000-0000-000000000026"), Market: "US", Value: "META"}
	dated := gen.StatedKey{ID: datedID, UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "META"}}, ViaTickerID: &meta.ID, ViaMarket: ptr.To("US"), ViaVenue: ptr.To("XNAS"), Arbiter: ptr.To(gen.ArbiterDatasource)}
	fixed := gen.StatedKey{ID: fixedID, UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME"}}, InstrID: ptr.To(uuid.New()), Arbiter: ptr.To(gen.ArbiterDatasource)}
	txn := func(n int, key gen.StatedKey, on time.Time) gen.Txn {
		return gen.Txn{ID: uuid.MustParse(fmt.Sprintf("00000000-0000-0000-0000-00000000003%d", n)), UserID: userID, StmtID: runID, StatedKeyID: key.ID, OrderDate: on, AsAt: on, Quantity: decimal.NewFromInt(10)}
	}
	txns := []gen.ListStmtTxnsRow{
		{Txn: txn(1, dated, notFoundOn), StatedKey: dated},
		{Txn: txn(2, dated, assertedOn), StatedKey: dated},
		{Txn: txn(3, fixed, assertedOn), StatedKey: fixed, FixedViaType: ptr.To(types.IdentifierTypeBrokerDescription)},
	}
	facts := metaFacts()
	assertions := []gen.ListIdentifierAssertionsRow{
		{IdentifierAssertion: gen.IdentifierAssertion{Market: "US", AssertedOn: assertedOn, InstrID: &metaInst, FetchKeyID: ptr.To(uuid.New())}, Value: "META", Datasource: ptr.To("massive"), Precedence: ptr.To(int32(2))},
		{IdentifierAssertion: gen.IdentifierAssertion{Market: "US", AssertedOn: notFoundOn, FetchKeyID: ptr.To(uuid.New())}, Value: "META", Datasource: ptr.To("massive"), Precedence: ptr.To(int32(2))},
		{IdentifierAssertion: gen.IdentifierAssertion{Market: "US", AssertedOn: renamed, InstrID: &metaInst, FetchKeyID: ptr.To(uuid.New()), Edge: ptr.To(gen.IdentifierEdgeFirst)}, Value: "META", Datasource: ptr.To("massive"), Precedence: ptr.To(int32(2))},
	}
	periods := []gen.ListIdentifierPeriodAssertionsRow{{Market: "US", Value: ptr.To("META"), AssertedFrom: facts.PeriodAssertions[0].Period.From, AssertedBefore: facts.PeriodAssertions[0].Period.Before}}
	txnMsgs := []*stmtv1.Txn{
		{Id: txns[0].Txn.ID.String(), StatedKeyId: datedID.String(), OrderDate: "2021-06-01", Quantity: "10", Mark: stmtv1.TxnMark_TXN_MARK_UNRESOLVED},
		{Id: txns[1].Txn.ID.String(), StatedKeyId: datedID.String(), OrderDate: "2024-01-16", Quantity: "10", Mark: stmtv1.TxnMark_TXN_MARK_CONFIRMED},
		{Id: txns[2].Txn.ID.String(), StatedKeyId: fixedID.String(), OrderDate: "2024-01-16", Quantity: "10", Mark: stmtv1.TxnMark_TXN_MARK_PROVISIONAL},
	}
	tests := []struct {
		name          string
		id            string
		authErr       error
		row           gen.GetStmtRow
		err           error
		items         []gen.StmtItem
		itemsErr      error
		readsKeys     bool
		keys          []gen.StatedKey
		keysErr       error
		latest        []gen.ResolutionKey
		latestErr     error
		txns          []gen.ListStmtTxnsRow
		txnsErr       error
		assertionsErr error
		want          *stmtv1.GetStmtResponse
		wantCode      connect.Code
	}{
		{
			name: "with items", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run, Rejected: 1}, items: items, readsKeys: true,
			want: &stmtv1.GetStmtResponse{Stmt: summ, Items: []*stmtv1.StmtItem{{Ordinal: 2, Reason: "no admissible identifier", Row: rowMsg}}},
		},
		{
			name: "with keys", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run, Rejected: 1}, readsKeys: true, keys: keys, latest: latest,
			want: &stmtv1.GetStmtResponse{Stmt: summ, Keys: keyMsgs},
		},
		{
			name: "with transactions", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run, Rejected: 1}, readsKeys: true, txns: txns,
			want: &stmtv1.GetStmtResponse{Stmt: summ, Txns: txnMsgs},
		},
		{name: "transactions failure", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run}, readsKeys: true, txnsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "ticker validity failure", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run}, readsKeys: true, txns: txns, assertionsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "not found", id: runID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: runID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "items failure", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run}, itemsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unreadable item", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run}, items: []gen.StmtItem{{Ordinal: 0, Stated: []byte("{")}}, wantCode: connect.CodeInternal},
		{name: "keys failure", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run}, readsKeys: true, keysErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "resolutions failure", id: runID.String(), row: gen.GetStmtRow{Stmt: stmtRow, Run: run}, readsKeys: true, keys: keys, latestErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "malformed id", id: "not-a-uuid", wantCode: connect.CodeInvalidArgument},
		{name: "unauthenticated", id: runID.String(), authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.authErr == nil && tc.wantCode != connect.CodeInvalidArgument {
				f.reader.EXPECT().GetStmt(gomock.Any(), gen.GetStmtParams{ID: runID, UserID: userID}).Return(tc.row, tc.err)
				if tc.err == nil {
					f.reader.EXPECT().ListStmtItems(gomock.Any(), gen.ListStmtItemsParams{StmtID: runID, UserID: userID}).Return(tc.items, tc.itemsErr)
				}
				if tc.readsKeys {
					f.reader.EXPECT().ListStatedKeys(gomock.Any(), gen.ListStatedKeysParams{StmtID: runID, UserID: userID}).Return(tc.keys, tc.keysErr)
				}
				if tc.readsKeys && tc.keysErr == nil && len(tc.keys) > 0 {
					f.reader.EXPECT().ListLatestResolutions(gomock.Any(), gen.ListLatestResolutionsParams{Ids: []uuid.UUID{matchedID, unrecognisedID, unreachedID}, UserID: userID}).Return(tc.latest, tc.latestErr)
				}
				if tc.readsKeys && tc.keysErr == nil && tc.latestErr == nil {
					f.reader.EXPECT().ListStmtTxns(gomock.Any(), gen.ListStmtTxnsParams{StmtID: runID, UserID: userID}).Return(tc.txns, tc.txnsErr)
				}
				if len(tc.txns) > 0 {
					f.reader.EXPECT().ListTickersByID(gomock.Any(), []uuid.UUID{meta.ID}).Return([]gen.Ticker{meta}, nil)
					f.reader.EXPECT().ListIdentifierAssertions(gomock.Any(), gen.ListIdentifierAssertionsParams{Markets: []string{"US"}, Values: []string{"META"}}).Return(assertions, tc.assertionsErr)
					if tc.assertionsErr == nil {
						f.reader.EXPECT().ListIdentifierPeriodAssertions(gomock.Any(), gen.ListIdentifierPeriodAssertionsParams{Markets: []string{"US"}, Values: []string{"META"}}).Return(periods, nil)
					}
				}
			}
			res, err := f.client.GetStmt(context.Background(), connect.NewRequest(&stmtv1.GetStmtRequest{RunId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("GetStmt(%q) code = %v (err %v), want %v", tc.id, connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("GetStmt(%q) mismatch (-want +got):\n%s", tc.id, diff)
			}
		})
	}
}

func TestFindCandidates(t *testing.T) {
	keyID := uuid.MustParse("00000000-0000-0000-0000-000000000031")
	usd := "USD"
	offer := resolve.Offer{
		Candidates: []resolve.Candidate{{
			Datasource: "openfigi", Attached: []string{"massive", "eodhd"}, Strongest: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US4581401001"},
			Instr: types.Instr{Class: types.AssetClassStock, Identifiers: []types.Identifier{{Type: types.IdentifierTypeIsin, Value: "US4581401001"}}, Listings: map[string]types.Listing{"USD": {Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "INTC"}}}, "": {Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "INTC"}}}}},
		}},
		Reasons: []string{"eodhd: failed: paused"},
	}
	isinMsg := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US4581401001"}
	want := &stmtv1.FindCandidatesResponse{
		Candidates: []*stmtv1.Candidate{{
			Datasource: "openfigi", Attached: []string{"massive", "eodhd"}, Strongest: isinMsg, AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK, Identifiers: []*typev1.Identifier{isinMsg},
			Listings: []*stmtv1.CandidateListing{
				{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XLON", Value: "INTC"}}},
				{Currency: &usd, Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNAS", Value: "INTC"}}},
			},
		}},
		Reasons: []string{"eodhd: failed: paused"},
	}
	tests := []struct {
		name     string
		id       string
		authErr  error
		called   bool
		offer    resolve.Offer
		err      error
		want     *stmtv1.FindCandidatesResponse
		wantCode connect.Code
	}{
		{name: "listed", id: keyID.String(), called: true, offer: offer, want: want},
		{name: "not found", id: keyID.String(), called: true, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "associated", id: keyID.String(), called: true, err: stmt.ErrAssociated, wantCode: connect.CodeFailedPrecondition},
		{name: "failure", id: keyID.String(), called: true, err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "malformed id", id: "not-a-uuid", wantCode: connect.CodeInvalidArgument},
		{name: "unauthenticated", id: keyID.String(), authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.called {
				f.confirmer.EXPECT().Candidates(gomock.Any(), userID, keyID).Return(tc.offer, tc.err)
			}
			res, err := f.client.FindCandidates(context.Background(), connect.NewRequest(&stmtv1.FindCandidatesRequest{StatedKeyId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("FindCandidates(%q) code = %v (err %v), want %v", tc.id, connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("FindCandidates() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestConfirmCandidate(t *testing.T) {
	keyID := uuid.MustParse("00000000-0000-0000-0000-000000000031")
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US4581401001"}
	isinMsg := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US4581401001"}
	pick := resolve.Pick{Datasource: "openfigi", Identifier: isin}
	key := gen.StatedKey{ID: keyID, UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Value: "INTC"}}, Arbiter: ptr.To(gen.ArbiterUser)}
	rk := gen.ResolutionKey{StatedKeyID: keyID, Outcome: gen.ResolutionOutcomeMatched}
	good := &stmtv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Datasource: "openfigi", Identifier: isinMsg}
	want := &stmtv1.ConfirmCandidateResponse{Key: &typev1.ResolutionItem{
		StatedKey:   &typev1.StatedKey{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Value: "INTC"}}},
		StatedKeyId: keyID.String(), Outcome: typev1.ResolutionOutcome_RESOLUTION_OUTCOME_MATCHED, Arbiter: typev1.Arbiter_ARBITER_USER,
	}}
	tests := []struct {
		name     string
		req      *stmtv1.ConfirmCandidateRequest
		authErr  error
		called   bool
		err      error
		readErr  error
		want     *stmtv1.ConfirmCandidateResponse
		wantCode connect.Code
	}{
		{name: "confirmed", req: good, called: true, want: want},
		{name: "not found", req: good, called: true, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "associated", req: good, called: true, err: stmt.ErrAssociated, wantCode: connect.CodeFailedPrecondition},
		{name: "the answers no longer offer the candidate", req: good, called: true, err: fmt.Errorf("%w: isin US4581401001 names no candidate of openfigi", resolve.ErrNoCandidate), wantCode: connect.CodeFailedPrecondition},
		{name: "failure", req: good, called: true, err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "the key cannot be read back", req: good, called: true, readErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "no datasource", req: &stmtv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Identifier: isinMsg}, wantCode: connect.CodeInvalidArgument},
		{name: "no identifier", req: &stmtv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Datasource: "openfigi"}, wantCode: connect.CodeInvalidArgument},
		{name: "identifier without a type", req: &stmtv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Datasource: "openfigi", Identifier: &typev1.Identifier{Value: "x"}}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed id", req: &stmtv1.ConfirmCandidateRequest{StatedKeyId: "not-a-uuid", Datasource: "openfigi", Identifier: isinMsg}, wantCode: connect.CodeInvalidArgument},
		{name: "unauthenticated", req: good, authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.called {
				f.confirmer.EXPECT().Confirm(gomock.Any(), userID, keyID, pick).Return(rk, tc.err)
				if tc.err == nil {
					f.reader.EXPECT().GetStatedKey(gomock.Any(), gen.GetStatedKeyParams{ID: keyID, UserID: userID}).Return(key, tc.readErr)
				}
			}
			res, err := f.client.ConfirmCandidate(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ConfirmCandidate() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				if tc.wantCode == connect.CodeFailedPrecondition && tc.err != nil && !strings.Contains(err.Error(), tc.err.Error()) {
					t.Errorf("ConfirmCandidate() error = %q, want it to carry %q", err, tc.err)
				}
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ConfirmCandidate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
