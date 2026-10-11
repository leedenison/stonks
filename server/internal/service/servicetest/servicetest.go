// Package servicetest serves a handler through the real handler chain for a
// test, with a client that carries a session cookie.
package servicetest

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
)

// Session is the session identifier the default cookie carries.
const Session = "session-1"

// Admin is the administrator an admin service test signs in as.
var Admin = auth.Principal{
	User:      gen.User{ID: uuid.MustParse("00000000-0000-0000-0000-000000000002"), Email: "admin@example.com", Role: gen.UserRoleAdmin},
	SessionID: Session,
}

// AsAdmin returns an authenticator that answers Session with Admin, as often
// as it is asked.
func AsAdmin(ctrl *gomock.Controller) service.Authenticator {
	authn := servicemock.NewMockAuthenticator(ctrl)
	authn.EXPECT().Authenticate(gomock.Any(), Session).Return(Admin, nil).AnyTimes()
	return authn
}

// Options returns handler options over authn that log nowhere.
func Options(t *testing.T, authn service.Authenticator) []connect.HandlerOption {
	t.Helper()
	opts, err := service.HandlerOptions(slog.New(slog.DiscardHandler), authn)
	if err != nil {
		t.Fatalf("HandlerOptions() error = %v", err)
	}
	return opts
}

// Transport sends Cookie as the raw Cookie header of every request, none
// when it is empty, and keeps the headers of the last response.
type Transport struct {
	Cookie string
	Res    http.Header
	Next   http.RoundTripper
}

// RoundTrip implements http.RoundTripper.
func (tr *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	if tr.Cookie != "" {
		req.Header.Set("Cookie", tr.Cookie)
	}
	res, err := tr.Next.RoundTrip(req)
	if err == nil {
		tr.Res = res.Header
	}
	return res, err
}

// Server is what mount put on a mux, served over loopback until the test
// ends. Its client's cookie names Session until a test changes it.
type Server struct {
	URL    string
	Client *http.Client
	*Transport
}

// Serve mounts handlers on a mux and serves it.
func Serve(t *testing.T, mount func(mux *http.ServeMux)) *Server {
	t.Helper()
	mux := http.NewServeMux()
	mount(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := srv.Client()
	tr := &Transport{Cookie: service.CookieName + "=" + Session, Next: client.Transport}
	client.Transport = tr
	return &Server{URL: srv.URL, Client: client, Transport: tr}
}

// CodeOf is connect.CodeOf with 0 for a nil error, so a wanted code of 0
// means success.
func CodeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}
