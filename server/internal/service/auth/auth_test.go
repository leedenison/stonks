package auth

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service"
	"github.com/leedenison/stonks/server/internal/service/auth/mock"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
)

var (
	userID    = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	expires   = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	user      = gen.User{ID: userID, Email: "one@example.com", Name: "One", Role: gen.UserRoleAdmin}
	principal = auth.Principal{User: user, SessionID: "session-1", ExpiresAt: expires}
	protoUser = &authv1.User{Id: userID.String(), Email: "one@example.com", Name: "One", Role: authv1.Role_ROLE_ADMIN}
)

type fixture struct {
	signer *mock.MockSigner
	authn  *servicemock.MockAuthenticator
	client authv1connect.AuthServiceClient
	res    http.Header
}

// newFixture mounts a Server with the real handler chain and a client that
// records the headers of its last response.
func newFixture(t *testing.T, secure bool) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{signer: mock.NewMockSigner(ctrl), authn: servicemock.NewMockAuthenticator(ctrl)}
	mux := http.NewServeMux()
	log := slog.New(slog.DiscardHandler)
	opts, err := service.HandlerOptions(log, f.authn)
	if err != nil {
		t.Fatalf("HandlerOptions() error = %v", err)
	}
	mux.Handle(authv1connect.NewAuthServiceHandler(New(f.signer, secure), opts...))
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.Transport = &recorder{f: f, next: client.Transport}
	f.client = authv1connect.NewAuthServiceClient(client, srv.URL)
	return f
}

type recorder struct {
	f    *fixture
	next http.RoundTripper
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	req.Header.Set("Cookie", service.CookieName+"=session-1")
	res, err := r.next.RoundTrip(req)
	if err == nil {
		r.f.res = res.Header
	}
	return res, err
}

func (f *fixture) cookie(t *testing.T) *http.Cookie {
	t.Helper()
	c, err := http.ParseSetCookie(f.res.Get("Set-Cookie"))
	if err != nil {
		t.Fatalf("Set-Cookie %q: %v", f.res.Get("Set-Cookie"), err)
	}
	return c
}

func TestSignIn(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode connect.Code
	}{
		{name: "ok"},
		{name: "malformed", err: google.ErrMalformed, wantCode: connect.CodeInvalidArgument},
		{name: "invalid", err: google.ErrInvalid, wantCode: connect.CodeUnauthenticated},
		{name: "unverified email", err: google.ErrEmailUnverified, wantCode: connect.CodePermissionDenied},
		{name: "not allowed", err: auth.ErrNotAllowed, wantCode: connect.CodePermissionDenied},
		{name: "failure", err: errors.New("boom"), wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			f.signer.EXPECT().SignIn(gomock.Any(), "token").Return(principal, tc.err)
			res, err := f.client.SignIn(context.Background(), connect.NewRequest(&authv1.SignInRequest{GoogleIdToken: "token"}))
			if codeOf(err) != tc.wantCode {
				t.Fatalf("SignIn() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				if got := f.res.Get("Set-Cookie"); got != "" {
					t.Errorf("SignIn() failed but set cookie %q", got)
				}
				return
			}
			want := &authv1.SignInResponse{User: protoUser, Session: &authv1.Session{ExpiresAt: timestamppb.New(expires)}}
			if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("SignIn() mismatch (-want +got):\n%s", diff)
			}
			got := f.cookie(t)
			wantCookie := &http.Cookie{Name: service.CookieName, Value: "session-1", Path: "/", MaxAge: int(session.Max.Seconds()), HttpOnly: true, Secure: true, SameSite: http.SameSiteLaxMode}
			if diff := cmp.Diff(wantCookie, got, cmp.FilterPath(func(p cmp.Path) bool { return p.Last().String() == ".Raw" }, cmp.Ignore())); diff != "" {
				t.Errorf("SignIn() cookie mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignInInsecure(t *testing.T) {
	f := newFixture(t, false)
	f.signer.EXPECT().SignIn(gomock.Any(), "token").Return(principal, nil)
	if _, err := f.client.SignIn(context.Background(), connect.NewRequest(&authv1.SignInRequest{GoogleIdToken: "token"})); err != nil {
		t.Fatalf("SignIn() error = %v", err)
	}
	if f.cookie(t).Secure {
		t.Error("SignIn() cookie is Secure, want not")
	}
}

func TestGetSession(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want *authv1.GetSessionResponse
	}{
		{name: "live", want: &authv1.GetSessionResponse{User: protoUser, Session: &authv1.Session{ExpiresAt: timestamppb.New(expires)}}},
		{name: "none", err: auth.ErrUnauthenticated, want: &authv1.GetSessionResponse{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			f.authn.EXPECT().Authenticate(gomock.Any(), "session-1").Return(principal, tc.err)
			res, err := f.client.GetSession(context.Background(), connect.NewRequest(&authv1.GetSessionRequest{}))
			if err != nil {
				t.Fatalf("GetSession() error = %v", err)
			}
			if diff := cmp.Diff(tc.want, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("GetSession() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignOut(t *testing.T) {
	tests := []struct {
		name     string
		authErr  error
		expect   func(f *fixture)
		wantCode connect.Code
	}{
		{name: "live", expect: func(f *fixture) { f.signer.EXPECT().SignOut(gomock.Any(), "session-1").Return(nil) }},
		{name: "none", authErr: auth.ErrUnauthenticated, expect: func(*fixture) {}},
		{name: "failure", expect: func(f *fixture) { f.signer.EXPECT().SignOut(gomock.Any(), "session-1").Return(errors.New("boom")) }, wantCode: connect.CodeInternal},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, true)
			f.authn.EXPECT().Authenticate(gomock.Any(), "session-1").Return(principal, tc.authErr)
			tc.expect(f)
			_, err := f.client.SignOut(context.Background(), connect.NewRequest(&authv1.SignOutRequest{}))
			if codeOf(err) != tc.wantCode {
				t.Fatalf("SignOut() code = %v (err %v), want %v", connect.CodeOf(err), err, tc.wantCode)
			}
			if err != nil {
				return
			}
			got := f.cookie(t)
			if got.Value != "" || got.MaxAge != -1 || !got.HttpOnly || !got.Secure || got.SameSite != http.SameSiteLaxMode || got.Path != "/" {
				t.Errorf("SignOut() cookie = %q, want an expired one with the same attributes", got.String())
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
