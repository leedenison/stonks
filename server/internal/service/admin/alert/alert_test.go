package alert

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	adminalertv1 "github.com/leedenison/stonks/proto/admin/alert/v1"
	"github.com/leedenison/stonks/proto/admin/alert/v1/adminalertv1connect"
	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/coverage"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/alert/mock"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
)

const (
	failedRun  = adminalertv1.AlertKind_ALERT_KIND_FAILED_RUN
	block      = adminalertv1.AlertKind_ALERT_KIND_BLOCK
	nothing    = adminalertv1.AlertKind_ALERT_KIND_TICKER_NAMES_NOTHING
	gap        = adminalertv1.AlertKind_ALERT_KIND_TICKER_VALIDITY_GAP
	corpAct    = adminalertv1.AlertKind_ALERT_KIND_CORP_ACT
	finding    = adminalertv1.AlertKind_ALERT_KIND_FINDING
	acme       = "00000000-0000-0000-0000-000000000001"
	emailAlpha = "one@example.com"
)

var (
	today   = time.Date(2026, time.March, 30, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

func day(n int) time.Time { return time.Date(2026, time.March, n, 0, 0, 0, 0, time.UTC) }

// id returns the nth fixed uuid, so that a later n is a later id.
func id(n int) uuid.UUID {
	return uuid.MustParse("00000000-0000-0000-0000-0000000001" + string(rune('0'+n/10)) + string(rune('0'+n%10)))
}

func runRow(n int) gen.ListFailedRunsRow {
	return gen.ListFailedRunsRow{Run: gen.Run{ID: id(n), Kind: gen.RunKindIngestStmt, Trigger: gen.RunTriggerUser, State: gen.RunStateFailed, CreatedAt: today}, RunDetail: gen.RunDetail{Email: ptr.To(emailAlpha)}}
}

func blockRow(n int) gen.ListBlocksRow {
	return gen.ListBlocksRow{DatasourceBlock: gen.DatasourceBlock{ID: id(n), Datasource: "alpha", Kind: gen.FetchKindIdentity, Scope: gen.BlockScopeDatasource, Reason: "refused", CreatedAt: today}, FetchID: id(90)}
}

func findingRow(n int, kind gen.FindingKind) gen.ListOpenFindingsRow {
	return gen.ListOpenFindingsRow{Finding: gen.Finding{ID: id(n), RunID: id(90), Kind: kind, Detail: ptr.To("grounds"), CreatedAt: today}}
}

func runAlert(r gen.ListFailedRunsRow) *adminalertv1.Alert {
	return &adminalertv1.Alert{Kind: failedRun, Subject: &adminalertv1.Alert_Run{Run: to.ProtoUserRun(r.Run, r.RunDetail)}}
}

func blockAlert(r gen.ListBlocksRow) *adminalertv1.Alert {
	return &adminalertv1.Alert{Kind: block, Subject: &adminalertv1.Alert_Block{Block: to.ProtoBlock(r)}}
}

func findingAlert(r gen.ListOpenFindingsRow) *adminalertv1.Alert {
	return &adminalertv1.Alert{Kind: finding, Subject: &adminalertv1.Alert_Finding{Finding: to.ProtoFinding(r.Finding, nil)}}
}

// actRow is an unhandled, undecided action of alpha, the source of the US,
// on US:NVDA with an ex-date on day n, where NVDA named acme.
func actRow(n int) gen.ListUnknownCorpActsRow {
	return gen.ListUnknownCorpActsRow{
		CorpAct:    gen.CorpAct{ID: id(n), ExDate: day(n), RatioFrom: decimal.NewFromInt(1), RatioTo: decimal.NewFromInt(4)},
		Datasource: "alpha", Market: "US", Value: "NVDA", Source: true, InstrID: ptr.To(uuid.MustParse(acme)),
	}
}

// actAlert is the applied alert of the row r.
func actAlert(r gen.ListUnknownCorpActsRow) *adminalertv1.Alert {
	a := corpact.InstrAct{
		Act:        corpact.Act{ID: r.CorpAct.ID, ExDate: r.CorpAct.ExDate, Ratio: types.Ratio{From: r.CorpAct.RatioFrom, To: r.CorpAct.RatioTo}},
		Datasource: r.Datasource, Ticker: types.MarketTicker{Market: r.Market, Value: r.Value}, Applied: true,
	}
	return &adminalertv1.Alert{Kind: corpAct, Subject: &adminalertv1.Alert_CorpAct{CorpAct: a.ToProto()}}
}

// markets serves the market of acme as the US.
func markets(f *fixture) {
	f.queries.EXPECT().ListInstrMarkets(gomock.Any(), []uuid.UUID{uuid.MustParse(acme)}).Return([]gen.ListInstrMarketsRow{{InstrID: uuid.MustParse(acme), Market: "US"}}, nil)
}

// tickerGap is a gap on US:value from day n with txns transactions.
func tickerGap(value string, txns, n int) coverage.TickerGap {
	return coverage.TickerGap{
		Ticker: types.MarketTicker{Market: "US", Value: value}, Instr: ptr.To(uuid.MustParse(acme)),
		Name: types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"},
		Gap:  coverage.Gap{Period: types.Period{From: day(n), Before: day(31)}, Keys: 1, Txns: txns},
	}
}

func gapAlert(g coverage.TickerGap) *adminalertv1.Alert {
	return &adminalertv1.Alert{Kind: gap, Subject: &adminalertv1.Alert_ValidityGap{ValidityGap: g.ToProto()}}
}

type fixture struct {
	queries *mock.MockQueries
	reader  *mock.MockReader
	client  adminalertv1connect.AlertServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{queries: mock.NewMockQueries(ctrl), reader: mock.NewMockReader(ctrl)}
	opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminalertv1connect.NewAlertServiceHandler(New(f.queries, f.reader, func() time.Time { return today }), opts...))
	})
	f.client = adminalertv1connect.NewAlertServiceClient(srv.Client, srv.URL)
	return f
}

// TestGetAlertDigest checks that the digest holds every kind in rank order,
// each with its count and at most its top five, counting the finding kinds
// that are alerts and leaving the rest out.
func TestGetAlertDigest(t *testing.T) {
	f := newFixture(t)
	unnamed := []coverage.NamesNothing{{Ticker: types.MarketTicker{Market: "US", Value: "NONE"}, Keys: 1, Txns: 2, First: day(3)}}
	var gaps []coverage.TickerGap
	for i, value := range []string{"A", "B", "C", "D", "E", "F", "G"} {
		gaps = append(gaps, tickerGap(value, i, 10))
	}
	f.reader.EXPECT().Unconfirmed(gomock.Any(), today).Return(unnamed, gaps, nil)
	f.queries.EXPECT().CountOpenFindings(gomock.Any()).Return([]gen.CountOpenFindingsRow{
		{Kind: gen.FindingKindDropped, Count: 9}, {Kind: gen.FindingKindContradiction, Count: 2}, {Kind: gen.FindingKindUnhandledCorpAct, Count: 1},
	}, nil)
	f.queries.EXPECT().CountFailedRuns(gomock.Any()).Return(int32(1), nil)
	f.queries.EXPECT().ListFailedRuns(gomock.Any(), gen.ListFailedRunsParams{Lim: top}).Return([]gen.ListFailedRunsRow{runRow(1)}, nil)
	f.queries.EXPECT().CountOpenBlocks(gomock.Any()).Return(int32(0), nil)
	f.queries.EXPECT().ListBlocks(gomock.Any(), gen.ListBlocksParams{Lim: top}).Return(nil, nil)
	f.queries.EXPECT().CountUnknownCorpActs(gomock.Any()).Return(int32(1), nil)
	f.queries.EXPECT().ListUnknownCorpActs(gomock.Any(), gen.ListUnknownCorpActsParams{Lim: top}).Return([]gen.ListUnknownCorpActsRow{actRow(2)}, nil)
	markets(f)
	f.queries.EXPECT().ListOpenFindings(gomock.Any(), gen.ListOpenFindingsParams{Kinds: findingKinds, Lim: top}).Return([]gen.ListOpenFindingsRow{findingRow(4, gen.FindingKindContradiction), findingRow(3, gen.FindingKindContradiction)}, nil)

	res, err := f.client.GetAlertDigest(context.Background(), connect.NewRequest(&adminalertv1.GetAlertDigestRequest{}))
	if err != nil {
		t.Fatalf("GetAlertDigest: %v", err)
	}
	want := &adminalertv1.GetAlertDigestResponse{Groups: []*adminalertv1.AlertGroup{
		{Kind: failedRun, Count: 1, Top: []*adminalertv1.Alert{runAlert(runRow(1))}},
		{Kind: block},
		{Kind: nothing, Count: 1, Top: []*adminalertv1.Alert{{Kind: nothing, Subject: &adminalertv1.Alert_NamesNothing{NamesNothing: unnamed[0].ToProto()}}}},
		{Kind: gap, Count: 7, Top: []*adminalertv1.Alert{gapAlert(gaps[6]), gapAlert(gaps[5]), gapAlert(gaps[4]), gapAlert(gaps[3]), gapAlert(gaps[2])}},
		{Kind: corpAct, Count: 1, Top: []*adminalertv1.Alert{actAlert(actRow(2))}},
		{Kind: finding, Count: 2, Top: []*adminalertv1.Alert{findingAlert(findingRow(4, gen.FindingKindContradiction)), findingAlert(findingRow(3, gen.FindingKindContradiction))}},
	}}
	if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
		t.Errorf("GetAlertDigest mismatch (-want +got):\n%s", diff)
	}
}

func TestGetAlertDigestFails(t *testing.T) {
	tests := []struct {
		name   string
		expect func(f *fixture)
	}{
		{name: "reader", expect: func(f *fixture) {
			f.reader.EXPECT().Unconfirmed(gomock.Any(), today).Return(nil, nil, errBoom)
		}},
		{name: "counts", expect: func(f *fixture) {
			f.reader.EXPECT().Unconfirmed(gomock.Any(), today).Return(nil, nil, nil)
			f.queries.EXPECT().CountFailedRuns(gomock.Any()).Return(int32(0), errBoom)
		}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.expect(f)
			_, err := f.client.GetAlertDigest(context.Background(), connect.NewRequest(&adminalertv1.GetAlertDigestRequest{}))
			if servicetest.CodeOf(err) != connect.CodeInternal {
				t.Errorf("GetAlertDigest code = %v (err %v), want %v", servicetest.CodeOf(err), err, connect.CodeInternal)
			}
		})
	}
}

// TestListAlertsDerived checks that a derived kind orders by transactions,
// then the latest date, then the ticker's name, pages by the last row's
// tuple, and continues from a token whose row has vanished.
func TestListAlertsDerived(t *testing.T) {
	a, b, c, d := tickerGap("A", 3, 10), tickerGap("B", 3, 12), tickerGap("C", 1, 20), tickerGap("D", 3, 12)
	gaps := []coverage.TickerGap{a, b, c, d}
	unnamed := []coverage.NamesNothing{{Ticker: types.MarketTicker{Market: "US", Value: "NONE"}, Keys: 1, Txns: 2, First: day(3)}}
	tests := []struct {
		name     string
		req      *adminalertv1.ListAlertsRequest
		read     bool
		want     *adminalertv1.ListAlertsResponse
		wantCode connect.Code
	}{
		{
			name: "first page", req: &adminalertv1.ListAlertsRequest{Kind: gap, PageSize: 2}, read: true,
			want: &adminalertv1.ListAlertsResponse{Alerts: []*adminalertv1.Alert{gapAlert(b), gapAlert(d)}, NextPageToken: "3:2026-03-12:US:D"},
		},
		{
			name: "next page", req: &adminalertv1.ListAlertsRequest{Kind: gap, PageSize: 2, PageToken: "3:2026-03-12:US:D"}, read: true,
			want: &adminalertv1.ListAlertsResponse{Alerts: []*adminalertv1.Alert{gapAlert(a), gapAlert(c)}},
		},
		{
			name: "vanished row", req: &adminalertv1.ListAlertsRequest{Kind: gap, PageToken: "3:2026-03-11:US:Z"}, read: true,
			want: &adminalertv1.ListAlertsResponse{Alerts: []*adminalertv1.Alert{gapAlert(a), gapAlert(c)}},
		},
		{
			name: "names nothing", req: &adminalertv1.ListAlertsRequest{Kind: nothing}, read: true,
			want: &adminalertv1.ListAlertsResponse{Alerts: []*adminalertv1.Alert{{Kind: nothing, Subject: &adminalertv1.Alert_NamesNothing{NamesNothing: unnamed[0].ToProto()}}}},
		},
		{name: "malformed token", req: &adminalertv1.ListAlertsRequest{Kind: gap, PageToken: "next"}, wantCode: connect.CodeInvalidArgument},
		{name: "malformed date", req: &adminalertv1.ListAlertsRequest{Kind: gap, PageToken: "3:soon:US:A"}, wantCode: connect.CodeInvalidArgument},
		{name: "unspecified kind", req: &adminalertv1.ListAlertsRequest{}, wantCode: connect.CodeInvalidArgument},
		{name: "oversized page", req: &adminalertv1.ListAlertsRequest{Kind: gap, PageSize: 201}, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.read {
				f.reader.EXPECT().Unconfirmed(gomock.Any(), today).Return(unnamed, gaps, nil)
			}
			res, err := f.client.ListAlerts(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListAlerts(%v) code = %v (err %v), want %v", tc.req, servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListAlerts(%v) mismatch (-want +got):\n%s", tc.req, diff)
			}
		})
	}
}

// TestListAlertsRows checks that each kind read from a table passes the
// token through as the row before, reads one row more than the page, cuts
// the page and names the last row in the next token. A corporate action's
// token carries its ex-date before its id.
func TestListAlertsRows(t *testing.T) {
	before := id(9)
	tests := []struct {
		name   string
		kind   adminalertv1.AlertKind
		token  string
		expect func(f *fixture, err error)
		want   []*adminalertv1.Alert
		next   string
	}{
		{
			name: "failed runs", kind: failedRun,
			expect: func(f *fixture, err error) {
				f.queries.EXPECT().ListFailedRuns(gomock.Any(), gen.ListFailedRunsParams{Before: &before, Lim: 3}).Return([]gen.ListFailedRunsRow{runRow(8), runRow(7), runRow(6)}, err)
			},
			want: []*adminalertv1.Alert{runAlert(runRow(8)), runAlert(runRow(7))},
		},
		{
			name: "blocks", kind: block,
			expect: func(f *fixture, err error) {
				f.queries.EXPECT().ListBlocks(gomock.Any(), gen.ListBlocksParams{Before: &before, Lim: 3}).Return([]gen.ListBlocksRow{blockRow(8), blockRow(7), blockRow(6)}, err)
			},
			want: []*adminalertv1.Alert{blockAlert(blockRow(8)), blockAlert(blockRow(7))},
		},
		{
			name: "corporate actions", kind: corpAct, token: "2026-03-09:" + before.String(),
			expect: func(f *fixture, err error) {
				rows := []gen.ListUnknownCorpActsRow{actRow(8), actRow(7), actRow(6)}
				f.queries.EXPECT().ListUnknownCorpActs(gomock.Any(), gen.ListUnknownCorpActsParams{BeforeDay: ptr.To(day(9)), BeforeID: &before, Lim: 3}).Return(rows, err)
				if err == nil {
					markets(f)
				}
			},
			want: []*adminalertv1.Alert{actAlert(actRow(8)), actAlert(actRow(7))},
			next: "2026-03-07:" + id(7).String(),
		},
		{
			name: "findings", kind: finding,
			expect: func(f *fixture, err error) {
				rows := []gen.ListOpenFindingsRow{findingRow(8, gen.FindingKindContradiction), findingRow(7, gen.FindingKindMissedEvent), findingRow(6, gen.FindingKindContradiction)}
				f.queries.EXPECT().ListOpenFindings(gomock.Any(), gen.ListOpenFindingsParams{Kinds: findingKinds, Before: &before, Lim: 3}).Return(rows, err)
			},
			want: []*adminalertv1.Alert{findingAlert(findingRow(8, gen.FindingKindContradiction)), findingAlert(findingRow(7, gen.FindingKindMissedEvent))},
		},
	}
	for _, tc := range tests {
		token, next := tc.token, tc.next
		if token == "" {
			token, next = before.String(), id(7).String()
		}
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.expect(f, nil)
			req := &adminalertv1.ListAlertsRequest{Kind: tc.kind, PageSize: 2, PageToken: token}
			res, err := f.client.ListAlerts(context.Background(), connect.NewRequest(req))
			if err != nil {
				t.Fatalf("ListAlerts(%v): %v", req, err)
			}
			want := &adminalertv1.ListAlertsResponse{Alerts: tc.want, NextPageToken: next}
			if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListAlerts(%v) mismatch (-want +got):\n%s", req, diff)
			}
		})
		t.Run(tc.name+" fails", func(t *testing.T) {
			f := newFixture(t)
			tc.expect(f, errBoom)
			req := &adminalertv1.ListAlertsRequest{Kind: tc.kind, PageSize: 2, PageToken: token}
			_, err := f.client.ListAlerts(context.Background(), connect.NewRequest(req))
			if servicetest.CodeOf(err) != connect.CodeInternal {
				t.Errorf("ListAlerts(%v) code = %v (err %v), want %v", req, servicetest.CodeOf(err), err, connect.CodeInternal)
			}
		})
	}
}

// TestListAlertsDefaultPage checks that a request without a page size reads
// the default size and one more, and that a malformed id token is refused.
func TestListAlertsDefaultPage(t *testing.T) {
	f := newFixture(t)
	f.queries.EXPECT().ListBlocks(gomock.Any(), gen.ListBlocksParams{Lim: page.DefaultSize + 1}).Return(nil, nil)
	res, err := f.client.ListAlerts(context.Background(), connect.NewRequest(&adminalertv1.ListAlertsRequest{Kind: block}))
	if err != nil || len(res.Msg.GetAlerts()) != 0 || res.Msg.GetNextPageToken() != "" {
		t.Errorf("ListAlerts(block) = %v, %v, want an empty page", res, err)
	}
	_, err = f.client.ListAlerts(context.Background(), connect.NewRequest(&adminalertv1.ListAlertsRequest{Kind: block, PageToken: "next"}))
	if servicetest.CodeOf(err) != connect.CodeInvalidArgument {
		t.Errorf("ListAlerts(block, next) code = %v, want %v", servicetest.CodeOf(err), connect.CodeInvalidArgument)
	}
	for _, token := range []string{id(1).String(), "soon:" + id(1).String(), "2026-03-01:next"} {
		_, err = f.client.ListAlerts(context.Background(), connect.NewRequest(&adminalertv1.ListAlertsRequest{Kind: corpAct, PageToken: token}))
		if servicetest.CodeOf(err) != connect.CodeInvalidArgument {
			t.Errorf("ListAlerts(corp act, %q) code = %v, want %v", token, servicetest.CodeOf(err), connect.CodeInvalidArgument)
		}
	}
}

// TestFindingKindsPartition checks that every finding kind is either an
// alert of kind FINDING or named here as not one, so a new kind cannot be
// left out of the alerts page by accident.
func TestFindingKindsPartition(t *testing.T) {
	notAlerts := []gen.FindingKind{gen.FindingKindBlock, gen.FindingKindUnhandledCorpAct, gen.FindingKindDropped, gen.FindingKindMerged}
	for _, k := range gen.AllFindingKindValues() {
		alert, not := slices.Contains(findingKinds, string(k)), slices.Contains(notAlerts, k)
		if alert == not {
			t.Errorf("finding kind %s: alert %v, named as not one %v; want exactly one", k, alert, not)
		}
	}
	want := []string{"contradiction", "identifier_disagreement", "broken_ticker_validity_bracket", "missed_event", "unmatched_split"}
	if diff := cmp.Diff(want, findingKinds); diff != "" {
		t.Errorf("findingKinds mismatch (-want +got):\n%s", diff)
	}
}
