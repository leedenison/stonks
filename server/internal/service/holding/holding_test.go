package holding

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
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
	"github.com/leedenison/stonks/server/internal/service"
	"github.com/leedenison/stonks/server/internal/service/holding/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	gbpID     = uuid.MustParse("00000000-0000-0000-0000-000000000020")
	acmeID    = uuid.MustParse("00000000-0000-0000-0000-000000000021")
	principal = auth.Principal{User: gen.User{ID: userID, Email: "one@example.com"}, SessionID: "session-1"}
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
	opts, err := service.HandlerOptions(slog.New(slog.DiscardHandler), f.authn)
	if err != nil {
		t.Fatalf("HandlerOptions() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle(holdingv1connect.NewHoldingServiceHandler(New(f.reader), opts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.Transport = &cookieTransport{next: client.Transport}
	f.client = holdingv1connect.NewHoldingServiceClient(client, srv.URL)
	return f
}

type cookieTransport struct {
	next http.RoundTripper
}

func (c *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Cookie", service.CookieName+"=session-1")
	return c.next.RoundTrip(req)
}

func TestListHoldings(t *testing.T) {
	dom := "fidelity_uk/upload"
	rows := []gen.ListHoldingsRow{
		{InstrumentID: gbpID, AssetClass: gen.AssetClassCash, Quantity: decimal.RequireFromString("12092.79")},
		{InstrumentID: acmeID, AssetClass: gen.AssetClassSecurity, Quantity: decimal.RequireFromString("-141")},
	}
	idents := []gen.Identifier{
		{InstrumentID: gbpID, Type: gen.IdentifierTypeCurrency, Value: "GBP"},
		{InstrumentID: acmeID, Type: gen.IdentifierTypeIsin, Value: "GB0002634946"},
		{InstrumentID: acmeID, Type: gen.IdentifierTypeBrokerDescription, Domain: &dom, Value: "ACME CORP", OwnerID: &userID},
	}
	tests := []struct {
		name      string
		authErr   error
		rows      []gen.ListHoldingsRow
		rowsErr   error
		idents    []gen.Identifier
		identsErr error
		want      []*holdingv1.Holding
		wantCode  connect.Code
	}{
		{name: "none"},
		{
			name:   "cash and a security",
			rows:   rows,
			idents: idents,
			want: []*holdingv1.Holding{
				{
					InstrumentId: gbpID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_CASH, Quantity: "12092.79",
					Identifiers: []*typev1.Identifier{{Type: typev1.IdentifierType_IDENTIFIER_TYPE_CURRENCY, Value: "GBP"}},
				},
				{
					InstrumentId: acmeID.String(), AssetClass: typev1.AssetClass_ASSET_CLASS_SECURITY, Quantity: "-141",
					Identifiers: []*typev1.Identifier{
						{Type: typev1.IdentifierType_IDENTIFIER_TYPE_ISIN, Value: "GB0002634946"},
						{Type: typev1.IdentifierType_IDENTIFIER_TYPE_BROKER_DESCRIPTION, Domain: &dom, Value: "ACME CORP"},
					},
				},
			},
		},
		{name: "holdings failure", rowsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "identifiers failure", rows: rows, identsErr: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "unauthenticated", authErr: auth.ErrUnauthenticated, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), "session-1").Return(principal, tc.authErr)
			if tc.authErr == nil {
				f.reader.EXPECT().ListHoldings(gomock.Any(), userID).Return(tc.rows, tc.rowsErr)
			}
			if tc.authErr == nil && tc.rowsErr == nil && len(tc.rows) > 0 {
				f.reader.EXPECT().ListHeldIdentifiers(gomock.Any(), userID).Return(tc.idents, tc.identsErr)
			}
			res, err := f.client.ListHoldings(context.Background(), connect.NewRequest(&holdingv1.ListHoldingsRequest{}))
			if codeOf(err) != tc.wantCode {
				t.Fatalf("ListHoldings() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			if diff := cmp.Diff(&holdingv1.ListHoldingsResponse{Holdings: tc.want}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("ListHoldings() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// codeOf is connect.CodeOf with 0 for success, so a want of 0 means no error.
func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}
