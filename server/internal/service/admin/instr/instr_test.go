package instr

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

	admininstrv1 "github.com/leedenison/stonks/proto/admin/instr/v1"
	"github.com/leedenison/stonks/proto/admin/instr/v1/admininstrv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/service/admin/instr/mock"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	acmeID   = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	gbpID    = uuid.MustParse("00000000-0000-0000-0000-000000000022")
	usdLine  = uuid.MustParse("00000000-0000-0000-0000-000000000031")
	gbpLine  = uuid.MustParse("00000000-0000-0000-0000-000000000032")
	cashLine = uuid.MustParse("00000000-0000-0000-0000-000000000033")
	goneLine = uuid.MustParse("00000000-0000-0000-0000-000000000034")
)

type fixture struct {
	queries *mock.MockQueries
	client  admininstrv1connect.InstrServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicetest.AsAdmin(ctrl)
	f := &fixture{queries: mock.NewMockQueries(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(admininstrv1connect.NewInstrServiceHandler(New(f.queries), opts...))
	})
	f.client = admininstrv1connect.NewInstrServiceClient(srv.Client, srv.URL)
	return f
}

func TestListInstrs(t *testing.T) {
	// A share listed in two currencies, named by an ISIN and by a ticker on
	// each listing, and the pound, named by its codes with one listing. The
	// dollar listing has two tickers, and the pound's has none.
	ticker := func(domain string) *typev1.Identifier {
		return &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: domain, Value: "ACME"}
	}
	listed := func(venue string) *admininstrv1.Listing_Ticker {
		return &admininstrv1.Listing_Ticker{Ticker: &typev1.MarketTicker{Market: "US", Value: "ACME"}, Mic: &venue, Relation: typev1.TickerRelation_TICKER_RELATION_CURRENT}
	}
	rows := []gen.ListInstrsRow{
		{ID: acmeID, AssetClass: types.AssetClassStock, Name: "ACME", NameType: string(types.IdentifierTypeMicTicker), NameDomain: "XNYS"},
		{ID: gbpID, AssetClass: types.AssetClassCash, Name: "GBP", NameType: string(types.IdentifierTypeCurrency)},
	}
	tickerID := uuid.MustParse("00000000-0000-0000-0000-000000000051")
	name := func(venue string) gen.ListListingNamesRow {
		return gen.ListListingNamesRow{ListingID: usdLine, InstrID: acmeID, Currency: "USD", PrimaryMic: ptr.To("XNYS"), TickerID: &tickerID, Market: ptr.To("US"), TickerValue: ptr.To("ACME"), Venue: &venue, Relation: ptr.To(gen.TickerRelationCurrent)}
	}
	// The dollar listing has a primary venue, so it precedes the pound's.
	names := []gen.ListListingNamesRow{
		{ListingID: gbpLine, InstrID: acmeID, Currency: "GBP"},
		name("XNYS"),
		name("XNAS"),
		{ListingID: cashLine, InstrID: gbpID, Currency: "GBP"},
	}
	isinID := uuid.MustParse("00000000-0000-0000-0000-000000000041")
	idents := []gen.Identifier{
		{ID: isinID, InstrID: acmeID, Type: types.IdentifierTypeIsin, Value: "GB0002634946"},
		{InstrID: acmeID, ListingID: &usdLine, Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000B9XRY4"},
		// A listing that ListListingNames did not return, so the response
		// leaves its identifier out.
		{InstrID: acmeID, ListingID: &goneLine, Type: types.IdentifierTypeOpenfigiComposite, Value: "BBG000B9XRZ3"},
		{InstrID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBP"},
		{InstrID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBX"},
	}
	want := []*admininstrv1.Instr{
		{
			Id: acmeID.String(), Name: ticker("XNYS"), AssetClass: typev1.AssetClass_ASSET_CLASS_STOCK,
			Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"}},
			Listings: []*admininstrv1.Listing{
				{
					Id: usdLine.String(), Currency: "USD", PrimaryMic: "XNYS",
					Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_OPENFIGI_COMPOSITE, Value: "BBG000B9XRY4"}},
					Tickers:     []*admininstrv1.Listing_Ticker{listed("XNYS"), listed("XNAS")},
				},
				{Id: gbpLine.String(), Currency: "GBP"},
			},
			Via: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"}},
		},
		{
			Id: gbpID.String(), Name: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"}, AssetClass: typev1.AssetClass_ASSET_CLASS_CASH,
			Identifiers: []*typev1.Identifier{
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"},
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBX"},
			},
			Listings: []*admininstrv1.Listing{{Id: cashLine.String(), Currency: "GBP"}},
		},
	}
	ids := []uuid.UUID{acmeID, gbpID}

	tests := []struct {
		name      string
		req       *admininstrv1.ListInstrsRequest
		wantArg   *gen.ListInstrsParams
		rows      []gen.ListInstrsRow
		rowsErr   error
		namesErr  error
		identsErr error
		viaErr    error
		want      *admininstrv1.ListInstrsResponse
		wantCode  connect.Code
	}{
		{
			name:    "a page after a token",
			req:     &admininstrv1.ListInstrsRequest{PageToken: acmeID.String() + ":ACME"},
			wantArg: &gen.ListInstrsParams{AfterID: &acmeID, AfterName: ptr.To("ACME"), Lim: page.DefaultSize + 1},
			rows:    rows,
			want:    &admininstrv1.ListInstrsResponse{Instrs: want},
		},
		{
			name:    "none",
			req:     &admininstrv1.ListInstrsRequest{},
			wantArg: &gen.ListInstrsParams{Lim: page.DefaultSize + 1},
			want:    &admininstrv1.ListInstrsResponse{},
		},
		{
			name:    "a query and a class, the query escaped",
			req:     &admininstrv1.ListInstrsRequest{Query: "50%_", AssetClass: typev1.AssetClass_ASSET_CLASS_EQUITY.Enum()},
			wantArg: &gen.ListInstrsParams{Lim: page.DefaultSize + 1, Q: ptr.To(`50\%\_`), AssetClass: ptr.To(types.AssetClassEquity)},
			want:    &admininstrv1.ListInstrsResponse{},
		},
		{name: "oversized page", req: &admininstrv1.ListInstrsRequest{PageSize: 201}, wantCode: connect.CodeInvalidArgument},
		{name: "unspecified class", req: &admininstrv1.ListInstrsRequest{AssetClass: typev1.AssetClass_ASSET_CLASS_UNSPECIFIED.Enum()}, wantCode: connect.CodeInvalidArgument},
		{name: "a token without a name", req: &admininstrv1.ListInstrsRequest{PageToken: acmeID.String()}, wantCode: connect.CodeInvalidArgument},
		{name: "a token whose id is malformed", req: &admininstrv1.ListInstrsRequest{PageToken: "not-a-uuid:ACME"}, wantCode: connect.CodeInvalidArgument},
		{name: "instruments failure", req: &admininstrv1.ListInstrsRequest{}, wantArg: &gen.ListInstrsParams{Lim: page.DefaultSize + 1}, rowsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "names failure", req: &admininstrv1.ListInstrsRequest{}, wantArg: &gen.ListInstrsParams{Lim: page.DefaultSize + 1}, rows: rows, namesErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "identifiers failure", req: &admininstrv1.ListInstrsRequest{}, wantArg: &gen.ListInstrsParams{Lim: page.DefaultSize + 1}, rows: rows, identsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "via failure", req: &admininstrv1.ListInstrsRequest{}, wantArg: &gen.ListInstrsParams{Lim: page.DefaultSize + 1}, rows: rows, viaErr: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.wantArg != nil {
				f.queries.EXPECT().ListInstrs(gomock.Any(), *tc.wantArg).Return(tc.rows, tc.rowsErr)
			}
			if tc.rowsErr == nil && len(tc.rows) > 0 {
				f.queries.EXPECT().ListListingNames(gomock.Any(), ids).Return(names, tc.namesErr)
			}
			if tc.rowsErr == nil && len(tc.rows) > 0 && tc.namesErr == nil {
				f.queries.EXPECT().ListIdentifiersOf(gomock.Any(), ids).Return(idents, tc.identsErr)
			}
			if tc.rowsErr == nil && len(tc.rows) > 0 && tc.namesErr == nil && tc.identsErr == nil {
				f.queries.EXPECT().ListViaOf(gomock.Any(), ids).Return([]uuid.UUID{isinID}, tc.viaErr)
			}
			res, err := f.client.ListInstrs(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListInstrs(%v) code = %v (err %v), want %v", tc.req, servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListInstrs(%v) mismatch (-want +got):\n%s", tc.req, diff)
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
			id, got, err := parseCursor(cursor(gen.ListInstrsRow{ID: acmeID, Name: name}))
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

// TestListInstrsPages checks that a full page is cut to its size and
// that the token of the next page names the last instrument served.
func TestListInstrsPages(t *testing.T) {
	f := newFixture(t)
	rows := []gen.ListInstrsRow{{ID: acmeID, AssetClass: types.AssetClassStock, Name: "ACME"}, {ID: gbpID, AssetClass: types.AssetClassCash, Name: "GBP"}}
	f.queries.EXPECT().ListInstrs(gomock.Any(), gen.ListInstrsParams{Lim: 2}).Return(rows, nil)
	f.queries.EXPECT().ListListingNames(gomock.Any(), []uuid.UUID{acmeID}).Return(nil, nil)
	f.queries.EXPECT().ListIdentifiersOf(gomock.Any(), []uuid.UUID{acmeID}).Return(nil, nil)
	f.queries.EXPECT().ListViaOf(gomock.Any(), []uuid.UUID{acmeID}).Return(nil, nil)
	res, err := f.client.ListInstrs(context.Background(), connect.NewRequest(&admininstrv1.ListInstrsRequest{PageSize: 1}))
	if err != nil {
		t.Fatalf("ListInstrs() error = %v", err)
	}
	got := res.Msg.GetInstrs()
	if len(got) != 1 || got[0].GetId() != acmeID.String() || res.Msg.GetNextPageToken() != acmeID.String()+":ACME" {
		t.Errorf("ListInstrs() = %v, want the first instrument and a token naming it", res.Msg)
	}
}
