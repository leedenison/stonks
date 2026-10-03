// Package auth serves stonks.auth.v1.
package auth

import (
	"context"
	"errors"
	"net/http"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/timestamppb"

	authv1 "github.com/leedenison/stonks/proto/auth/v1"
	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/service"
)

// Signer is this package's view of the authenticator.
type Signer interface {
	SignIn(ctx context.Context, token string) (auth.Principal, error)
	SignOut(ctx context.Context, sessionID string) error
}

//go:generate go tool mockgen -source=auth.go -destination=mock/auth_mock.go -package=mock

// Server implements AuthService.
type Server struct {
	signer Signer
	secure bool
}

var _ authv1connect.AuthServiceHandler = (*Server)(nil)

// New returns a Server. secure marks the cookie Secure.
func New(signer Signer, secure bool) *Server {
	return &Server{signer: signer, secure: secure}
}

// SignIn exchanges a Google ID token for a session.
func (s *Server) SignIn(ctx context.Context, req *connect.Request[authv1.SignInRequest]) (*connect.Response[authv1.SignInResponse], error) {
	p, err := s.signer.SignIn(ctx, req.Msg.GetGoogleIdToken())
	if err != nil {
		return nil, signInError(err)
	}
	res := connect.NewResponse(&authv1.SignInResponse{User: to.ProtoUser(p.User), Session: &authv1.Session{ExpiresAt: timestamppb.New(p.ExpiresAt)}})
	res.Header().Set("Set-Cookie", s.cookie(p.SessionID, int(session.Max.Seconds())).String())
	return res, nil
}

// GetSession reports the session the request carries.
func (*Server) GetSession(ctx context.Context, _ *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return connect.NewResponse(&authv1.GetSessionResponse{}), nil
	}
	return connect.NewResponse(&authv1.GetSessionResponse{User: to.ProtoUser(p.User), Session: &authv1.Session{ExpiresAt: timestamppb.New(p.ExpiresAt)}}), nil
}

// SignOut ends the session the request carries and expires its cookie.
func (s *Server) SignOut(ctx context.Context, _ *connect.Request[authv1.SignOutRequest]) (*connect.Response[authv1.SignOutResponse], error) {
	if p, ok := auth.PrincipalFrom(ctx); ok {
		if err := s.signer.SignOut(ctx, p.SessionID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	res := connect.NewResponse(&authv1.SignOutResponse{})
	res.Header().Set("Set-Cookie", s.cookie("", -1).String())
	return res, nil
}

// cookie is the session cookie: HttpOnly, SameSite=Lax, Path=/ and Secure
// outside local development. SignIn sets it with a Max-Age of the session's
// maximum lifetime, and SignOut with -1 to expire it; the server-side idle
// window governs whether the session is still live.
func (s *Server) cookie(value string, maxAge int) *http.Cookie {
	return &http.Cookie{
		Name:     service.CookieName,
		Value:    value,
		Path:     "/",
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	}
}

// signInError maps a refused sign-in to an error that reveals nothing about
// whether an account exists.
func signInError(err error) error {
	switch {
	case errors.Is(err, google.ErrMalformed):
		return connect.NewError(connect.CodeInvalidArgument, errors.New("malformed token"))
	case errors.Is(err, google.ErrInvalid):
		return connect.NewError(connect.CodeUnauthenticated, errors.New("invalid token"))
	case errors.Is(err, google.ErrEmailUnverified), errors.Is(err, auth.ErrNotAllowed):
		return connect.NewError(connect.CodePermissionDenied, errors.New("email not permitted"))
	}
	return connect.NewError(connect.CodeInternal, err)
}
