package google

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"

	adminpricev1 "github.com/leedenison/stonks/proto/admin/price/v1"
	"github.com/leedenison/stonks/proto/admin/price/v1/adminpricev1connect"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
)

// service stands in for the Stonks service: sign-in and the price service.
type service struct {
	adminpricev1connect.UnimplementedPriceServiceHandler
	authv1connect.UnimplementedAuthServiceHandler
	// role is the role of the account that signs in.
	role authv1.Role
	// gaps is the answer of ListPriceGaps.
	gaps *adminpricev1.ListPriceGapsResponse
	// cookies holds the Cookie header of each price call.
	cookies []string
	// tokens holds the ID token of each sign-in.
	tokens []string
}

func (s *service) SignIn(_ context.Context, req *connect.Request[authv1.SignInRequest]) (*connect.Response[authv1.SignInResponse], error) {
	s.tokens = append(s.tokens, req.Msg.GetGoogleIdToken())
	res := connect.NewResponse(&authv1.SignInResponse{User: &authv1.User{Email: "admin@example.com", Role: s.role}})
	res.Header().Set("Set-Cookie", (&http.Cookie{Name: "stonks_session", Value: "session-1", Path: "/", HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}).String())
	return res, nil
}

func (s *service) ListPriceGaps(_ context.Context, req *connect.Request[adminpricev1.ListPriceGapsRequest]) (*connect.Response[adminpricev1.ListPriceGapsResponse], error) {
	s.cookies = append(s.cookies, req.Header().Get("Cookie"))
	return connect.NewResponse(s.gaps), nil
}

// serve mounts s and returns its address.
func (s *service) serve(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.Handle(adminpricev1connect.NewPriceServiceHandler(s))
	mux.Handle(authv1connect.NewAuthServiceHandler(s))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func TestSignIn(t *testing.T) {
	t.Run("an administrator's session rides on each call", func(t *testing.T) {
		svc := &service{role: authv1.Role_ROLE_ADMIN, gaps: &adminpricev1.ListPriceGapsResponse{}}
		srv := svc.serve(t)
		prices, err := SignIn(context.Background(), srv.Client(), srv.URL, "id-token")
		require.NoError(t, err)
		_, err = prices.ListPriceGaps(context.Background(), connect.NewRequest(&adminpricev1.ListPriceGapsRequest{}))
		require.NoError(t, err)
		if diff := cmp.Diff([]string{"id-token"}, svc.tokens); diff != "" {
			t.Errorf("sign-in tokens mismatch (-want +got):\n%s", diff)
		}
		if diff := cmp.Diff([]string{"stonks_session=session-1"}, svc.cookies); diff != "" {
			t.Errorf("cookies mismatch (-want +got):\n%s", diff)
		}
	})
	t.Run("an account without the admin role is refused", func(t *testing.T) {
		svc := &service{role: authv1.Role_ROLE_USER}
		srv := svc.serve(t)
		if _, err := SignIn(context.Background(), srv.Client(), srv.URL, "id-token"); err == nil || !strings.Contains(err.Error(), "not an administrator") {
			t.Errorf("SignIn() error = %v, want a refusal of the role", err)
		}
	})
	t.Run("a refused token is an error", func(t *testing.T) {
		srv := httptest.NewServer(http.NewServeMux())
		t.Cleanup(srv.Close)
		if _, err := SignIn(context.Background(), srv.Client(), srv.URL, "id-token"); err == nil {
			t.Error("SignIn() error = nil against a service without sign-in")
		}
	})
}
