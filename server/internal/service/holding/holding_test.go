package holding

import (
	"context"
	"errors"
	"net/http"
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
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/holding/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	gbpID     = uuid.MustParse("00000000-0000-0000-0000-000000000020")
	acmeID    = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	groupID   = uuid.MustParse("00000000-0000-0000-0000-000000000030")
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: servicetest.Session}
)

type fixture struct {
	reader *mock.MockReader
	authn  *servicemock.MockAuthenticator
	client holdingv1connect.HoldingServiceClient
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
		mux.Handle(holdingv1connect.NewHoldingServiceHandler(New(f.reader), opts...))
	})
	f.client = holdingv1connect.NewHoldingServiceClient(srv.Client, srv.URL)
	return f
}

func TestListHoldings(t *testing.T) {
	rows := []gen.ListInstrumentHoldingsRow{
		{InstrumentID: gbpID, AssetClass: gen.AssetClassCash, Quantity: decimal.RequireFromString("12092.79")},
		{InstrumentID: acmeID, AssetClass: gen.AssetClassSecurity, Quantity: decimal.RequireFromString("-141")},
	}
	idents := []gen.Identifier{
		{InstrumentID: gbpID, Type: types.IdentifierTypeCurrency, Value: "GBP"},
		{InstrumentID: acmeID, Type: types.IdentifierTypeIsin, Value: "GB0002634946"},
		{InstrumentID: acmeID, Type: types.IdentifierTypeSedol, Value: "0263494"},
	}
	wantInstruments := []*holdingv1.InstrumentHolding{
		{
			InstrumentId: gbpID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Quantity: "12092.79",
			Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"}},
		},
		{
			InstrumentId: acmeID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_SECURITY, Quantity: "-141",
			Identifiers: []*typev1.Identifier{
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"},
				{Type: typev1.IdentifierType_IDENTIFIER_TYPE_SEDOL, Value: "0263494"},
			},
		},
	}

	// Two keys of one group, sharing an ISIN and differing in every other
	// thing they state.
	groupRows := []gen.ListGroupHoldingsRow{{GroupID: groupID, Quantity: decimal.RequireFromString("12.5")}}
	isin := types.Identifier{Type: types.IdentifierTypeIsin, Value: "US0000000001"}
	ticker := types.Identifier{Type: types.IdentifierTypeMicTicker, Value: "ACME"}
	ibkr := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "ibkr", Value: "ACME CORP"}
	schwab := types.Identifier{Type: types.IdentifierTypeBrokerDescription, Domain: "schwab", Value: "ACME CORPORATION"}
	groupKeys := []gen.StatedKey{
		{GroupID: &groupID, AssetClass: ptr.To(gen.AssetClassEquity), Identifiers: []types.Identifier{ibkr, isin}},
		{GroupID: &groupID, AssetClass: ptr.To(gen.AssetClassSecurity), Identifiers: []types.Identifier{schwab, isin, ticker}},
	}
	wantGroups := []*holdingv1.GroupHolding{{
		GroupId: groupID.String(), Quantity: "12.5",
		AssetClasses: []typev1.AssetClass{typev1.AssetClass_ASSET_CLASS_SECURITY, typev1.AssetClass_ASSET_CLASS_EQUITY},
		Identifiers: []*typev1.Identifier{
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "ibkr", Value: "ACME CORP"},
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "US0000000001"},
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: "schwab", Value: "ACME CORPORATION"},
			{Type: typev1.IdentifierType_IDENTIFIER_TYPE_MIC_TICKER, Value: "ACME"},
		},
	}}

	tests := []struct {
		name       string
		authErr    error
		rows       []gen.ListInstrumentHoldingsRow
		rowsErr    error
		idents     []gen.Identifier
		identsErr  error
		groups     []gen.ListGroupHoldingsRow
		groupsErr  error
		keys       []gen.StatedKey
		keysErr    error
		want       []*holdingv1.InstrumentHolding
		wantGroups []*holdingv1.GroupHolding
		wantCode   connect.Code
	}{
		{name: "none"},
		{name: "instruments only", rows: rows, idents: idents, want: wantInstruments},
		{name: "groups only", groups: groupRows, keys: groupKeys, wantGroups: wantGroups},
		{
			name: "both kinds", rows: rows, idents: idents, groups: groupRows, keys: groupKeys,
			want: wantInstruments, wantGroups: wantGroups,
		},
		{name: "holdings failure", rowsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "identifiers failure", rows: rows, identsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "group holdings failure", groupsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "group keys failure", groups: groupRows, keysErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, tc.authErr)
			// Each read is offered once, and what the handler reads after a
			// failure does not matter to the response.
			f.reader.EXPECT().ListInstrumentHoldings(gomock.Any(), userID).Return(tc.rows, tc.rowsErr).MaxTimes(1)
			f.reader.EXPECT().ListIdentifiersOf(gomock.Any(), []uuid.UUID{gbpID, acmeID}).Return(tc.idents, tc.identsErr).MaxTimes(1)
			f.reader.EXPECT().ListGroupHoldings(gomock.Any(), userID).Return(tc.groups, tc.groupsErr).MaxTimes(1)
			keysArg := gen.ListStatedKeysOfGroupsParams{UserID: userID, GroupIds: []uuid.UUID{groupID}}
			f.reader.EXPECT().ListStatedKeysOfGroups(gomock.Any(), keysArg).Return(tc.keys, tc.keysErr).MaxTimes(1)
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
