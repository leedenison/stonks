// Package auth turns a Google identity into a session and a session into a
// principal.
//
// A Google ID token is verified once, exchanged for a session held in Redis
// (see [session.go](session/session.go)), and discarded; nothing of it is
// stored. The session identifier is what the client keeps, in a cookie the
// service boundary sets and reads, and it carries no claims. Revocation is a
// delete. Redis is a hard dependency of the request path, not a cache: losing
// it signs everyone out.
//
// Every authorization decision is made in the service. The edge proxy routes
// and terminates TLS; it does not authenticate and injects no identity.
//
// A user is provisioned on first sight, in this order: by Google subject; by
// email, case-insensitively, binding the subject to that account; else
// created from the verified email and name.
//
// A principal is loaded from the users table on every authenticated request,
// so a role change or a deleted account takes effect at once.
//
// The package depends on the user queries, the session store and the token
// verifier only through the narrow interfaces it declares here.
package auth

//go:generate go tool mockgen -source=auth.go -destination=mock/auth_mock.go -package=mock

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

var (
	// ErrUnauthenticated is returned when a request carries no live session.
	ErrUnauthenticated = errors.New("unauthenticated")
	// ErrPermissionDenied is returned when the principal lacks the role.
	ErrPermissionDenied = errors.New("permission denied")
	// ErrNotAllowed is returned when an unknown email is not on the allowlist.
	ErrNotAllowed = errors.New("email not allowed")
)

// UserStore is the view of the user queries this package depends on.
type UserStore interface {
	CreateUser(ctx context.Context, arg gen.CreateUserParams) (gen.User, error)
	GetUser(ctx context.Context, id uuid.UUID) (gen.User, error)
	GetUserByGoogleSubject(ctx context.Context, googleSubject string) (gen.User, error)
	GetUserByEmail(ctx context.Context, email string) (gen.User, error)
	BindGoogleSubject(ctx context.Context, arg gen.BindGoogleSubjectParams) (gen.User, error)
}

var _ UserStore = (*gen.Queries)(nil)

// SessionStore is the view of the session store this package depends on.
type SessionStore interface {
	Create(ctx context.Context, userID uuid.UUID) (session.Session, error)
	Get(ctx context.Context, id string) (session.Session, error)
	Delete(ctx context.Context, id string) error
}

var _ SessionStore = (*session.Store)(nil)

// Verifier is the view of the token verifier this package depends on.
type Verifier interface {
	Verify(ctx context.Context, token string) (google.Claims, error)
}

var _ Verifier = (*google.Verifier)(nil)

// Principal is the authenticated caller of a request.
type Principal struct {
	User gen.User
	// SessionID is the identifier of the session the request carried.
	SessionID string
	// ExpiresAt is when the session lapses unless a later request extends it.
	ExpiresAt time.Time
}

type ctxKey struct{}

// WithPrincipal returns ctx carrying p.
func WithPrincipal(ctx context.Context, p Principal) context.Context {
	return context.WithValue(ctx, ctxKey{}, p)
}

// PrincipalFrom returns the principal ctx carries, if any.
func PrincipalFrom(ctx context.Context) (Principal, bool) {
	p, ok := ctx.Value(ctxKey{}).(Principal)
	return p, ok
}

// User returns the principal, or ErrUnauthenticated when there is none.
func User(ctx context.Context) (Principal, error) {
	p, ok := PrincipalFrom(ctx)
	if !ok {
		return Principal{}, ErrUnauthenticated
	}
	return p, nil
}

// Admin returns the principal when it holds the admin role, ErrUnauthenticated
// when there is none, and ErrPermissionDenied otherwise.
func Admin(ctx context.Context) (Principal, error) {
	p, err := User(ctx)
	if err != nil {
		return Principal{}, err
	}
	if p.User.Role != gen.UserRoleAdmin {
		return Principal{}, ErrPermissionDenied
	}
	return p, nil
}
