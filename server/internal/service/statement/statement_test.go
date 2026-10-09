package statement

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
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/encoding/protojson"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	runv1 "github.com/leedenison/stonks/proto/run/v1"
	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/proto/statement/v1/statementv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/resolve"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/service/statement/mock"
	"github.com/leedenison/stonks/server/internal/statement"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	runID     = uuid.MustParse("00000000-0000-0000-0000-000000000010")
	created   = time.Date(2026, 9, 14, 12, 0, 0, 0, time.UTC)
	from      = time.Date(2026, 3, 1, 0, 0, 0, 0, time.UTC)
	before    = time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC)
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: servicetest.Session}
	run       = gen.Run{ID: runID, UserID: userID, Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser, State: gen.RunStatePending, CreatedAt: created}
	runMsg    = &runv1.Run{Id: runID.String(), Kind: runv1.RunKind_RUN_KIND_STATEMENT, Trigger: runv1.RunTrigger_RUN_TRIGGER_USER, State: runv1.RunState_RUN_STATE_PENDING, CreatedAt: timestamppb.New(created)}
	stmt      = gen.Statement{ID: runID, UserID: userID, Broker: gen.BrokerIbkr, OrderFrom: from, OrderBefore: before, RowCount: 3, CreatedAt: created}
	summ      = &statementv1.StatementSummary{Run: runMsg, Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01", Rows: 3, Rejected: 1}
)

type fixture struct {
	ingester  *mock.MockIngester
	confirmer *mock.MockConfirmer
	reader    *mock.MockReader
	authn     *servicemock.MockAuthenticator
	client    statementv1connect.StatementServiceClient
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
		mux.Handle(statementv1connect.NewStatementServiceHandler(New(f.ingester, f.confirmer, f.reader), opts...))
	})
	f.client = statementv1connect.NewStatementServiceClient(srv.Client, srv.URL)
	return f
}

func statementMsg() *statementv1.Statement {
	return &statementv1.Statement{Broker: typev1.Broker_BROKER_IBKR, OrderFrom: "2026-03-01", OrderBefore: "2026-04-01"}
}

// splitMsg returns a split of key that passes every check but those on the key.
func splitMsg(key *typev1.StatedKey) *statementv1.StatedSplit {
	return &statementv1.StatedSplit{Key: key, EffectiveDate: "2026-03-05", Quantity: "10"}
}

func TestCreateStatement(t *testing.T) {
	tests := []struct {
		name     string
		msg      *statementv1.Statement
		edit     func(*statementv1.Statement)
		authErr  error
		err      error
		called   bool
		wantCode connect.Code
	}{
		{name: "created", called: true},
		{name: "empty period", edit: func(m *statementv1.Statement) { m.OrderBefore = m.OrderFrom }, wantCode: connect.CodeInvalidArgument},
		{name: "no broker", edit: func(m *statementv1.Statement) { m.Broker = typev1.Broker_BROKER_UNSPECIFIED }, wantCode: connect.CodeInvalidArgument},
		{name: "malformed split", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{{Key: &typev1.StatedKey{}, EffectiveDate: "2026-03-05", Quantity: "ten"}}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split currency not a code", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{splitMsg(&typev1.StatedKey{Currency: ptr.To("usd")})}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split identifier without a type", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{splitMsg(&typev1.StatedKey{Identifiers: []*typev1.Identifier{{Value: "ACME"}}})}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split identifier without a value", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{splitMsg(&typev1.StatedKey{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN}}})}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "split key with a currency and identifiers", edit: func(m *statementv1.Statement) {
			m.Splits = []*statementv1.StatedSplit{splitMsg(&typev1.StatedKey{Currency: ptr.To("USD"), Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000001"}}})}
		}, called: true},
		{name: "too many rows", edit: func(m *statementv1.Statement) {
			m.Rows = make([]*statementv1.Row, 50001)
			for i := range m.Rows {
				m.Rows[i] = &statementv1.Row{}
			}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "too many splits", edit: func(m *statementv1.Statement) {
			m.Splits = make([]*statementv1.StatedSplit, 1001)
			for i := range m.Splits {
				m.Splits[i] = splitMsg(&typev1.StatedKey{})
			}
		}, wantCode: connect.CodeInvalidArgument},
		{name: "unreadable", err: fmt.Errorf("%w: split 0: no key", statement.ErrInvalid), called: true, wantCode: connect.CodeInvalidArgument},
		{name: "failure", err: errors.New("boom"), called: true, wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			msg := statementMsg()
			if tc.edit != nil {
				tc.edit(msg)
			}
			if tc.called {
				f.ingester.EXPECT().Create(gomock.Any(), userID, gomock.Any()).DoAndReturn(func(_ context.Context, _ uuid.UUID, got *statementv1.Statement) (gen.Run, error) {
					if diff := cmp.Diff(msg, got, protocmp.Transform()); diff != "" {
						t.Errorf("Create statement mismatch (-want +got):\n%s", diff)
					}
					return run, tc.err
				})
			}
			res, err := f.client.CreateStatement(context.Background(), connect.NewRequest(&statementv1.CreateStatementRequest{Statement: msg}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("CreateStatement code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(&statementv1.CreateStatementResponse{Run: runMsg}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("CreateStatement mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestListStatements(t *testing.T) {
	tests := []struct {
		name     string
		authErr  error
		rows     []gen.ListStatementsRow
		err      error
		want     *statementv1.ListStatementsResponse
		wantCode connect.Code
	}{
		{name: "none", want: &statementv1.ListStatementsResponse{}},
		{name: "one", rows: []gen.ListStatementsRow{{Statement: stmt, Run: run, Rejected: 1}}, want: &statementv1.ListStatementsResponse{Statements: []*statementv1.StatementSummary{summ}}},
		{name: "failure", err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.authErr == nil {
				f.reader.EXPECT().ListStatements(gomock.Any(), userID).Return(tc.rows, tc.err)
			}
			res, err := f.client.ListStatements(context.Background(), connect.NewRequest(&statementv1.ListStatementsRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListStatements code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListStatements mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetStatement(t *testing.T) {
	acme := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ACME"}
	rowMsg := &statementv1.Row{Key: &typev1.StatedKey{Identifiers: []*typev1.Identifier{acme}}, OrderDate: "2026-03-05", SettlementDate: "2026-03-07", AsAt: "2026-03-05", Quantity: "10"}
	stated, err := protojson.Marshal(rowMsg)
	if err != nil {
		t.Fatal(err)
	}
	items := []gen.StatementItem{{StatementID: runID, UserID: userID, Ordinal: 2, Reason: "no admissible identifier", Stated: stated}}
	// Three keys the statement stated: one matched by its latest resolution
	// after an earlier one left it unavailable, one left unrecognised, and
	// one no resolution has reached.
	matchedID, unrecognisedID, unreachedID := uuid.MustParse("00000000-0000-0000-0000-000000000021"), uuid.MustParse("00000000-0000-0000-0000-000000000022"), uuid.MustParse("00000000-0000-0000-0000-000000000023")
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0378331005"}
	keys := []gen.StatedKey{
		{ID: matchedID, StatementID: runID, UserID: userID, Identifiers: []types.Identifier{isin}, Arbiter: ptr.To(gen.ArbiterUser)},
		{ID: unrecognisedID, StatementID: runID, UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME"}}},
		{ID: unreachedID, StatementID: runID, UserID: userID, Currency: ptr.To("GBP"), Identifiers: []types.Identifier{}},
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
	tests := []struct {
		name      string
		id        string
		authErr   error
		row       gen.GetStatementRow
		err       error
		items     []gen.StatementItem
		itemsErr  error
		readsKeys bool
		keys      []gen.StatedKey
		keysErr   error
		latest    []gen.ResolutionKey
		latestErr error
		want      *statementv1.GetStatementResponse
		wantCode  connect.Code
	}{
		{
			name: "with items", id: runID.String(), row: gen.GetStatementRow{Statement: stmt, Run: run, Rejected: 1}, items: items, readsKeys: true,
			want: &statementv1.GetStatementResponse{Statement: summ, Items: []*statementv1.StatementItem{{Ordinal: 2, Reason: "no admissible identifier", Row: rowMsg}}},
		},
		{
			name: "with keys", id: runID.String(), row: gen.GetStatementRow{Statement: stmt, Run: run, Rejected: 1}, readsKeys: true, keys: keys, latest: latest,
			want: &statementv1.GetStatementResponse{Statement: summ, Keys: keyMsgs},
		},
		{name: "not found", id: runID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: runID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "items failure", id: runID.String(), row: gen.GetStatementRow{Statement: stmt, Run: run}, itemsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unreadable item", id: runID.String(), row: gen.GetStatementRow{Statement: stmt, Run: run}, items: []gen.StatementItem{{Ordinal: 0, Stated: []byte("{")}}, wantCode: connect.CodeInternal},
		{name: "keys failure", id: runID.String(), row: gen.GetStatementRow{Statement: stmt, Run: run}, readsKeys: true, keysErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "resolutions failure", id: runID.String(), row: gen.GetStatementRow{Statement: stmt, Run: run}, readsKeys: true, keys: keys, latestErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "malformed id", id: "not-a-uuid", wantCode: connect.CodeInvalidArgument},
		{name: "unauthenticated", id: runID.String(), authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.authErr == nil && tc.wantCode != connect.CodeInvalidArgument {
				f.reader.EXPECT().GetStatement(gomock.Any(), gen.GetStatementParams{ID: runID, UserID: userID}).Return(tc.row, tc.err)
				if tc.err == nil {
					f.reader.EXPECT().ListStatementItems(gomock.Any(), gen.ListStatementItemsParams{StatementID: runID, UserID: userID}).Return(tc.items, tc.itemsErr)
				}
				if tc.readsKeys {
					f.reader.EXPECT().ListStatedKeys(gomock.Any(), gen.ListStatedKeysParams{StatementID: runID, UserID: userID}).Return(tc.keys, tc.keysErr)
				}
				if tc.readsKeys && tc.keysErr == nil && len(tc.keys) > 0 {
					f.reader.EXPECT().ListLatestResolutions(gomock.Any(), gen.ListLatestResolutionsParams{Ids: []uuid.UUID{matchedID, unrecognisedID, unreachedID}, UserID: userID}).Return(tc.latest, tc.latestErr)
				}
			}
			res, err := f.client.GetStatement(context.Background(), connect.NewRequest(&statementv1.GetStatementRequest{RunId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("GetStatement(%q) code = %v (err %v), want %v", tc.id, connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("GetStatement(%q) mismatch (-want +got):\n%s", tc.id, diff)
			}
		})
	}
}

func TestFindCandidates(t *testing.T) {
	keyID := uuid.MustParse("00000000-0000-0000-0000-000000000031")
	usd := "USD"
	offer := resolve.Offer{
		Candidates: []resolve.Candidate{{
			Datasource: "openfigi", Strongest: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US4581401001"}, AssetClass: gen.AssetClassStock,
			Identifiers: []types.Identifier{{Type: types.IdentifierTypeIsin, Value: "US4581401001"}},
			Listings:    []resolve.CandidateListing{{Currency: &usd, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "INTC"}}}, {Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "INTC"}}}},
		}},
		Reasons: []string{"eodhd: failed: paused"},
	}
	isinMsg := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US4581401001"}
	want := &statementv1.FindCandidatesResponse{
		Candidates: []*statementv1.Candidate{{
			Datasource: "openfigi", Strongest: isinMsg, AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK, Identifiers: []*typev1.Identifier{isinMsg},
			Listings: []*statementv1.CandidateListing{
				{Currency: &usd, Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNAS", Value: "INTC"}}},
				{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XLON", Value: "INTC"}}},
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
		want     *statementv1.FindCandidatesResponse
		wantCode connect.Code
	}{
		{name: "listed", id: keyID.String(), called: true, offer: offer, want: want},
		{name: "not found", id: keyID.String(), called: true, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "associated", id: keyID.String(), called: true, err: statement.ErrAssociated, wantCode: connect.CodeFailedPrecondition},
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
			res, err := f.client.FindCandidates(context.Background(), connect.NewRequest(&statementv1.FindCandidatesRequest{StatedKeyId: tc.id}))
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
	key := gen.StatedKey{ID: keyID, StatementID: runID, UserID: userID, Identifiers: []types.Identifier{{Type: types.IdentifierTypeMicTicker, Value: "INTC"}}, Arbiter: ptr.To(gen.ArbiterUser)}
	rk := gen.ResolutionKey{StatedKeyID: keyID, Outcome: gen.ResolutionOutcomeMatched}
	good := &statementv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Datasource: "openfigi", Identifier: isinMsg}
	want := &statementv1.ConfirmCandidateResponse{Key: &typev1.ResolutionItem{
		StatedKey:   &typev1.StatedKey{Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Value: "INTC"}}},
		StatedKeyId: keyID.String(), Outcome: typev1.ResolutionOutcome_RESOLUTION_OUTCOME_MATCHED, Arbiter: typev1.Arbiter_ARBITER_USER,
	}}
	tests := []struct {
		name     string
		req      *statementv1.ConfirmCandidateRequest
		authErr  error
		called   bool
		err      error
		readErr  error
		want     *statementv1.ConfirmCandidateResponse
		wantCode connect.Code
	}{
		{name: "confirmed", req: good, called: true, want: want},
		{name: "not found", req: good, called: true, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "associated", req: good, called: true, err: statement.ErrAssociated, wantCode: connect.CodeFailedPrecondition},
		{name: "the answer lacks the candidate", req: good, called: true, err: fmt.Errorf("%w: isin US4581401001 names no candidate of openfigi", resolve.ErrNoCandidate), wantCode: connect.CodeFailedPrecondition},
		{name: "failure", req: good, called: true, err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "the key cannot be read back", req: good, called: true, readErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "no datasource", req: &statementv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Identifier: isinMsg}, wantCode: connect.CodeInvalidArgument},
		{name: "no identifier", req: &statementv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Datasource: "openfigi"}, wantCode: connect.CodeInvalidArgument},
		{name: "identifier without a type", req: &statementv1.ConfirmCandidateRequest{StatedKeyId: keyID.String(), Datasource: "openfigi", Identifier: &typev1.Identifier{Value: "x"}}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed id", req: &statementv1.ConfirmCandidateRequest{StatedKeyId: "not-a-uuid", Datasource: "openfigi", Identifier: isinMsg}, wantCode: connect.CodeInvalidArgument},
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
