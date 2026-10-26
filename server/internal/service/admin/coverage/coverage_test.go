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
	"github.com/leedenison/stonks/server/internal/attest"
	reader "github.com/leedenison/stonks/server/internal/coverage"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/coverage/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

var (
	acme        = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	assertionID = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	today       = time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	acmeTicker  = types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNAS", Value: "ACME"}
	acmeProto   = &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNAS", Value: "ACME"}
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

func TestListInstrumentTickers(t *testing.T) {
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
				f.reader.EXPECT().InstrumentTickers(gomock.Any(), acme).Return([]gen.ListInstrumentTickersRow{{Domain: "XNAS", Value: "ACME", Current: true, Transactions: 2}}, tc.err)
			}
			res, err := f.client.ListInstrumentTickers(context.Background(), connect.NewRequest(&admincoveragev1.ListInstrumentTickersRequest{InstrumentId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListInstrumentTickers code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			want := []*admincoveragev1.InstrumentTicker{{Ticker: acmeProto, Current: true, Transactions: 2}}
			if diff := cmp.Diff(want, res.Msg.GetTickers(), protocmp.Transform()); diff != "" {
				t.Errorf("ListInstrumentTickers mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestGetTicker(t *testing.T) {
	from, before := day(10), day(21)
	ticker := &reader.Ticker{
		Identifier: acmeTicker,
		Days:       []time.Time{day(5)},
		Rows: validity.Rows{
			Assertions: []gen.ListIdentifierAssertionsRow{{
				IdentifierAssertion: gen.IdentifierAssertion{ID: assertionID, Type: acmeTicker.Type, Domain: "XNAS", Value: "ACME", InstrumentID: &acme, AssertedOn: day(10)},
				AttestedBy:          ptr.To("admin@example.com"),
			}},
			Events:   []gen.ListIdentifierEventsRow{{Value: "OLD", Action: gen.IdentifierEventActionRenamed, ToValue: ptr.To("ACME"), EffectiveOn: day(3)}},
			Coverage: []gen.ListIdentifierCoverageRow{{Domain: "XNAS", CoveredFrom: day(1), CoveredBefore: day(20), Datasource: "alpha"}},
		},
		Intervals: []validity.Interval{{From: &from, Before: &before, Instrument: acme, Validity: validity.Confirmed}},
		Gaps:      []reader.Gap{{Ticker: acmeTicker, Period: types.Period{From: day(5), Before: day(10)}, Keys: 1, Transactions: 1}},
		Names:     map[uuid.UUID]types.Identifier{acme: {Type: types.IdentifierTypeIsin, Value: "US0000000001"}},
	}
	want := &admincoveragev1.Ticker{
		Ticker:          acmeProto,
		TransactionDays: []string{"2026-03-05"},
		Coverage: []*admincoveragev1.Coverage{{
			Datasource: "alpha", Covers: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNAS"},
			CoveredFrom: "2026-03-01", CoveredBefore: "2026-03-20",
		}},
		Assertions: []*adminrunv1.AssertionItem{{
			Id: assertionID.String(), Ticker: acmeProto, AssertedOn: "2026-03-10", InstrumentId: ptr.To(acme.String()),
			Source: &adminrunv1.AssertionItem_AttestedBy{AttestedBy: "admin@example.com"},
		}},
		Events:    []*admincoveragev1.IdentifierEvent{{EffectiveOn: "2026-03-03", Action: admincoveragev1.IdentifierEventAction_IDENTIFIER_EVENT_ACTION_RENAMED, Value: "OLD", ToValue: "ACME"}},
		Intervals: []*admincoveragev1.Interval{{ValidFrom: ptr.To("2026-03-10"), ValidBefore: ptr.To("2026-03-21"), InstrumentId: acme.String(), Validity: admincoveragev1.Validity_VALIDITY_CONFIRMED}},
		Gaps:      []*admincoveragev1.Gap{{Ticker: acmeProto, UnconfirmedFrom: "2026-03-05", UnconfirmedBefore: "2026-03-10", Keys: 1, Transactions: 1}},
		Instruments: []*admincoveragev1.InstrumentName{{
			Id: acme.String(), Name: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000001"},
		}},
	}
	tests := []struct {
		name     string
		req      *typev1.Identifier
		err      error
		wantCode connect.Code
	}{
		{name: "read", req: acmeProto},
		{name: "missing", wantCode: connect.CodeInvalidArgument},
		{name: "not a ticker", req: acmeProto, err: validity.ErrNotTicker, wantCode: connect.CodeInvalidArgument},
		{name: "domain", req: acmeProto, err: validity.ErrSpelling, wantCode: connect.CodeInvalidArgument},
		{name: "failure", req: acmeProto, err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.req != nil {
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

func TestAttest(t *testing.T) {
	runID := uuid.MustParse("00000000-0000-0000-0000-000000000010")
	started := gen.Run{ID: runID, Kind: gen.RunKindAttestation, Trigger: gen.RunTriggerAdministrator, State: gen.RunStatePending, CreatedAt: today}
	names := &admincoveragev1.AttestRequest_InstrumentId{InstrumentId: acme.String()}
	tests := []struct {
		name           string
		req            *admincoveragev1.AttestRequest
		wantInstrument *uuid.UUID
		wantTo         *time.Time
		err            error
		wantCode       connect.Code
	}{
		{name: "an instrument over a range", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10", To: ptr.To("2026-03-20")}, wantInstrument: &acme, wantTo: ptr.To(day(20))},
		{name: "nothing on a day", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: &admincoveragev1.AttestRequest_Nothing{Nothing: true}, From: "2026-03-10"}},
		{name: "no names", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, From: "2026-03-10"}, wantCode: connect.CodeInvalidArgument},
		{name: "no ticker", req: &admincoveragev1.AttestRequest{Names: names, From: "2026-03-10"}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed day", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "10/03/2026"}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed end", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10", To: ptr.To("soon")}, wantCode: connect.CodeInvalidArgument},
		{name: "future", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstrument: &acme, err: validity.ErrFuture, wantCode: connect.CodeFailedPrecondition},
		{name: "range reversed", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstrument: &acme, err: attest.ErrRange, wantCode: connect.CodeFailedPrecondition},
		{name: "misspelled", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstrument: &acme, err: validity.ErrSpelling, wantCode: connect.CodeInvalidArgument},
		{name: "unknown instrument", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstrument: &acme, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", req: &admincoveragev1.AttestRequest{Ticker: acmeProto, Names: names, From: "2026-03-10"}, wantInstrument: &acme, err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode == 0 || tc.err != nil {
				f.attester.EXPECT().Start(gomock.Any(), servicetest.Admin.User.ID, acmeTicker, tc.wantInstrument, day(10), tc.wantTo).Return(started, tc.err)
			}
			res, err := f.client.Attest(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("Attest code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := res.Msg.GetRun()
			if got.GetId() != runID.String() || got.GetKind() != runv1.RunKind_RUN_KIND_ATTESTATION || got.GetState() != runv1.RunState_RUN_STATE_PENDING {
				t.Errorf("Attest = %v, want the pending attestation run", got)
			}
		})
	}
}

func TestWithdrawAttestation(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		err      error
		wantCode connect.Code
	}{
		{name: "withdrawn", id: assertionID.String()},
		{name: "malformed id", id: "first", wantCode: connect.CodeInvalidArgument},
		{name: "a datasource's", id: assertionID.String(), err: attest.ErrNotAttested, wantCode: connect.CodeFailedPrecondition},
		{name: "missing", id: assertionID.String(), err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", id: assertionID.String(), err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantCode == 0 || tc.err != nil {
				f.attester.EXPECT().Withdraw(gomock.Any(), assertionID).Return(tc.err)
			}
			_, err := f.client.WithdrawAttestation(context.Background(), connect.NewRequest(&admincoveragev1.WithdrawAttestationRequest{AssertionId: tc.id}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("WithdrawAttestation code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
