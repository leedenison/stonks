package service_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"connectrpc.com/connect"
	"go.uber.org/mock/gomock"

	adminv1 "github.com/leedenison/stonks/proto/admin/v1"
	"github.com/leedenison/stonks/proto/admin/v1/adminv1connect"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	instrumentv1 "github.com/leedenison/stonks/proto/instrument/v1"
	"github.com/leedenison/stonks/proto/instrument/v1/instrumentv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service"
	"github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

// stub answers SignIn according to the token it is given, and records the
// principal GetSession, ListInstruments and ListRuns see.
type stub struct {
	adminv1connect.UnimplementedAdminServiceHandler
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

func (s *stub) ListRuns(ctx context.Context, _ *connect.Request[adminv1.ListRunsRequest]) (*connect.Response[adminv1.ListRunsResponse], error) {
	s.record(ctx)
	return connect.NewResponse(&adminv1.ListRunsResponse{}), nil
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
	srv   *servicetest.Server
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{log: &bytes.Buffer{}, authn: mock.NewMockAuthenticator(ctrl), stub: &stub{}}
	opts, err := service.HandlerOptions(slog.New(slog.NewTextHandler(f.log, nil)), f.authn)
	if err != nil {
		t.Fatalf("HandlerOptions() error = %v", err)
	}
	f.srv = servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(adminv1connect.NewAdminServiceHandler(f.stub, opts...))
		mux.Handle(authv1connect.NewAuthServiceHandler(f.stub, opts...))
		mux.Handle(instrumentv1connect.NewInstrumentServiceHandler(f.stub, opts...))
	})
	return f
}

func TestHandlerOptions(t *testing.T) {
	tests := []struct {
		name     string
		token    string
		wantCode connect.Code
		wantMsg  string
		wantLog  string
	}{
		{name: "ok", token: "token"},
		{name: "invalid request", token: "", wantCode: connect.CodeInvalidArgument},
		{name: "client fault", token: "missing", wantCode: connect.CodeNotFound, wantMsg: "no such"},
		{name: "server fault", token: "fault", wantCode: connect.CodeInternal, wantMsg: "internal error", wantLog: "boom"},
		{name: "panic", token: "panic", wantCode: connect.CodeInternal, wantMsg: "internal error", wantLog: "rpc panicked"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			client := authv1connect.NewAuthServiceClient(f.srv.Client, f.srv.URL)

			_, err := client.SignIn(context.Background(), connect.NewRequest(&authv1.SignInRequest{GoogleIdToken: tc.token}))
			switch {
			case tc.wantCode == 0 && err != nil:
				t.Errorf("SignIn(%q) error = %v, want nil", tc.token, err)
			case tc.wantCode != 0 && connect.CodeOf(err) != tc.wantCode:
				t.Errorf("SignIn(%q) code = %v (err %v), want %v", tc.token, connect.CodeOf(err), err, tc.wantCode)
			}
			if tc.wantMsg != "" && messageOf(err) != tc.wantMsg {
				t.Errorf("SignIn(%q) message = %q, want %q", tc.token, messageOf(err), tc.wantMsg)
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
	live := auth.Principal{User: gen.User{Email: "one@example.com", Role: gen.UserRoleUser}, SessionID: "live"}
	admin := auth.Principal{User: gen.User{Email: "admin@example.com", Role: gen.UserRoleAdmin}, SessionID: "admin"}
	tests := []struct {
		name     string
		cookie   string
		call     func(f *fixture) error
		wantCode connect.Code
		wantMsg  string
		wantSeen *auth.Principal
		wantLog  bool
	}{
		{name: "required without cookie", call: listInstruments, wantCode: connect.CodeUnauthenticated},
		{name: "required with live session", cookie: "stonks_session=live", call: listInstruments, wantSeen: &live},
		{name: "required with dead session", cookie: "stonks_session=dead", call: listInstruments, wantCode: connect.CodeUnauthenticated},
		{name: "required with store failure", cookie: "stonks_session=broken", call: listInstruments, wantCode: connect.CodeInternal, wantLog: true, wantMsg: "internal error"},
		{name: "optional without cookie", call: getSession},
		{name: "optional with live session", cookie: "stonks_session=live", call: getSession, wantSeen: &live},
		{name: "optional with dead session", cookie: "stonks_session=dead", call: getSession},
		{name: "optional with store failure", cookie: "stonks_session=broken", call: getSession, wantCode: connect.CodeInternal, wantLog: true},
		{name: "malformed neighbour", cookie: `bad=";; \x01"; stonks_session=live`, call: listInstruments, wantSeen: &live},
		{name: "other cookie only", cookie: "other=live", call: listInstruments, wantCode: connect.CodeUnauthenticated},
		{name: "admin without cookie", call: listRuns, wantCode: connect.CodeUnauthenticated},
		{name: "admin with a user's session", cookie: "stonks_session=live", call: listRuns, wantCode: connect.CodePermissionDenied},
		{name: "admin with an administrator's session", cookie: "stonks_session=admin", call: listRuns, wantSeen: &admin},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.authn.EXPECT().Authenticate(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, id string) (auth.Principal, error) {
				switch id {
				case "live":
					return live, nil
				case "admin":
					return admin, nil
				case "broken":
					return auth.Principal{}, errors.New("redis down")
				}
				return auth.Principal{}, auth.ErrUnauthenticated
			}).AnyTimes()
			f.srv.Cookie = tc.cookie

			err := tc.call(f)
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("call with cookie %q: code = %v (err %v), want %v", tc.cookie, connect.CodeOf(err), err, tc.wantCode)
			}
			if tc.wantMsg != "" && messageOf(err) != tc.wantMsg {
				t.Errorf("call with cookie %q: message = %q, want %q", tc.cookie, messageOf(err), tc.wantMsg)
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
			if tc.wantLog && !strings.Contains(f.log.String(), "redis down") {
				t.Errorf("log lacks the store's error:\n%s", f.log.String())
			}
		})
	}
}

func listInstruments(f *fixture) error {
	client := instrumentv1connect.NewInstrumentServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.ListInstruments(context.Background(), connect.NewRequest(&instrumentv1.ListInstrumentsRequest{}))
	return err
}

func listRuns(f *fixture) error {
	client := adminv1connect.NewAdminServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.ListRuns(context.Background(), connect.NewRequest(&adminv1.ListRunsRequest{}))
	return err
}

func getSession(f *fixture) error {
	client := authv1connect.NewAuthServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.GetSession(context.Background(), connect.NewRequest(&authv1.GetSessionRequest{}))
	return err
}

// messageOf is the message a client reads from err, without its code prefix.
func messageOf(err error) string {
	var cerr *connect.Error
	if errors.As(err, &cerr) {
		return cerr.Message()
	}
	return ""
}
