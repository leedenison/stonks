package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/leedenison/stonks/server/internal/auth/allowlist"
	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Options are the dependencies and settings of an Authenticator.
type Options struct {
	Verifier Verifier
	Users    UserStore
	Sessions SessionStore
	// Allowed gates account creation.
	Allowed allowlist.List
}

// Authenticator signs users in and out and authenticates their sessions.
type Authenticator struct {
	o Options
}

// New returns an Authenticator.
func New(o Options) *Authenticator {
	return &Authenticator{o: o}
}

// SignIn verifies a Google ID token, provisions its user and starts a session.
func (a *Authenticator) SignIn(ctx context.Context, token string) (p Principal, err error) {
	defer func() { instr.signIn(ctx, signInOutcome(err)) }()

	c, err := a.o.Verifier.Verify(ctx, token)
	if err != nil {
		return Principal{}, fmt.Errorf("verify: %w", err)
	}
	u, err := a.provision(ctx, c)
	if err != nil {
		return Principal{}, err
	}
	sess, err := a.o.Sessions.Create(ctx, u.ID)
	if err != nil {
		return Principal{}, fmt.Errorf("create session: %w", err)
	}
	instr.created(ctx)
	return Principal{User: u, SessionID: sess.ID, ExpiresAt: sess.ExpiresAt}, nil
}

// Authenticate returns the principal of a live session, extending it, or
// ErrUnauthenticated when the session is unknown or its user is gone.
func (a *Authenticator) Authenticate(ctx context.Context, sessionID string) (Principal, error) {
	// Both refusals wrap ErrUnauthenticated, so the result is tracked here
	// rather than classified from the error the way a sign-in is.
	result := resultError
	defer func() { instr.lookup(ctx, result) }()

	sess, err := a.o.Sessions.Get(ctx, sessionID)
	if errors.Is(err, session.ErrNotFound) {
		result = resultNotFound
		return Principal{}, fmt.Errorf("%w: no session", ErrUnauthenticated)
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get session: %w", err)
	}
	u, err := a.o.Users.GetUser(ctx, sess.UserID)
	if errors.Is(err, db.ErrNotFound) {
		result = resultUserGone
		if err := a.o.Sessions.Delete(ctx, sessionID); err != nil {
			return Principal{}, fmt.Errorf("delete session of a missing user: %w", err)
		}
		instr.deleted(ctx, reasonUserGone)
		return Principal{}, fmt.Errorf("%w: user gone", ErrUnauthenticated)
	}
	if err != nil {
		return Principal{}, fmt.Errorf("get user: %w", err)
	}
	result = resultLive
	return Principal{User: u, SessionID: sess.ID, ExpiresAt: sess.ExpiresAt}, nil
}

// SignOut ends a session. An unknown session is not an error.
func (a *Authenticator) SignOut(ctx context.Context, sessionID string) error {
	if err := a.o.Sessions.Delete(ctx, sessionID); err != nil {
		return fmt.Errorf("sign out: %w", err)
	}
	instr.deleted(ctx, reasonSignOut)
	return nil
}

func (a *Authenticator) provision(ctx context.Context, c google.Claims) (gen.User, error) {
	u, err := a.lookup(ctx, c)
	switch {
	case err == nil:
		return u, nil
	case !errors.Is(err, db.ErrNotFound):
		return gen.User{}, err
	}
	if !a.o.Allowed.Match(c.Email) {
		return gen.User{}, ErrNotAllowed
	}
	subject := c.Subject
	u, err = a.o.Users.CreateUser(ctx, gen.CreateUserParams{Email: c.Email, Name: c.Name, GoogleSubject: &subject, Role: gen.UserRoleUser})
	switch {
	case db.IsConflict(err):
		// A concurrent sign-in for the same identity created the account
		// between the lookup and the insert, so it is there to be found now.
		u, err = a.lookup(ctx, c)
		if errors.Is(err, db.ErrNotFound) {
			return gen.User{}, fmt.Errorf("create user: refused by a constraint no account matches")
		}
		return u, err
	case err != nil:
		return gen.User{}, fmt.Errorf("create user: %w", err)
	}
	instr.provision(ctx, reasonCreated)
	return u, nil
}

// lookup returns the account the claims name, by Google subject and otherwise
// by email, binding the subject to an account reached by email. It returns
// db.ErrNotFound, unwrapped, when no account matches either.
func (a *Authenticator) lookup(ctx context.Context, c google.Claims) (gen.User, error) {
	u, err := a.o.Users.GetUserByGoogleSubject(ctx, c.Subject)
	switch {
	case err == nil:
		instr.provision(ctx, reasonSubject)
		return u, nil
	case !errors.Is(err, db.ErrNotFound):
		return gen.User{}, fmt.Errorf("get user by subject: %w", err)
	}
	u, err = a.o.Users.GetUserByEmail(ctx, c.Email)
	switch {
	case errors.Is(err, db.ErrNotFound):
		return gen.User{}, err
	case err != nil:
		return gen.User{}, fmt.Errorf("get user by email: %w", err)
	}
	u, err = a.o.Users.BindGoogleSubject(ctx, gen.BindGoogleSubjectParams{ID: u.ID, GoogleSubject: c.Subject})
	if err != nil {
		return gen.User{}, fmt.Errorf("bind subject: %w", err)
	}
	instr.provision(ctx, reasonEmail)
	return u, nil
}
