package price

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

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
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

func day(y int, m time.Month, d int) time.Time { return time.Date(y, m, d, 0, 0, 0, 0, time.UTC) }

type fixture struct {
	queries *mock.MockQueries
	demand  *mock.MockDemand
	sources *mock.MockSources
	client  adminpricev1connect.PriceServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{queries: mock.NewMockQueries(ctrl), demand: mock.NewMockDemand(ctrl), sources: mock.NewMockSources(ctrl)}
	opts := servicetest.Options(t, servicetest.AsAdmin(ctrl))
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminpricev1connect.NewPriceServiceHandler(New(f.queries, f.demand, f.sources), opts...))
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
