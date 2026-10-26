package holding

import (
	"context"
	"errors"
	"net/http"
	"slices"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"

	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/holding/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	gbpID     = uuid.MustParse("00000000-0000-0000-0000-000000000020")
	acmeID    = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	groupID   = uuid.MustParse("00000000-0000-0000-0000-000000000030")
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: servicetest.Session}
	families  = currency.Families{"GBP": "GBP", "GBX": "GBP", "USD": "USD"}
	scales    = currency.Scales{"GBX": decimal.RequireFromString("0.01")}
)

type fixture struct {
	reader *mock.MockReader
	authn  *servicemock.MockAuthenticator
	client holdingv1connect.HoldingServiceClient
}

// newFixture mounts a Server with the real handler chain and a client that
// carries a session cookie.
// mics puts XNAS and XNYS in the market US, so a key dated through a ticker
// at XNAS reads its facts at US.
var mics = mic.MICMap{"XNAS": {Operating: "XNAS", Market: "US"}, "XNYS": {Operating: "XNYS", Market: "US"}}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{reader: mock.NewMockReader(ctrl), authn: servicemock.NewMockAuthenticator(ctrl)}
	opts := servicetest.Options(t, f.authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(holdingv1connect.NewHoldingServiceHandler(New(f.reader, families, scales, mics), opts...))
	})
	f.client = holdingv1connect.NewHoldingServiceClient(srv.Client, srv.URL)
	return f
}

func TestListHoldings(t *testing.T) {
	acmeListingID, acmeKeyID := uuid.MustParse("00000000-0000-0000-0000-000000000050"), uuid.MustParse("00000000-0000-0000-0000-000000000060")
	gbpListingID := uuid.MustParse("00000000-0000-0000-0000-000000000051")
	instruments := []gen.Instrument{{ID: gbpID, AssetClass: types.AssetClassCash}, {ID: acmeID, AssetClass: types.AssetClassSecurity}}
	idents := []gen.Identifier{
		{InstrumentID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBP"},
		{InstrumentID: acmeID, Type: types.IdentifierTypeIsin, Value: "GB0002634946"},
		{InstrumentID: acmeID, Type: types.IdentifierTypeSedol, Value: "0263494"},
	}
	// The pound listing has no ticker; ACME's is named at the LSE.
	listings := []gen.ListListingNamesRow{
		{ListingID: gbpListingID, InstrumentID: gbpID, Currency: "GBP"},
		{ListingID: acmeListingID, InstrumentID: acmeID, Currency: "GBP", TickerDomain: ptr.To("XLON"), TickerValue: ptr.To("ACME"), Venue: "LSE"},
	}
	// ACME is named by its ISIN; the pound instrument's name is left out, as
	// an instrument nothing names.
	names := []gen.InstrumentName{{InstrumentID: acmeID, Type: types.IdentifierTypeIsin, Value: "GB0002634946"}}
	acmeISIN := types.Identifier{Type: types.IdentifierTypeIsin, Value: "GB0002634946"}
	acmeDescribed := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME PLC"}
	// The pound holding is stated in pence, and sums in pounds.
	gbxKeyID := uuid.MustParse("00000000-0000-0000-0000-000000000063")
	gbx := types.Identifier{Type: types.IdentifierTypeCurrency, Value: "GBX"}
	instrumentKeys := []gen.ListHoldingKeysRow{
		{
			StatedKey: gen.StatedKey{ID: acmeKeyID, InstrumentID: &acmeID, ListingID: &acmeListingID, Identifiers: []types.Identifier{acmeISIN, acmeDescribed}},
			Quantity:  decimal.RequireFromString("-141"),
		},
		{
			StatedKey: gen.StatedKey{ID: gbxKeyID, InstrumentID: &gbpID, ListingID: &gbpListingID, Currency: ptr.To("GBX"), Identifiers: []types.Identifier{gbx}},
			Quantity:  decimal.RequireFromString("1209279"),
		},
	}
	wantInstruments := []*holdingv1.InstrumentHolding{
		{
			InstrumentId: gbpID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Quantity: "12092.79",
			Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"}},
			Listings:    []*holdingv1.HoldingListing{{Id: gbpListingID.String(), Currency: "GBP"}},
			Keys: []*holdingv1.HoldingKey{{
				ListingId: gbpListingID.String(), Quantity: "1209279",
				StatedKey: &typev1.StatedKey{Currency: ptr.To("GBX"), Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBX"}}},
			}},
		},
		{
			InstrumentId: acmeID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_SECURITY, Quantity: "-141",
			Name: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"},
			Identifiers: []*typev1.Identifier{
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"},
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_SEDOL, Value: "0263494"},
			},
			Listings: []*holdingv1.HoldingListing{{
				Id: acmeListingID.String(), Currency: "GBP", Venue: "LSE",
				Ticker: &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Domain: "XLON", Value: "ACME"},
			}},
			Keys: []*holdingv1.HoldingKey{{
				ListingId: acmeListingID.String(), Quantity: "-141",
				StatedKey: &typev1.StatedKey{Identifiers: []*typev1.Identifier{
					{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"},
					{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ACME PLC"},
				}},
			}},
		},
	}

	// Two keys of one group, sharing an ISIN and differing in every other
	// thing they state.
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "ACME"}
	ibkr := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME CORP"}
	schwab := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "schwab", Value: "ACME CORPORATION"}
	k1, k2 := uuid.MustParse("00000000-0000-0000-0000-000000000061"), uuid.MustParse("00000000-0000-0000-0000-000000000062")
	groupKeys := []gen.ListHoldingKeysRow{
		{StatedKey: gen.StatedKey{ID: k1, GroupID: &groupID, AssetClass: ptr.To(types.AssetClassEquity), Identifiers: []types.Identifier{ibkr, isin}}, Quantity: decimal.RequireFromString("10")},
		{StatedKey: gen.StatedKey{ID: k2, GroupID: &groupID, AssetClass: ptr.To(types.AssetClassSecurity), Identifiers: []types.Identifier{schwab, isin, ticker}}, Quantity: decimal.RequireFromString("2.5")},
	}
	protoISIN := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000001"}
	protoIbkr := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ACME CORP"}
	protoSchwab := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "schwab", Value: "ACME CORPORATION"}
	protoTicker := &typev1.Identifier{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Value: "ACME"}
	wantGroups := []*holdingv1.GroupHolding{{
		GroupId: groupID.String(), Quantity: "12.5",
		AssetClasses: []typev1.AssetClass{typev1.AssetClass_ASSET_CLASS_SECURITY, typev1.AssetClass_ASSET_CLASS_EQUITY},
		Identifiers:  []*typev1.Identifier{protoIbkr, protoISIN, protoSchwab, protoTicker},
		Keys: []*holdingv1.HoldingKey{
			{Quantity: "10", StatedKey: &typev1.StatedKey{AssetClass: typev1.AssetClass_ASSET_CLASS_EQUITY, Identifiers: []*typev1.Identifier{protoIbkr, protoISIN}}},
			{Quantity: "2.5", StatedKey: &typev1.StatedKey{AssetClass: typev1.AssetClass_ASSET_CLASS_SECURITY, Identifiers: []*typev1.Identifier{protoSchwab, protoISIN, protoTicker}}},
		},
	}}

	tests := []struct {
		name        string
		authErr     error
		instruments []gen.Instrument
		instrErr    error
		idents      []gen.Identifier
		identsErr   error
		listings    []gen.ListListingNamesRow
		listingsErr error
		names       []gen.InstrumentName
		namesErr    error
		keys        []gen.ListHoldingKeysRow
		keysErr     error
		want        []*holdingv1.InstrumentHolding
		wantGroups  []*holdingv1.GroupHolding
		wantCode    connect.Code
	}{
		{name: "none"},
		{name: "instruments only", instruments: instruments, idents: idents, listings: listings, names: names, keys: instrumentKeys, want: wantInstruments},
		{name: "groups only", keys: groupKeys, wantGroups: wantGroups},
		{
			name: "both kinds", instruments: instruments, idents: idents, listings: listings, names: names, keys: append(slices.Clone(instrumentKeys), groupKeys...),
			want: wantInstruments, wantGroups: wantGroups,
		},
		{
			name:        "zero sums are no holdings",
			instruments: instruments,
			keys:        []gen.ListHoldingKeysRow{withQuantity(instrumentKeys[1], "0"), withQuantity(groupKeys[0], "10"), withQuantity(groupKeys[1], "-10")},
		},
		{name: "keys failure", keysErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "instruments failure", keys: instrumentKeys, instrErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "identifiers failure", keys: instrumentKeys, instruments: instruments, identsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "listings failure", keys: instrumentKeys, instruments: instruments, idents: idents, listingsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "names failure", keys: instrumentKeys, instruments: instruments, idents: idents, listings: listings, namesErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			// Each read is offered once, and what the handler reads after a
			// failure does not matter to the response.
			f.reader.EXPECT().ListHoldingKeys(gomock.Any(), userID).Return(tc.keys, tc.keysErr).MaxTimes(1)
			f.reader.EXPECT().ListDatedTransactions(gomock.Any(), userID).Return(nil, nil).MaxTimes(1)
			f.reader.EXPECT().ListInstrumentsByID(gomock.Any(), gomock.Any()).Return(tc.instruments, tc.instrErr).MaxTimes(1)
			f.reader.EXPECT().ListIdentifiersOf(gomock.Any(), []uuid.UUID{gbpID, acmeID}).Return(tc.idents, tc.identsErr).MaxTimes(1)
			f.reader.EXPECT().ListListingNames(gomock.Any(), []uuid.UUID{gbpID, acmeID}).Return(tc.listings, tc.listingsErr).MaxTimes(1)
			f.reader.EXPECT().ListInstrumentNames(gomock.Any(), []uuid.UUID{gbpID, acmeID}).Return(tc.names, tc.namesErr).MaxTimes(1)
			res, err := f.client.ListHoldings(context.Background(), connect.NewRequest(&holdingv1.ListHoldingsRequest{}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("ListHoldings() code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			want := &holdingv1.ListHoldingsResponse{Instruments: tc.want, Groups: tc.wantGroups}
			if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListHoldings() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// withQuantity returns k with its sum set to quantity.
func withQuantity(k gen.ListHoldingKeysRow, quantity string) gen.ListHoldingKeysRow {
	k.Quantity = decimal.RequireFromString(quantity)
	return k
}

// TestListHoldingsScale checks that cash sums in its family's unit, so pence
// count as hundredths of a pound, and that a security's quantity stays in
// shares whatever its currency.
func TestListHoldingsScale(t *testing.T) {
	cash := func(id uuid.UUID, code, quantity string) gen.ListHoldingKeysRow {
		return gen.ListHoldingKeysRow{
			StatedKey: gen.StatedKey{ID: id, InstrumentID: &gbpID, Currency: ptr.To(code), Identifiers: []types.Identifier{{Type: types.IdentifierTypeCurrency, Value: code}}},
			Quantity:  decimal.RequireFromString(quantity),
		}
	}
	pounds, pence := uuid.MustParse("00000000-0000-0000-0000-000000000064"), uuid.MustParse("00000000-0000-0000-0000-000000000065")
	shares := gen.ListHoldingKeysRow{
		StatedKey: gen.StatedKey{ID: pence, InstrumentID: &acmeID, Currency: ptr.To("GBX"), Identifiers: []types.Identifier{{Type: types.IdentifierTypeIsin, Value: "GB0002634946"}}},
		Quantity:  decimal.RequireFromString("100"),
	}
	instruments := []gen.Instrument{{ID: gbpID, AssetClass: types.AssetClassCash}, {ID: acmeID, AssetClass: types.AssetClassStock}}
	tests := []struct {
		name string
		keys []gen.ListHoldingKeysRow
		id   uuid.UUID
		want string
	}{
		{name: "pounds and pence sum in pounds", keys: []gen.ListHoldingKeysRow{cash(pounds, "GBP", "5"), cash(pence, "GBX", "1000")}, id: gbpID, want: "15"},
		{name: "pounds and pence that cancel are no holding", keys: []gen.ListHoldingKeysRow{cash(pounds, "GBP", "-5"), cash(pence, "GBX", "500")}},
		{name: "a security quoted in pence counts shares", keys: []gen.ListHoldingKeysRow{shares}, id: acmeID, want: "100"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil)
			f.reader.EXPECT().ListHoldingKeys(gomock.Any(), userID).Return(tc.keys, nil)
			f.reader.EXPECT().ListDatedTransactions(gomock.Any(), userID).Return(nil, nil)
			f.reader.EXPECT().ListInstrumentsByID(gomock.Any(), gomock.Any()).Return(instruments, nil)
			if tc.want != "" {
				ids := []uuid.UUID{tc.id}
				f.reader.EXPECT().ListIdentifiersOf(gomock.Any(), ids).Return(nil, nil)
				f.reader.EXPECT().ListListingNames(gomock.Any(), ids).Return(nil, nil)
				f.reader.EXPECT().ListInstrumentNames(gomock.Any(), ids).Return(nil, nil)
			}
			res, err := f.client.ListHoldings(context.Background(), connect.NewRequest(&holdingv1.ListHoldingsRequest{}))
			if err != nil {
				t.Fatalf("ListHoldings() error = %v", err)
			}
			got := res.Msg.GetInstruments()
			switch {
			case tc.want == "" && len(got) != 0:
				t.Errorf("instruments = %v, want none", got)
			case tc.want != "" && (len(got) != 1 || got[0].GetInstrumentId() != tc.id.String() || got[0].GetQuantity() != tc.want):
				t.Errorf("instruments = %v, want %s of %s", got, tc.want, tc.id)
			}
		})
	}
}
