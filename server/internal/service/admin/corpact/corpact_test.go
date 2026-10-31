package corpact

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	admincorpactv1 "github.com/leedenison/stonks/proto/admin/corpact/v1"
	"github.com/leedenison/stonks/proto/admin/corpact/v1/admincorpactv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/corpact/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	instrID = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	actID   = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	spun    = time.Date(2024, 4, 2, 0, 0, 0, 0, time.UTC)
)

type fixture struct {
	queries *mock.MockQueries
	client  admincorpactv1connect.CorpActServiceClient
}

// newFixture returns an administrator's client for a Server behind the real
// handler chain.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicetest.AsAdmin(ctrl)
	f := &fixture{queries: mock.NewMockQueries(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(admincorpactv1connect.NewCorpActServiceHandler(New(f.queries), opts...))
	})
	f.client = admincorpactv1connect.NewCorpActServiceClient(srv.Client, srv.URL)
	return f
}

// row is a stored action of datasource for US:GE on spun, from units
// becoming to units, with the decision d where d is not empty. alpha is the
// source of the US.
func row(id uuid.UUID, datasource string, from, to int64, handled bool, d gen.CorpActDecision) gen.ListInstrCorpActsRow {
	e := gen.CorpAct{ID: id, ExDate: spun, RatioFrom: decimal.NewFromInt(from), RatioTo: decimal.NewFromInt(to), Handled: handled, Type: ptr.To("forward_split")}
	if d != "" {
		e.Decision = &d
	}
	return gen.ListInstrCorpActsRow{CorpAct: e, Datasource: datasource, Market: "US", Value: "GE", Source: datasource == "alpha"}
}

// msg is the message of row, with the effective restates-shares and the
// decision given.
func msg(id uuid.UUID, datasource string, from, to string, restates bool, d admincorpactv1.CorpActDecision, applied bool) *admincorpactv1.CorpAct {
	return &admincorpactv1.CorpAct{
		Id: id.String(), Ticker: &typev1.MarketTicker{Market: "US", Value: "GE"}, ExDate: "2024-04-02",
		Ratio: &typev1.Ratio{From: from, To: to}, Datasource: datasource, Type: ptr.To("forward_split"),
		RestatesShares: restates, Decision: d, Applied: applied,
	}
}

// TestListInstrCorpActs checks the effective restates-shares of a handled, an
// unhandled and a decided action, and that only the market's source is
// applied.
func TestListInstrCorpActs(t *testing.T) {
	f := newFixture(t)
	ids := []uuid.UUID{uuid.New(), uuid.New(), uuid.New(), uuid.New()}
	f.queries.EXPECT().ListInstrCorpActs(gomock.Any(), instrID).Return([]gen.ListInstrCorpActsRow{
		row(ids[0], "alpha", 1, 4, true, ""),
		row(ids[1], "alpha", 1000, 1253, false, ""),
		row(ids[2], "alpha", 1, 4, true, gen.CorpActDecisionSpinOffPriceFactor),
		row(ids[3], "beta", 1, 4, true, ""),
	}, nil)
	f.queries.EXPECT().ListInstrMarkets(gomock.Any(), []uuid.UUID{instrID}).Return([]gen.ListInstrMarketsRow{{InstrID: instrID, Market: "US"}}, nil)

	res, err := f.client.ListInstrCorpActs(context.Background(), connect.NewRequest(&admincorpactv1.ListInstrCorpActsRequest{InstrId: instrID.String()}))
	if err != nil {
		t.Fatalf("ListInstrCorpActs() error = %v", err)
	}
	want := &admincorpactv1.ListInstrCorpActsResponse{CorpActs: []*admincorpactv1.CorpAct{
		msg(ids[0], "alpha", "1", "4", true, admincorpactv1.CorpActDecision_CORP_ACT_DECISION_UNSPECIFIED, true),
		msg(ids[1], "alpha", "1000", "1253", false, admincorpactv1.CorpActDecision_CORP_ACT_DECISION_UNSPECIFIED, true),
		msg(ids[2], "alpha", "1", "4", false, admincorpactv1.CorpActDecision_CORP_ACT_DECISION_SPIN_OFF_PRICE_FACTOR, true),
		msg(ids[3], "beta", "1", "4", true, admincorpactv1.CorpActDecision_CORP_ACT_DECISION_UNSPECIFIED, false),
	}}
	if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
		t.Errorf("ListInstrCorpActs() mismatch (-want +got):\n%s", diff)
	}
}

// TestGetCorpAct checks that an action of a ticker that named nothing on
// its ex-date comes back alone, and that a missing action is not found.
func TestGetCorpAct(t *testing.T) {
	t.Run("names nothing", func(t *testing.T) {
		f := newFixture(t)
		r := row(actID, "alpha", 1000, 1253, false, "")
		f.queries.EXPECT().GetCorpAct(gomock.Any(), actID).Return(gen.GetCorpActRow{CorpAct: r.CorpAct, Datasource: r.Datasource, Market: r.Market, Value: r.Value}, nil)
		f.queries.EXPECT().ListTickerValidity(gomock.Any(), gomock.Any()).Return(nil, nil)
		res, err := f.client.GetCorpAct(context.Background(), connect.NewRequest(&admincorpactv1.GetCorpActRequest{CorpActId: actID.String()}))
		if err != nil {
			t.Fatalf("GetCorpAct() error = %v", err)
		}
		want := &admincorpactv1.GetCorpActResponse{CorpAct: msg(actID, "alpha", "1000", "1253", false, admincorpactv1.CorpActDecision_CORP_ACT_DECISION_UNSPECIFIED, false)}
		if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
			t.Errorf("GetCorpAct() mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("not found", func(t *testing.T) {
		f := newFixture(t)
		f.queries.EXPECT().GetCorpAct(gomock.Any(), actID).Return(gen.GetCorpActRow{}, db.ErrNotFound)
		_, err := f.client.GetCorpAct(context.Background(), connect.NewRequest(&admincorpactv1.GetCorpActRequest{CorpActId: actID.String()}))
		if servicetest.CodeOf(err) != connect.CodeNotFound {
			t.Errorf("GetCorpAct() code = %v (err %v), want NotFound", servicetest.CodeOf(err), err)
		}
	})
}

func TestDecideCorpAct(t *testing.T) {
	tests := []struct {
		name     string
		decision admincorpactv1.CorpActDecision
		// stored is the decision the store records; empty where none is.
		stored   gen.CorpActDecision
		err      error
		wantCode connect.Code
	}{
		{name: "decided", decision: admincorpactv1.CorpActDecision_CORP_ACT_DECISION_SPLIT, stored: gen.CorpActDecisionSplit},
		{name: "not found", decision: admincorpactv1.CorpActDecision_CORP_ACT_DECISION_SPLIT, stored: gen.CorpActDecisionSplit, err: db.ErrNotFound, wantCode: connect.CodeNotFound},
		{name: "failure", decision: admincorpactv1.CorpActDecision_CORP_ACT_DECISION_OTHER, stored: gen.CorpActDecisionOther, err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unspecified", decision: admincorpactv1.CorpActDecision_CORP_ACT_DECISION_UNSPECIFIED, wantCode: connect.CodeInvalidArgument},
		{name: "undefined", decision: admincorpactv1.CorpActDecision(99), wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.stored != "" {
				f.queries.EXPECT().DecideCorpAct(gomock.Any(), gen.DecideCorpActParams{ID: actID, Decision: tc.stored}).Return(actID, tc.err)
			}
			_, err := f.client.DecideCorpAct(context.Background(), connect.NewRequest(&admincorpactv1.DecideCorpActRequest{CorpActId: actID.String(), Decision: tc.decision}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("DecideCorpAct() code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
