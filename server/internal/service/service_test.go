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

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	"github.com/leedenison/stonks/proto/admin/block/v1/adminblockv1connect"
	admindatasourcev1 "github.com/leedenison/stonks/proto/admin/datasource/v1"
	"github.com/leedenison/stonks/proto/admin/datasource/v1/admindatasourcev1connect"
	adminreplayv1 "github.com/leedenison/stonks/proto/admin/replay/v1"
	"github.com/leedenison/stonks/proto/admin/replay/v1/adminreplayv1connect"
	adminrunv1 "github.com/leedenison/stonks/proto/admin/run/v1"
	"github.com/leedenison/stonks/proto/admin/run/v1/adminrunv1connect"
	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	holdingv1 "github.com/leedenison/stonks/proto/holding/v1"
	"github.com/leedenison/stonks/proto/holding/v1/holdingv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service"
	"github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

// stub answers SignIn according to the token it is given, and records the
// principal GetSession, ListHoldings and ListRuns see. The other admin
// services are left unimplemented, so a call that reaches one answers
// Unimplemented.
type stub struct {
	adminblockv1connect.UnimplementedBlockServiceHandler
	admindatasourcev1connect.UnimplementedDatasourceServiceHandler
	adminreplayv1connect.UnimplementedReplayServiceHandler
	adminrunv1connect.UnimplementedRunServiceHandler
	authv1connect.UnimplementedAuthServiceHandler
	holdingv1connect.UnimplementedHoldingServiceHandler
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

func (s *stub) ListHoldings(ctx context.Context, _ *connect.Request[holdingv1.ListHoldingsRequest]) (*connect.Response[holdingv1.ListHoldingsResponse], error) {
	s.record(ctx)
	return connect.NewResponse(&holdingv1.ListHoldingsResponse{}), nil
}

func (s *stub) ListRuns(ctx context.Context, _ *connect.Request[adminrunv1.ListRunsRequest]) (*connect.Response[adminrunv1.ListRunsResponse], error) {
	s.record(ctx)
	return connect.NewResponse(&adminrunv1.ListRunsResponse{}), nil
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
		mux.Handle(adminblockv1connect.NewBlockServiceHandler(f.stub, opts...))
		mux.Handle(admindatasourcev1connect.NewDatasourceServiceHandler(f.stub, opts...))
		mux.Handle(adminreplayv1connect.NewReplayServiceHandler(f.stub, opts...))
		mux.Handle(adminrunv1connect.NewRunServiceHandler(f.stub, opts...))
		mux.Handle(authv1connect.NewAuthServiceHandler(f.stub, opts...))
		mux.Handle(holdingv1connect.NewHoldingServiceHandler(f.stub, opts...))
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
		{name: "required without cookie", call: listHoldings, wantCode: connect.CodeUnauthenticated},
		{name: "required with live session", cookie: "stonks_session=live", call: listHoldings, wantSeen: &live},
		{name: "required with dead session", cookie: "stonks_session=dead", call: listHoldings, wantCode: connect.CodeUnauthenticated},
		{name: "required with store failure", cookie: "stonks_session=broken", call: listHoldings, wantCode: connect.CodeInternal, wantLog: true, wantMsg: "internal error"},
		{name: "optional without cookie", call: getSession},
		{name: "optional with live session", cookie: "stonks_session=live", call: getSession, wantSeen: &live},
		{name: "optional with dead session", cookie: "stonks_session=dead", call: getSession},
		{name: "optional with store failure", cookie: "stonks_session=broken", call: getSession, wantCode: connect.CodeInternal, wantLog: true},
		{name: "malformed neighbour", cookie: `bad=";; \x01"; stonks_session=live`, call: listHoldings, wantSeen: &live},
		{name: "other cookie only", cookie: "other=live", call: listHoldings, wantCode: connect.CodeUnauthenticated},
		{name: "admin without cookie", call: listRuns, wantCode: connect.CodeUnauthenticated},
		{name: "admin with a user's session", cookie: "stonks_session=live", call: listRuns, wantCode: connect.CodePermissionDenied},
		{name: "admin with an administrator's session", cookie: "stonks_session=admin", call: listRuns, wantSeen: &admin},
		{name: "block service with a user's session", cookie: "stonks_session=live", call: listBlocks, wantCode: connect.CodePermissionDenied},
		{name: "datasource service with a user's session", cookie: "stonks_session=live", call: listDatasources, wantCode: connect.CodePermissionDenied},
		{name: "replay service with a user's session", cookie: "stonks_session=live", call: startReplay, wantCode: connect.CodePermissionDenied},
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

func listHoldings(f *fixture) error {
	client := holdingv1connect.NewHoldingServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.ListHoldings(context.Background(), connect.NewRequest(&holdingv1.ListHoldingsRequest{}))
	return err
}

func listRuns(f *fixture) error {
	client := adminrunv1connect.NewRunServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.ListRuns(context.Background(), connect.NewRequest(&adminrunv1.ListRunsRequest{}))
	return err
}

func listBlocks(f *fixture) error {
	client := adminblockv1connect.NewBlockServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.ListBlocks(context.Background(), connect.NewRequest(&adminblockv1.ListBlocksRequest{}))
	return err
}

func listDatasources(f *fixture) error {
	client := admindatasourcev1connect.NewDatasourceServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.ListDatasources(context.Background(), connect.NewRequest(&admindatasourcev1.ListDatasourcesRequest{}))
	return err
}

func startReplay(f *fixture) error {
	client := adminreplayv1connect.NewReplayServiceClient(f.srv.Client, f.srv.URL)
	_, err := client.StartReplay(context.Background(), connect.NewRequest(&adminreplayv1.StartReplayRequest{}))
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
