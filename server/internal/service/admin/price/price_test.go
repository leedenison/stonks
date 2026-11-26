package price

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
	runv1 "github.com/leedenison/stonks/proto/run/v1"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/market/googlesheets"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/price"
	"github.com/leedenison/stonks/server/internal/service/admin/price/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	listingID = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	rateID    = uuid.MustParse("00000000-0000-0000-0000-000000000022")
	unaskedID = uuid.MustParse("00000000-0000-0000-0000-000000000023")
	instrID   = uuid.MustParse("00000000-0000-0000-0000-000000000031")
)

// now is the moment of every request: a Wednesday afternoon.
var now = time.Date(2026, time.November, 25, 15, 30, 0, 0, time.UTC)

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

type fixture struct {
	queries  *mock.MockQueries
	demand   *mock.MockDemand
	sources  *mock.MockSources
	uploader *mock.MockUploader
	client   adminpricev1connect.PriceServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{queries: mock.NewMockQueries(ctrl), demand: mock.NewMockDemand(ctrl), sources: mock.NewMockSources(ctrl), uploader: mock.NewMockUploader(ctrl)}
	opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminpricev1connect.NewPriceServiceHandler(New(f.queries, f.demand, f.sources, f.uploader, func() time.Time { return now }), opts...))
	})
	f.client = adminpricev1connect.NewPriceServiceClient(srv.Client, srv.URL)
	return f
}

// enabled has the registry hold the Google Sheets datasource.
func (f *fixture) enabled() {
	f.sources.EXPECT().Entry(googlesheets.Name).Return(&market.Entry{Name: googlesheets.Name}).AnyTimes()
}

func TestListPriceGaps(t *testing.T) {
	gaps := []price.Gap{
		{
			Listing:    listingID,
			Identifier: types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "IBM"},
			Periods:    []types.Period{{From: day(2010, 1, 4), Before: day(2012, 1, 1)}, {From: day(2015, 6, 1), Before: day(2015, 6, 8)}},
		},
		{
			Listing:    rateID,
			Identifier: types.Identifier{Type: types.IdentifierTypeCurrency, Value: "EUR"},
			Periods:    []types.Period{{From: day(2010, 1, 4), Before: day(2026, 11, 25)}},
		},
	}
	tests := []struct {
		name     string
		expect   func(f *fixture)
		want     *adminpricev1.ListPriceGapsResponse
		wantCode connect.Code
	}{
		{
			name: "gaps and a listing without a current ticker",
			expect: func(f *fixture) {
				f.enabled()
				gomock.InOrder(
					f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(nil),
					f.demand.EXPECT().Gaps(gomock.Any(), (*uuid.UUID)(nil)).Return(gaps, []uuid.UUID{unaskedID}, nil),
				)
				f.queries.EXPECT().GetListingByID(gomock.Any(), unaskedID).Return(gen.Listing{ID: unaskedID, InstrID: instrID, Currency: "GBP"}, nil)
			},
			want: &adminpricev1.ListPriceGapsResponse{
				Gaps: []*adminpricev1.PriceGap{
					{
						ListingId:  listingID.String(),
						Identifier: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XNYS", Value: "IBM"},
						Periods: []*typev1.Period{
							{From: ptr.To("2010-01-04"), Before: ptr.To("2012-01-01")},
							{From: ptr.To("2015-06-01"), Before: ptr.To("2015-06-08")},
						},
					},
					{
						ListingId:  rateID.String(),
						Identifier: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "EUR"},
						Periods:    []*typev1.Period{{From: ptr.To("2010-01-04"), Before: ptr.To("2026-11-25")}},
					},
				},
				Unasked: []*adminpricev1.UnaskedListing{{ListingId: unaskedID.String(), InstrId: instrID.String(), Currency: "GBP"}},
			},
		},
		{
			name: "nothing demanded",
			expect: func(f *fixture) {
				f.enabled()
				f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(nil)
				f.demand.EXPECT().Gaps(gomock.Any(), gomock.Any()).Return(nil, nil, nil)
			},
			want: &adminpricev1.ListPriceGapsResponse{},
		},
		{
			name: "the datasource is absent or disabled",
			expect: func(f *fixture) {
				f.sources.EXPECT().Entry(googlesheets.Name).Return(nil)
			},
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name: "the rate listings cannot be created",
			expect: func(f *fixture) {
				f.enabled()
				f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(errors.New("boom"))
			},
			wantCode: connect.CodeInternal,
		},
		{
			name: "the gaps cannot be read",
			expect: func(f *fixture) {
				f.enabled()
				f.demand.EXPECT().UseCurrencies(gomock.Any()).Return(nil)
				f.demand.EXPECT().Gaps(gomock.Any(), gomock.Any()).Return(nil, nil, errors.New("boom"))
			},
			wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			tc.expect(f)
			got, err := f.client.ListPriceGaps(context.Background(), connect.NewRequest(&adminpricev1.ListPriceGapsRequest{}))
			if code := servicetest.CodeOf(err); code != tc.wantCode {
				t.Fatalf("ListPriceGaps() code = %v (%v), want %v", code, err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, got.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListPriceGaps() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// upload is a valid request for one series, evaluated on the day of now.
func upload() *adminpricev1.UploadPricesRequest {
	return &adminpricev1.UploadPricesRequest{
		AsAt: "2026-11-25",
		Series: []*adminpricev1.PriceSeries{{
			ListingId: listingID.String(), Symbol: "NYSE:IBM", Currency: "USD",
			Coverage: []*typev1.Period{{From: ptr.To("2010-01-04"), Before: ptr.To("2010-01-09")}},
			Prices:   []*adminpricev1.DayPrice{{Day: "2010-01-04", Price: "132.45"}, {Day: "2010-01-05", Price: "130.85"}},
		}},
	}
}

func TestUploadPrices(t *testing.T) {
	entry := &market.Entry{Name: googlesheets.Name}
	runID := uuid.MustParse("00000000-0000-0000-0000-000000000041")
	want := []price.Series{{
		Listing: listingID, Symbol: "NYSE:IBM", Currency: "USD",
		Coverage: []types.Period{{From: day(2010, 1, 4), Before: day(2010, 1, 9)}},
		Closes: []price.Close{
			{Day: day(2010, 1, 4), Price: decimal.RequireFromString("132.45")},
			{Day: day(2010, 1, 5), Price: decimal.RequireFromString("130.85")},
		},
	}}
	refusals := []price.Refusal{
		{Listing: listingID, Err: fmt.Errorf("%w: GBX", price.ErrCurrency)},
		{Listing: rateID, Err: fmt.Errorf("%w: 0", price.ErrInvalid)},
		{Listing: unaskedID, Err: fmt.Errorf("%w: gone", price.ErrListing)},
	}
	tests := []struct {
		name string
		req  func(r *adminpricev1.UploadPricesRequest)
		// asAt is the day the uploader must receive; zero means the uploader
		// must not be called.
		asAt     time.Time
		refused  []price.Refusal
		err      error
		absent   bool
		want     []*adminpricev1.RefusedSeries
		wantCode connect.Code
	}{
		{name: "stored", asAt: day(2026, 11, 25)},
		{name: "evaluated on an earlier day", req: func(r *adminpricev1.UploadPricesRequest) { r.AsAt = "2026-11-20" }, asAt: day(2026, 11, 20)},
		{
			name: "each refusal by its reason", asAt: day(2026, 11, 25), refused: refusals,
			want: []*adminpricev1.RefusedSeries{
				{ListingId: listingID.String(), Reason: adminpricev1.RefusalReason_REFUSAL_REASON_CURRENCY},
				{ListingId: rateID.String(), Reason: adminpricev1.RefusalReason_REFUSAL_REASON_INVALID_PRICE},
				{ListingId: unaskedID.String(), Reason: adminpricev1.RefusalReason_REFUSAL_REASON_UNKNOWN_LISTING},
			},
		},
		{name: "the upload fails", asAt: day(2026, 11, 25), err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "the datasource is absent or disabled", absent: true, wantCode: connect.CodeFailedPrecondition},
		{name: "evaluated tomorrow", req: func(r *adminpricev1.UploadPricesRequest) { r.AsAt = "2026-11-26" }, wantCode: connect.CodeInvalidArgument},
		{name: "a day the calendar lacks", req: func(r *adminpricev1.UploadPricesRequest) { r.AsAt = "2026-02-30" }, wantCode: connect.CodeInvalidArgument},
		{name: "a malformed as at day", req: func(r *adminpricev1.UploadPricesRequest) { r.AsAt = "25/11/2026" }, wantCode: connect.CodeInvalidArgument},
		{name: "no series", req: func(r *adminpricev1.UploadPricesRequest) { r.Series = nil }, wantCode: connect.CodeInvalidArgument},
		{
			name: "two series for one listing",
			req: func(r *adminpricev1.UploadPricesRequest) {
				r.Series = append(r.Series, upload().Series[0])
			},
			wantCode: connect.CodeInvalidArgument,
		},
		{name: "a listing that is not a UUID", req: func(r *adminpricev1.UploadPricesRequest) { r.Series[0].ListingId = "listing" }, wantCode: connect.CodeInvalidArgument},
		{name: "a negative price", req: func(r *adminpricev1.UploadPricesRequest) { r.Series[0].Prices[0].Price = "-1" }, wantCode: connect.CodeInvalidArgument},
		{name: "a price that is not a decimal", req: func(r *adminpricev1.UploadPricesRequest) { r.Series[0].Prices[0].Price = "#N/A" }, wantCode: connect.CodeInvalidArgument},
		{name: "an open coverage period", req: func(r *adminpricev1.UploadPricesRequest) { r.Series[0].Coverage[0].Before = nil }, wantCode: connect.CodeInvalidArgument},
		{name: "a series without coverage", req: func(r *adminpricev1.UploadPricesRequest) { r.Series[0].Coverage = nil }, wantCode: connect.CodeInvalidArgument},
		{name: "a series without a currency", req: func(r *adminpricev1.UploadPricesRequest) { r.Series[0].Currency = "" }, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.absent {
				f.sources.EXPECT().Entry(googlesheets.Name).Return(nil).AnyTimes()
			} else {
				f.sources.EXPECT().Entry(googlesheets.Name).Return(entry).AnyTimes()
			}
			if !tc.asAt.IsZero() {
				f.uploader.EXPECT().Upload(gomock.Any(), entry, tc.asAt, gomock.Any()).DoAndReturn(
					func(_ context.Context, _ *market.Entry, _ time.Time, got []price.Series) (gen.Run, []price.Refusal, error) {
						if diff := cmp.Diff(want, got); diff != "" {
							t.Errorf("series mismatch (-want +got):\n%s", diff)
						}
						return gen.Run{ID: runID, Kind: gen.RunKindUploadPrices, Trigger: gen.RunTriggerAdministrator, State: gen.RunStateCompleted}, tc.refused, tc.err
					})
			}
			req := upload()
			if tc.req != nil {
				tc.req(req)
			}
			got, err := f.client.UploadPrices(context.Background(), connect.NewRequest(req))
			if code := servicetest.CodeOf(err); code != tc.wantCode {
				t.Fatalf("UploadPrices() code = %v (%v), want %v", code, err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if got.Msg.GetRun().GetId() != runID.String() || got.Msg.GetRun().GetKind() != runv1.RunKind_RUN_KIND_UPLOAD_PRICES {
				t.Errorf("UploadPrices() run = %v, want the upload prices run %s", got.Msg.GetRun(), runID)
			}
			if diff := cmp.Diff(tc.want, got.Msg.GetRefused(), protocmp.Transform()); diff != "" {
				t.Errorf("UploadPrices() refused mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
