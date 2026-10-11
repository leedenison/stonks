package instrument

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	admininstrumentv1 "github.com/leedenison/stonks/proto/admin/instrument/v1"
	"github.com/leedenison/stonks/proto/admin/instrument/v1/admininstrumentv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	acmeID    = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	gbpID     = uuid.MustParse("00000000-0000-0000-0000-000000000022")
	usdLine   = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	gbpLine   = uuid.MustParse("00000000-0000-0000-0000-000000000032")
	cashLine  = uuid.MustParse("00000000-0000-0000-0000-000000000033")
	adminID   = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	principal = auth.Principal{User: gen.User{ID: adminID, Email: "admin@example.com", Role: gen.UserRoleAdmin}, SessionID: servicetest.Session}
)

type fixture struct {
	queries *MockQueries
	client  admininstrumentv1connect.InstrumentServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicemock.NewMockAuthenticator(ctrl)
	authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil).AnyTimes()
	f := &fixture{queries: NewMockQueries(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(admininstrumentv1connect.NewInstrumentServiceHandler(New(f.queries), opts...))
	})
	f.client = admininstrumentv1connect.NewInstrumentServiceClient(srv.Client, srv.URL)
	return f
}

func TestListInstruments(t *testing.T) {
	// A share listed in two currencies, named by an ISIN and by a ticker on
	// each listing, and the pound, named by its codes with one listing.
	ticker := func(domain string) *typev1.Identifier {
		return &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: domain, Value: "ACME"}
	}
	rows := []gen.ListInstrumentsRow{
		{ID: acmeID, AssetClass: gen.AssetClassStock, Name: "ACME", NameType: string(types.IdentifierTypeMicTicker), NameDomain: "XNYS"},
		{ID: gbpID, AssetClass: gen.AssetClassCash, Name: "GBP", NameType: string(types.IdentifierTypeCurrency)},
	}
	// The dollar listing has a primary venue, so it precedes the pound's.
	listings := []gen.Listing{
		{ID: gbpLine, InstrumentID: acmeID, Currency: "GBP"},
		{ID: usdLine, InstrumentID: acmeID, Currency: "USD", PrimaryMic: ptr.To("XNYS")},
		{ID: cashLine, InstrumentID: gbpID, Currency: "GBP"},
	}
	isinID := uuid.MustParse("00000000-0000-0000-0000-000000000041")
	idents := []gen.Identifier{
		{ID: isinID, InstrumentID: acmeID, Type: types.IdentifierTypeIsin, Value: "GB0002634946"},
		{InstrumentID: acmeID, ListingID: &gbpLine, Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "ACME"},
		{InstrumentID: acmeID, ListingID: &usdLine, Type: types.IdentifierTypeMicTicker, Domain: "XNYS", Value: "ACME"},
		{InstrumentID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBP"},
		{InstrumentID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBX"},
	}
	want := []*admininstrumentv1.Instrument{
		{
			Id: acmeID.String(), Name: ticker("XNYS"), AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK,
			Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"}},
			Listings: []*admininstrumentv1.Listing{
				{Id: usdLine.String(), Currency: "USD", PrimaryMic: "XNYS", Identifiers: []*typev1.Identifier{ticker("XNYS")}},
				{Id: gbpLine.String(), Currency: "GBP", Identifiers: []*typev1.Identifier{ticker("XLON")}},
			},
			Via: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"}},
		},
		{
			Id: gbpID.String(), Name: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"}, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH,
			Identifiers: []*typev1.Identifier{
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"},
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBX"},
			},
			Listings: []*admininstrumentv1.Listing{{Id: cashLine.String(), Currency: "GBP"}},
		},
	}
	ids := []uuid.UUID{acmeID, gbpID}

	tests := []struct {
		name        string
		req         *admininstrumentv1.ListInstrumentsRequest
		wantArg     *gen.ListInstrumentsParams
		rows        []gen.ListInstrumentsRow
		rowsErr     error
		listingsErr error
		identsErr   error
		viaErr      error
		want        *admininstrumentv1.ListInstrumentsResponse
		wantCode    connect.Code
	}{
		{
			name:    "a page after a token",
			req:     &admininstrumentv1.ListInstrumentsRequest{PageToken: acmeID.String() + ":ACME"},
			wantArg: &gen.ListInstrumentsParams{AfterID: &acmeID, AfterName: ptr.To("ACME"), Lim: page.DefaultSize + 1},
			rows:    rows,
			want:    &admininstrumentv1.ListInstrumentsResponse{Instruments: want},
		},
		{
			name:    "none",
			req:     &admininstrumentv1.ListInstrumentsRequest{},
			wantArg: &gen.ListInstrumentsParams{Lim: page.DefaultSize + 1},
			want:    &admininstrumentv1.ListInstrumentsResponse{},
		},
		{
			name:    "a query and a class, the query escaped",
			req:     &admininstrumentv1.ListInstrumentsRequest{Query: "50%_", AssetClass: typev1.AssetClass_ASSET_CLASS_EQUITY.Enum()},
			wantArg: &gen.ListInstrumentsParams{Lim: page.DefaultSize + 1, Q: ptr.To(`50\%\_`), AssetClass: ptr.To(gen.AssetClassEquity)},
			want:    &admininstrumentv1.ListInstrumentsResponse{},
		},
		{name: "oversized page", req: &admininstrumentv1.ListInstrumentsRequest{PageSize: 201}, wantCode: connect.CodeInvalidArgument},
		{name: "unspecified class", req: &admininstrumentv1.ListInstrumentsRequest{AssetClass: typev1.AssetClass_ASSET_CLASS_UNSPECIFIED.Enum()}, wantCode: connect.CodeInvalidArgument},
		{name: "a token without a name", req: &admininstrumentv1.ListInstrumentsRequest{PageToken: acmeID.String()}, wantCode: connect.CodeInvalidArgument},
		{name: "a token whose id is malformed", req: &admininstrumentv1.ListInstrumentsRequest{PageToken: "not-a-uuid:ACME"}, wantCode: connect.CodeInvalidArgument},
		{name: "instruments failure", req: &admininstrumentv1.ListInstrumentsRequest{}, wantArg: &gen.ListInstrumentsParams{Lim: page.DefaultSize + 1}, rowsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "listings failure", req: &admininstrumentv1.ListInstrumentsRequest{}, wantArg: &gen.ListInstrumentsParams{Lim: page.DefaultSize + 1}, rows: rows, listingsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "identifiers failure", req: &admininstrumentv1.ListInstrumentsRequest{}, wantArg: &gen.ListInstrumentsParams{Lim: page.DefaultSize + 1}, rows: rows, identsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "via failure", req: &admininstrumentv1.ListInstrumentsRequest{}, wantArg: &gen.ListInstrumentsParams{Lim: page.DefaultSize + 1}, rows: rows, viaErr: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantArg != nil {
				f.queries.EXPECT().ListInstruments(gomock.Any(), *tc.wantArg).Return(tc.rows, tc.rowsErr)
			}
			if tc.rowsErr == nil && len(tc.rows) > 0 {
				f.queries.EXPECT().ListListingsOf(gomock.Any(), ids).Return(listings, tc.listingsErr)
			}
			if tc.rowsErr == nil && len(tc.rows) > 0 && tc.listingsErr == nil {
				f.queries.EXPECT().ListIdentifiersOf(gomock.Any(), ids).Return(idents, tc.identsErr)
			}
			if tc.rowsErr == nil && len(tc.rows) > 0 && tc.listingsErr == nil && tc.identsErr == nil {
				f.queries.EXPECT().ListViaOf(gomock.Any(), ids).Return([]uuid.UUID{isinID}, tc.viaErr)
			}
			res, err := f.client.ListInstruments(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListInstruments(%v) code = %v (err %v), want %v", tc.req, servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListInstruments(%v) mismatch (-want +got):\n%s", tc.req, diff)
			}
		})
	}
}

// TestCursor checks that a page token carries the last row's id and name
// back to the query whatever the name holds, and that a token with no name
// or a malformed id is refused.
func TestCursor(t *testing.T) {
	for _, name := range []string{"ACME", "ACME: CLASS A", ""} {
		t.Run(fmt.Sprintf("name %q", name), func(t *testing.T) {
			id, got, err := parseCursor(cursor(gen.ListInstrumentsRow{ID: acmeID, Name: name}))
			if err != nil || id == nil || *id != acmeID || got == nil || *got != name {
				t.Errorf("parseCursor(cursor()) = %v, %v, %v, want the id and %q", id, got, err, name)
			}
		})
	}
	for _, token := range []string{acmeID.String(), "not-a-uuid:ACME", ":ACME"} {
		if _, _, err := parseCursor(token); err == nil {
			t.Errorf("parseCursor(%q) = nil, want an error", token)
		}
	}
	if id, name, err := parseCursor(""); id != nil || name != nil || err != nil {
		t.Errorf("parseCursor(\"\") = %v, %v, %v, want the first page", id, name, err)
	}
}

// TestListInstrumentsPages checks that a full page is cut to its size and
// that the token of the next page names the last instrument served.
func TestListInstrumentsPages(t *testing.T) {
	f := newFixture(t)
	rows := []gen.ListInstrumentsRow{{ID: acmeID, AssetClass: gen.AssetClassStock, Name: "ACME"}, {ID: gbpID, AssetClass: gen.AssetClassCash, Name: "GBP"}}
	f.queries.EXPECT().ListInstruments(gomock.Any(), gen.ListInstrumentsParams{Lim: 2}).Return(rows, nil)
	f.queries.EXPECT().ListListingsOf(gomock.Any(), []uuid.UUID{acmeID}).Return(nil, nil)
	f.queries.EXPECT().ListIdentifiersOf(gomock.Any(), []uuid.UUID{acmeID}).Return(nil, nil)
	f.queries.EXPECT().ListViaOf(gomock.Any(), []uuid.UUID{acmeID}).Return(nil, nil)
	res, err := f.client.ListInstruments(context.Background(), connect.NewRequest(&admininstrumentv1.ListInstrumentsRequest{PageSize: 1}))
	if err != nil {
		t.Fatalf("ListInstruments() error = %v", err)
	}
	got := res.Msg.GetInstruments()
	if len(got) != 1 || got[0].GetId() != acmeID.String() || res.Msg.GetNextPageToken() != acmeID.String()+":ACME" {
		t.Errorf("ListInstruments() = %v, want the first instrument and a token naming it", res.Msg)
	}
}
