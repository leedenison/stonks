// Package auth serves stonks.auth.v1.
//
// SignIn answers with the session cookie, HttpOnly, SameSite=Lax, Path=/,
// Secure outside local development, and a Max-Age of the session's maximum
// lifetime; the server-side idle window governs whether it is still live.
// SignOut answers with the same cookie expired. A refused sign-in reveals
// nothing about whether an account exists.
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
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/service"
)

// Signer is the view of the authenticator this package depends on.
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
	res := connect.NewResponse(&authv1.SignInResponse{User: toProto(p.User), Session: &authv1.Session{ExpiresAt: timestamppb.New(p.ExpiresAt)}})
	res.Header().Set("Set-Cookie", s.cookie(p.SessionID, int(session.Max.Seconds())).String())
	return res, nil
}

// GetSession reports the session the request carries.
func (*Server) GetSession(ctx context.Context, _ *connect.Request[authv1.GetSessionRequest]) (*connect.Response[authv1.GetSessionResponse], error) {
	p, ok := auth.PrincipalFrom(ctx)
	if !ok {
		return connect.NewResponse(&authv1.GetSessionResponse{}), nil
	}
	return connect.NewResponse(&authv1.GetSessionResponse{User: toProto(p.User), Session: &authv1.Session{ExpiresAt: timestamppb.New(p.ExpiresAt)}}), nil
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

func toProto(u gen.User) *authv1.User {
	role := authv1.Role_ROLE_UNSPECIFIED
	switch u.Role {
	case gen.UserRoleUser:
		role = authv1.Role_ROLE_USER
	case gen.UserRoleAdmin:
		role = authv1.Role_ROLE_ADMIN
	}
	return &authv1.User{Id: u.ID.String(), Email: u.Email, Name: u.Name, Role: role}
}
