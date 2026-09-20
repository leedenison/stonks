// Package servicetest serves a handler through the real handler chain for a
// test, with a client that carries a session cookie.
package servicetest

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"connectrpc.com/connect"

	"github.com/leedenison/stonks/server/internal/service"
)

// Session is the session identifier the default cookie carries.
const Session = "session-1"

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
