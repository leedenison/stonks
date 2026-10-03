package instrument

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	instrumentv1 "github.com/leedenison/stonks/proto/instrument/v1"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/service/instrument/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	acmeID    = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	gbpID     = uuid.MustParse("00000000-0000-0000-0000-000000000022")
	usdLine   = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	gbpLine   = uuid.MustParse("00000000-0000-0000-0000-000000000032")
	cashLine  = uuid.MustParse("00000000-0000-0000-0000-000000000033")
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: servicetest.Session}
)

type fixture struct {
	reader *mock.MockReader
	authn  *servicemock.MockAuthenticator
	client instrumentv1connect.InstrumentServiceClient
}

// newFixture mounts a Server with the real handler chain and a client that
// carries a session cookie.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{reader: mock.NewMockReader(ctrl), authn: servicemock.NewMockAuthenticator(ctrl)}
	opts := servicetest.Options(t, f.authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(instrumentv1connect.NewInstrumentServiceHandler(New(f.reader), opts...))
	})
	f.client = instrumentv1connect.NewInstrumentServiceClient(srv.Client, srv.URL)
	return f
}

func TestListInstruments(t *testing.T) {
	// A share listed in two currencies, named by an ISIN and by a ticker on
	// each listing, and the pound, named by its codes with one listing.
	rows := []gen.Instrument{
		{ID: acmeID, AssetClass: gen.AssetClassStock},
		{ID: gbpID, AssetClass: gen.AssetClassCash},
	}
	listings := []gen.Listing{
		{ID: gbpLine, InstrumentID: acmeID, Currency: "GBP"},
		{ID: usdLine, InstrumentID: acmeID, Currency: "USD"},
		{ID: cashLine, InstrumentID: gbpID, Currency: "GBP"},
	}
	idents := []gen.Identifier{
		{InstrumentID: acmeID, Type: types.IdentifierTypeIsin, Value: "GB0002634946"},
		{InstrumentID: acmeID, ListingID: &gbpLine, Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "ACME"},
		{InstrumentID: acmeID, ListingID: &usdLine, Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "ACME"},
		{InstrumentID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBP"},
		{InstrumentID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBX"},
	}
	ticker := func(domain string) *typev1.Identifier {
		return &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: domain, Value: "ACME"}
	}
	want := []*instrumentv1.Instrument{
		{
			Id: acmeID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK,
			Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"}},
			Listings: []*instrumentv1.Listing{
				{Id: gbpLine.String(), Currency: "GBP", Identifiers: []*typev1.Identifier{ticker("XLON")}},
				{Id: usdLine.String(), Currency: "USD", Identifiers: []*typev1.Identifier{ticker("XNYS")}},
			},
		},
		{
			Id: gbpID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_CASH,
			Identifiers: []*typev1.Identifier{
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"},
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBX"},
			},
			Listings: []*instrumentv1.Listing{{Id: cashLine.String(), Currency: "GBP"}},
		},
	}
	ids := []uuid.UUID{acmeID, gbpID}

	tests := []struct {
		name        string
		authErr     error
		rows        []gen.Instrument
		rowsErr     error
		listings    []gen.Listing
		listingsErr error
		idents      []gen.Identifier
		identsErr   error
		want        []*instrumentv1.Instrument
		wantCode    connect.Code
	}{
		{name: "none"},
		{name: "two instruments", rows: rows, listings: listings, idents: idents, want: want},
		{name: "instruments failure", rowsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "listings failure", rows: rows, listingsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "identifiers failure", rows: rows, listings: listings, identsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			if tc.authErr == nil {
				f.reader.EXPECT().ListUserInstruments(gomock.Any(), userID).Return(tc.rows, tc.rowsErr)
			}
			if tc.authErr == nil && tc.rowsErr == nil && len(tc.rows) > 0 {
				f.reader.EXPECT().ListListingsOf(gomock.Any(), ids).Return(tc.listings, tc.listingsErr)
			}
			if tc.authErr == nil && tc.rowsErr == nil && len(tc.rows) > 0 && tc.listingsErr == nil {
				f.reader.EXPECT().ListIdentifiersOf(gomock.Any(), ids).Return(tc.idents, tc.identsErr)
			}
			res, err := f.client.ListInstruments(context.Background(), connect.NewRequest(&instrumentv1.ListInstrumentsRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListInstruments() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(&instrumentv1.ListInstrumentsResponse{Instruments: tc.want}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListInstruments() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
