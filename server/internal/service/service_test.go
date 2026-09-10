package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"go.uber.org/mock/gomock"

	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	instrumentv1 "github.com/leedenison/stonks/proto/instrument/v1"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service/mock"
)

// stub answers SignIn according to the token it is given, and records the
// principal GetSession and ListInstruments see.
type stub struct {
	authv1connect.UnimplementedAuthServiceHandler
	instrumentv1connect.UnimplementedInstrumentServiceHandler
	seen *auth.Principal
}

func (stub) SignIn(_ context.Context, req *connect.Request[authv1.SignInRequest]) (*connect.Response[authv1.SignInResponse], error) {
	switch req.Msg.GetGoogleIdToken() {
	case "panic":
		panic("boom")
	case "fault":
		return nil, connect.NewError(connect.CodeInternal, errors.New("boom"))
	case "missing":
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such"))
	}
	return connect.NewResponse(&authv1.SignInResponse{}), nil
}

func (s *stub) GetSession(ctx context.Context, _ *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	s.record(ctx)
	return connect.NewResponse(&authv1.GetSessionResponse{}), nil
}

func (s *stub) ListInstruments(ctx context.Context, _ *connect.Request[instrumentv1.ListInstrumentsRequest]) (*connect.Response[instrumentv1.ListInstrumentsResponse], error) {
	s.record(ctx)
	return connect.NewResponse(&instrumentv1.ListInstrumentsResponse{}), nil
}

func (s *stub) record(ctx context.Context) {
	s.seen = nil
	if p, ok := auth.PrincipalFrom(ctx); ok {
		s.seen = &p
	}
}

type fixture struct {
	log   *bytes.Buffer
	authn *mock.MockAuthenticator
	stub  *stub
	srv   *httptest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{log: &bytes.Buffer{}, authn: mock.NewMockAuthenticator(ctrl), stub: &stub{}}
	opts, err := HandlerOptions(slog.New(slog.NewTextHandler(f.log, nil)), f.authn)
	if err != nil {
		t.Fatalf("HandlerOptions() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle(authv1connect.NewAuthServiceHandler(f.stub, opts...))
	mux.Handle(instrumentv1connect.NewInstrumentServiceHandler(f.stub, opts...))
	f.srv = httptest.NewServer(mux)
	t.Cleanup(f.srv.Close)
	return f
}

func TestHandlerOptions(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode connect.Code
		wantLog  string
	}{
		{name: "ok", token: "token"},
		{name: "invalid request", token: "", wantCode: connect.CodeInvalidArgument},
		{name: "client fault", token: "missing", wantCode: connect.CodeNotFound},
		{name: "server fault", token: "fault", wantCode: connect.CodeInternal, wantLog: "rpc failed"},
		{name: "panic", token: "panic", wantCode: connect.CodeInternal, wantLog: "rpc panicked"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			client := authv1connect.NewAuthServiceClient(f.srv.Client(), f.srv.URL)

			_, err := client.SignIn(context.Background(), connect.NewRequest(&authv1.SignInRequest{GoogleIdToken: tc.token}))
			switch {
			case tc.wantCode == 0 && err != nil:
				t.Errorf("SignIn(%q) error = %v, want nil", tc.token, err)
			case tc.wantCode != 0 && connect.CodeOf(err) != tc.wantCode:
				t.Errorf("SignIn(%q) code = %v (err %v), want %v", tc.token, connect.CodeOf(err), err, tc.wantCode)
			}
			if tc.wantLog == "" {
				if f.log.Len() > 0 {
					t.Errorf("SignIn(%q) logged:\n%s", tc.token, f.log.String())
				}
				return
			}
			if n := strings.Count(f.log.String(), "level=ERROR"); n != 1 || !strings.Contains(f.log.String(), tc.wantLog) {
				t.Errorf("SignIn(%q) logged %d error lines, want one containing %q:\n%s", tc.token, n, tc.wantLog, f.log.String())
			}
		})
	}
}

func TestAuthenticate(t *testing.T) {
	live := auth.Principal{User: gen.User{Email: "one@example.com"}, SessionID: "live"}
	tests := []struct {
		name     string
		cookie   string
		call     func(f *fixture) error
		wantCode connect.Code
		wantSeen *auth.Principal
		wantLog  bool
	}{
		{name: "required without cookie", call: listInstruments, wantCode: connect.CodeUnauthenticated},
		{name: "required with live session", cookie: "stonks_session=live", call: listInstruments, wantSeen: &live},
		{name: "required with dead session", cookie: "stonks_session=dead", call: listInstruments, wantCode: connect.CodeUnauthenticated},
		{name: "required with store failure", cookie: "stonks_session=broken", call: listInstruments, wantCode: connect.CodeInternal, wantLog: true},
		{name: "optional without cookie", call: getSession},
		{name: "optional with live session", cookie: "stonks_session=live", call: getSession, wantSeen: &live},
		{name: "optional with dead session", cookie: "stonks_session=dead", call: getSession},
		{name: "optional with store failure", cookie: "stonks_session=broken", call: getSession, wantCode: connect.CodeInternal, wantLog: true},
		{name: "malformed neighbour", cookie: `bad=";; \x01"; stonks_session=live`, call: listInstruments, wantSeen: &live},
		{name: "other cookie only", cookie: "other=live", call: listInstruments, wantCode: connect.CodeUnauthenticated},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id string) (auth.Principal, error) {
				switch id {
				case "live":
					return live, nil
				case "broken":
					return auth.Principal{}, errors.New("redis down")
				}
				return auth.Principal{}, auth.ErrUnauthenticated
			}).AnyTimes()
			f.srv.Client().Transport = &cookieTransport{cookie: tc.cookie, next: f.srv.Client().Transport}

			err := tc.call(f)
			if codeOf(err) != tc.wantCode {
				t.Errorf("call with cookie %q: code = %v (err %v), want %v", tc.cookie, connect.CodeOf(err), err, tc.wantCode)
			}
			switch {
			case tc.wantSeen == nil && f.stub.seen != nil:
				t.Errorf("handler saw principal %+v, want none", *f.stub.seen)
			case tc.wantSeen != nil && (f.stub.seen == nil || f.stub.seen.SessionID != tc.wantSeen.SessionID):
				t.Errorf("handler saw principal %+v, want %+v", f.stub.seen, *tc.wantSeen)
			}
			if got := strings.Count(f.log.String(), "level=ERROR"); (got == 1) != tc.wantLog {
				t.Errorf("logged %d error lines, want logged %v:\n%s", got, tc.wantLog, f.log.String())
			}
		})
	}
}

func listInstruments(f *fixture) error {
	client := instrumentv1connect.NewInstrumentServiceClient(f.srv.Client(), f.srv.URL)
	_, err := client.ListInstruments(context.Background(), connect.NewRequest(&instrumentv1.ListInstrumentsRequest{}))
	return err
}

func getSession(f *fixture) error {
	client := authv1connect.NewAuthServiceClient(f.srv.Client(), f.srv.URL)
	_, err := client.GetSession(context.Background(), connect.NewRequest(&authv1.GetSessionRequest{}))
	return err
}

// cookieTransport sends a raw Cookie header with every request.
type cookieTransport struct {
	cookie string
	next   http.RoundTripper
}

func (c *cookieTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if c.cookie != "" {
		req.Header.Set("Cookie", c.cookie)
	}
	return c.next.RoundTrip(req)
}

// codeOf is connect.CodeOf with 0 for success, so a want of 0 means no error.
func codeOf(err error) connect.Code {
	if err == nil {
		return 0
	}
	return connect.CodeOf(err)
}
