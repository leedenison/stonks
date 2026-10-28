package coverage

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	admincoveragev1 "github.com/leedenison/stonks/proto/admin/coverage/v1"
	"github.com/leedenison/stonks/proto/admin/coverage/v1/admincoveragev1connect"
	adminrunv1 "github.com/leedenison/stonks/proto/admin/run/v1"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/attestidentifier"
	reader "github.com/leedenison/stonks/server/internal/identity/coverage"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/coverage/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	acme        = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	assertionID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	edgeID      = uuid.MustParse("00000000-0000-0000-0000-000000000003")
	today       = time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	acmeTicker  = types.MarketTicker{Market: "US", Value: "ACME"}
	acmeProto   = &typev1.MarketTicker{Market: "US", Value: "ACME"}
)

func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

type fixture struct {
	reader   *mock.MockReader
	attester *mock.MockAttester
	client   admincoveragev1connect.CoverageServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{reader: mock.NewMockReader(ctrl), attester: mock.NewMockAttester(ctrl)}
	opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(admincoveragev1connect.NewCoverageServiceHandler(New(f.reader, f.attester, func() time.Time { return today }), opts...))
	})
	f.client = admincoveragev1connect.NewCoverageServiceClient(srv.Client, srv.URL)
	return f
}

func TestListInstrTickers(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		err      error
		wantCode connect.Code
	}{
		{name: "listed", id: acme.String()},
		{name: "not a uuid", id: "acme", wantCode: connect.CodeInvalidArgument},
		{name: "failure", id: acme.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.id == acme.String() {
				f.reader.EXPECT().InstrTickers(gomock.Any(), acme).Return([]reader.InstrTicker{{Ticker: acmeTicker, Relation: gen.TickerRelationDefunct, Txns: 2, Venues: []string{"XNAS"}}}, tc.err)
			}
			res, err := f.client.ListInstrTickers(context.Background(), connect.NewRequest(&admincoveragev1.ListInstrTickersRequest{InstrId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListInstrTickers code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			want := []*admincoveragev1.InstrTicker{{Ticker: acmeProto, Relation: typev1.TickerRelation_TICKER_RELATION_DEFUNCT, Txns: 2, Venues: []string{"XNAS"}}}
			if diff := cmp.Diff(want, res.Msg.GetTickers(), protocmp.Transform()); diff != "" {
				t.Errorf("ListInstrTickers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetTicker(t *testing.T) {
	from, before := day(10), day(21)
	ticker := &reader.Page{
		Ticker: acmeTicker,
		Days:   []time.Time{day(5)},
		Rows: tickervalidity.Rows{
			Assertions: []gen.ListIdentifierAssertionsRow{{
				IdentifierAssertion: gen.IdentifierAssertion{ID: assertionID, Market: "US", InstrID: &acme, AssertedOn: day(10)}, Value: "ACME",
				AttestedBy: ptr.To("admin@example.com"),
			}, {
				IdentifierAssertion: gen.IdentifierAssertion{ID: edgeID, Market: "US", InstrID: &acme, AssertedOn: day(3), Edge: ptr.To(gen.IdentifierEdgeFirst)}, Value: "ACME",
				Datasource: ptr.To("alpha"),
			}},
			AssertionCoverage: []gen.ListIdentifierAssertionCoverageRow{{Market: "US", CoveredFrom: day(1), CoveredBefore: day(20), Datasource: "alpha"}},
		},
		Intervals: []tickervalidity.Interval{{From: &from, Before: &before, Instr: acme, Validity: tickervalidity.Confirmed}},
		Gaps:      []reader.Gap{{Period: types.Period{From: day(5), Before: day(10)}, Keys: 1, Txns: 1}},
		Names:     map[uuid.UUID]types.Identifier{acme: {Type: types.IdentifierTypeIsin, Value: "US0000000001"}},
	}
	want := &admincoveragev1.Ticker{
		Ticker:  acmeProto,
		TxnDays: []string{"2026-03-05"},
		AssertionCoverage: []*admincoveragev1.AssertionCoverage{{
			Datasource: "alpha", Market: "US",
			CoveredFrom: "2026-03-01", CoveredBefore: "2026-03-20",
		}},
		Assertions: []*adminrunv1.AssertionItem{{
			Id: assertionID.String(), Ticker: acmeProto, AssertedOn: "2026-03-10", InstrId: ptr.To(acme.String()),
			Source: &adminrunv1.AssertionItem_AttestedBy{AttestedBy: "admin@example.com"},
		}, {
			Id: edgeID.String(), Ticker: acmeProto, AssertedOn: "2026-03-03", InstrId: ptr.To(acme.String()),
			Source: &adminrunv1.AssertionItem_Datasource{Datasource: "alpha"}, Edge: ptr.To(adminrunv1.Edge_EDGE_FIRST),
		}},
		Intervals: []*admincoveragev1.Interval{{ValidFrom: ptr.To("2026-03-10"), ValidBefore: ptr.To("2026-03-21"), InstrId: acme.String(), Validity: admincoveragev1.TickerValidity_TICKER_VALIDITY_CONFIRMED}},
		Gaps:      []*admincoveragev1.Gap{{UnconfirmedFrom: "2026-03-05", UnconfirmedBefore: "2026-03-10", Keys: 1, Txns: 1}},
		Instrs: []*admincoveragev1.InstrName{{
			Id: acme.String(), Name: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000001"},
		}},
	}
	tests := []struct {
		name     string
		req      *typev1.MarketTicker
		err      error
		wantCode connect.Code
	}{
		{name: "read", req: acmeProto},
		{name: "missing", wantCode: connect.CodeInvalidArgument},
		{name: "unknown market", req: acmeProto, err: tickers.ErrSpelling, wantCode: connect.CodeInvalidArgument},
		{name: "failure", req: acmeProto, err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.req != nil && tc.req.GetValue() != "" {
				f.reader.EXPECT().Ticker(gomock.Any(), acmeTicker, today).Return(ticker, tc.err)
			}
			res, err := f.client.GetTicker(context.Background(), connect.NewRequest(&admincoveragev1.GetTickerRequest{Ticker: tc.req}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("GetTicker code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(want, res.Msg.GetTicker(), protocmp.Transform()); diff != "" {
				t.Errorf("GetTicker mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestAttestIdentifier(t *testing.T) {
	runID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	started := gen.Run{ID: runID, Kind: gen.RunKindAttestIdentifier, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: today}
	names := &admincoveragev1.AttestIdentifierRequest_InstrId{InstrId: acme.String()}
	tests := []struct {
		name      string
		req       *admincoveragev1.AttestIdentifierRequest
		wantInstr *uuid.UUID
		wantTo    *time.Time
		err       error
		wantCode  connect.Code
	}{
		{name: "an instrument over a range", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10", To: ptr.To("2026-03-20")}, wantInstr: &acme, wantTo: ptr.To(day(20))},
		{name: "nothing on a day", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: &admincoveragev1.AttestIdentifierRequest_Nothing{Nothing: true}, From: "2026-03-10"}},
		{name: "no names", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, From: "2026-03-10"}, wantCode: connect.CodeInvalidArgument},
		{name: "no ticker", req: &admincoveragev1.AttestIdentifierRequest{Names: names, From: "2026-03-10"}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed day", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "10/03/2026"}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed end", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10", To: ptr.To("soon")}, wantCode: connect.CodeInvalidArgument},
		{name: "future", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstr: &acme, err: tickervalidity.ErrFuture, wantCode: connect.CodeFailedPrecondition},
		{name: "range reversed", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstr: &acme, err: attestidentifier.ErrRange, wantCode: connect.CodeFailedPrecondition},
		{name: "misspelled", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstr: &acme, err: tickers.ErrSpelling, wantCode: connect.CodeInvalidArgument},
		{name: "unknown instrument", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstr: &acme, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", req: &admincoveragev1.AttestIdentifierRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstr: &acme, err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode == 0 || tc.err != nil {
				f.attester.EXPECT().Start(gomock.Any(), servicetest.Admin.User.ID, acmeTicker, tc.wantInstr, day(10), tc.wantTo).Return(started, tc.err)
			}
			res, err := f.client.AttestIdentifier(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("AttestIdentifier code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != runID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_ATTEST_IDENTIFIER || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("AttestIdentifier = %v, want the pending attest identifier run", got)
			}
		})
	}
}

func TestWithdrawIdentifierAttestation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		err      error
		wantCode connect.Code
	}{
		{name: "withdrawn", id: assertionID.String()},
		{name: "malformed id", id: "first", wantCode: connect.CodeInvalidArgument},
		{name: "a datasource's", id: assertionID.String(), err: attestidentifier.ErrNotAttested, wantCode: connect.CodeFailedPrecondition},
		{name: "missing", id: assertionID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: assertionID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode == 0 || tc.err != nil {
				f.attester.EXPECT().Withdraw(gomock.Any(), assertionID).Return(tc.err)
			}
			_, err := f.client.WithdrawIdentifierAttestation(context.Background(), connect.NewRequest(&admincoveragev1.WithdrawIdentifierAttestationRequest{AssertionId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("WithdrawIdentifierAttestation code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
