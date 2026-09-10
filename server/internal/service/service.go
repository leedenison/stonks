// Package service is the boundary between the API and the rest of the server.
//
// Handlers are the net/http handlers Connect generates, serving the Connect,
// gRPC and gRPC-Web protocols on one endpoint, so a cookie is a plain HTTP
// header in both directions. Each proto package has one handler package below
// this one. A handler translates the sentinel errors of the packages it calls
// into Connect codes; nothing below this boundary imports connect.
//
// HandlerOptions gives every handler one chain, outermost first: the RPC is
// traced and timed, and its span records whatever code the caller finally
// saw. Tracing is outermost because that is the only position that sees a
// refusal the chain itself decided, such as one from authentication or from
// request validation. Only gRPC reflection is left untraced.
//
// Authentication reads the session cookie, CookieName, and applies a policy
// by procedure. The default is that a live session is required, so a new RPC
// is protected unless it is exempted here: SignIn needs no session, GetSession
// and SignOut tolerate its absence, and gRPC reflection is skipped. A live
// session puts its principal in the context, where [auth.go](../auth/auth.go)
// reads it; a missing or dead one on a protected RPC is refused as
// unauthenticated, and a failure to reach the session store is an internal
// error. /healthz is served outside the chain.
package service

//go:generate go tool mockgen -source=service.go -destination=mock/service_mock.go -package=mock

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"connectrpc.com/connect"
	"connectrpc.com/otelconnect"
	"connectrpc.com/validate"

	"github.com/leedenison/stonks/proto/auth/v1/authv1connect"
	"github.com/leedenison/stonks/server/internal/auth"
)

// CookieName is the cookie carrying the session identifier.
const CookieName = "stonks_session"

// Authenticator resolves a session identifier to its principal.
type Authenticator interface {
	Authenticate(ctx context.Context, sessionID string) (auth.Principal, error)
}

// HandlerOptions returns the options every handler is mounted with. It fails
// only if the telemetry interceptor cannot build its instruments.
func HandlerOptions(log *slog.Logger, authn Authenticator) ([]connect.HandlerOption, error) {
	traceRPC, err := otelconnect.NewInterceptor(
		// The client's address and port would otherwise be an attribute of
		// every metric, one series per connection.
		otelconnect.WithoutServerPeerAttributes(),
		otelconnect.WithFilter(func(_ context.Context, spec connect.Spec) bool {
			return !isReflection(spec.Procedure)
		}),
	)
	if err != nil {
		return nil, fmt.Errorf("telemetry interceptor: %w", err)
	}
	return []connect.HandlerOption{
		connect.WithRecover(recoverPanic(log)),
		connect.WithInterceptors(traceRPC, logErrors(log), &authenticate{authn: authn}, validate.NewInterceptor()),
	}, nil
}

type policy int

const (
	required policy = iota
	optional
	none
)

func policyFor(procedure string) policy {
	switch procedure {
	case authv1connect.AuthServiceSignInProcedure:
		return none
	case authv1connect.AuthServiceGetSessionProcedure, authv1connect.AuthServiceSignOutProcedure:
		return optional
	}
	if isReflection(procedure) {
		return none
	}
	return required
}

// isReflection reports whether procedure belongs to gRPC reflection, which
// carries no session and is not traced.
func isReflection(procedure string) bool {
	return strings.HasPrefix(procedure, "/grpc.reflection.")
}

// sessionID returns the value of the session cookie in h, or "". A malformed
// neighbouring cookie is skipped rather than failing the lookup.
func sessionID(h http.Header) string {
	c, err := (&http.Request{Header: h}).Cookie(CookieName)
	if err != nil {
		return ""
	}
	return c.Value
}

// authenticate applies the policy to unary and streaming handlers alike.
type authenticate struct {
	authn Authenticator
}

func (a *authenticate) WrapUnary(next connect.UnaryFunc) connect.UnaryFunc {
	return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
		ctx, err := a.apply(ctx, req.Spec().Procedure, req.Header())
		if err != nil {
			return nil, err
		}
		return next(ctx, req)
	}
}

func (*authenticate) WrapStreamingClient(next connect.StreamingClientFunc) connect.StreamingClientFunc {
	return next
}

func (a *authenticate) WrapStreamingHandler(next connect.StreamingHandlerFunc) connect.StreamingHandlerFunc {
	return func(ctx context.Context, conn connect.StreamingHandlerConn) error {
		ctx, err := a.apply(ctx, conn.Spec().Procedure, conn.RequestHeader())
		if err != nil {
			return err
		}
		return next(ctx, conn)
	}
}

func (a *authenticate) apply(ctx context.Context, procedure string, h http.Header) (context.Context, error) {
	pol := policyFor(procedure)
	if pol == none {
		return ctx, nil
	}
	if id := sessionID(h); id != "" {
		p, err := a.authn.Authenticate(ctx, id)
		switch {
		case err == nil:
			return auth.WithPrincipal(ctx, p), nil
		case !errors.Is(err, auth.ErrUnauthenticated):
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	if pol == required {
		return nil, connect.NewError(connect.CodeUnauthenticated, errors.New("session required"))
	}
	return ctx, nil
}

// logErrors logs a failure whose code a client cannot have caused.
func logErrors(log *slog.Logger) connect.UnaryInterceptorFunc {
	return func(next connect.UnaryFunc) connect.UnaryFunc {
		return func(ctx context.Context, req connect.AnyRequest) (connect.AnyResponse, error) {
			res, err := next(ctx, req)
			if err != nil && serverFault(connect.CodeOf(err)) {
				log.ErrorContext(ctx, "rpc failed",
					"procedure", req.Spec().Procedure,
					"code", connect.CodeOf(err).String(),
					"err", err)
			}
			return res, err
		}
	}
}

func serverFault(c connect.Code) bool {
	switch c {
	case connect.CodeInternal, connect.CodeUnknown, connect.CodeUnavailable, connect.CodeDataLoss:
		return true
	default:
		return false
	}
}

func recoverPanic(log *slog.Logger) func(context.Context, connect.Spec, http.Header, any) error {
	return func(ctx context.Context, spec connect.Spec, _ http.Header, p any) error {
		log.ErrorContext(ctx, "rpc panicked", "procedure", spec.Procedure, "panic", p)
		return connect.NewError(connect.CodeInternal, errors.New("internal error"))
	}
}
