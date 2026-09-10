package auth

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgconn"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/auth/allowlist"
	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/mock"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

var (
	subject = "subject-1"
	claims  = google.Claims{Subject: subject, Email: "One@example.com", Name: "One"}
	userID  = uuid.MustParse("00000000-0000-0000-0000-000000000001")
	expires = time.Date(2026, 9, 16, 12, 0, 0, 0, time.UTC)
	errBoom = errors.New("boom")
)

type mocks struct {
	verifier *mock.MockVerifier
	users    *mock.MockUserStore
	sessions *mock.MockSessionStore
}

func newMocks(t *testing.T) mocks {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	return mocks{
		verifier: mock.NewMockVerifier(ctrl),
		users:    mock.NewMockUserStore(ctrl),
		sessions: mock.NewMockSessionStore(ctrl),
	}
}

func (m mocks) authenticator() *Authenticator {
	return New(Options{
		Verifier: m.verifier,
		Users:    m.users,
		Sessions: m.sessions,
		Allowed:  allowlist.List{"*@example.com"},
	})
}

func TestSignIn(t *testing.T) {
	any := gomock.Any()
	bound := gen.User{ID: userID, GoogleSubject: &subject, Email: "one@example.com", Name: "One", Role: gen.UserRoleUser}
	sess := session.Session{ID: "session-1", UserID: userID, ExpiresAt: expires}

	tests := []struct {
		name    string
		claims  google.Claims
		expect  func(m mocks)
		want    Principal
		wantErr error
	}{
		{
			name:   "known subject",
			claims: claims,
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(sess, nil)
			},
			want: Principal{User: bound, SessionID: "session-1", ExpiresAt: expires},
		},
		{
			name:   "bound by email",
			claims: claims,
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				unbound := bound
				unbound.GoogleSubject = nil
				m.users.EXPECT().GetUserByEmail(any, "One@example.com").Return(unbound, nil)
				m.users.EXPECT().BindGoogleSubject(any, gen.BindGoogleSubjectParams{ID: userID, GoogleSubject: subject}).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(sess, nil)
			},
			want: Principal{User: bound, SessionID: "session-1", ExpiresAt: expires},
		},
		{
			name:   "created",
			claims: claims,
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().GetUserByEmail(any, "One@example.com").Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().CreateUser(any, gen.CreateUserParams{Email: "One@example.com", Name: "One", GoogleSubject: &subject, Role: gen.UserRoleUser}).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(sess, nil)
			},
			want: Principal{User: bound, SessionID: "session-1", ExpiresAt: expires},
		},
		{
			// Two sign-ins for one new account race; the insert that loses is
			// refused by a unique constraint and finds what the winner made.
			name:   "created by a concurrent sign-in",
			claims: claims,
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().GetUserByEmail(any, "One@example.com").Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().CreateUser(any, any).Return(gen.User{}, &pgconn.PgError{Code: "23505"})
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(sess, nil)
			},
			want: Principal{User: bound, SessionID: "session-1", ExpiresAt: expires},
		},
		{
			name:   "not allowed",
			claims: google.Claims{Subject: subject, Email: "one@example.org"},
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().GetUserByEmail(any, "one@example.org").Return(gen.User{}, db.ErrNotFound)
			},
			wantErr: ErrNotAllowed,
		},
		{
			name:   "existing user outside the allowlist",
			claims: google.Claims{Subject: subject, Email: "one@example.org"},
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(sess, nil)
			},
			want: Principal{User: bound, SessionID: "session-1", ExpiresAt: expires},
		},
		{
			name:   "store failure",
			claims: claims,
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, errBoom)
			},
			wantErr: errBoom,
		},
		{
			name:   "session failure",
			claims: claims,
			expect: func(m mocks) {
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(session.Session{}, errBoom)
			},
			wantErr: errBoom,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t)
			m.verifier.EXPECT().Verify(any, "token").Return(tc.claims, nil)
			tc.expect(m)
			got, err := m.authenticator().SignIn(context.Background(), "token")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("SignIn() error = %v, want %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("SignIn() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignInVerifyFailure(t *testing.T) {
	for _, want := range []error{google.ErrMalformed, google.ErrInvalid, google.ErrEmailUnverified, errBoom} {
		m := newMocks(t)
		m.verifier.EXPECT().Verify(gomock.Any(), "token").Return(google.Claims{}, want)
		if _, err := m.authenticator().SignIn(context.Background(), "token"); !errors.Is(err, want) {
			t.Errorf("SignIn() error = %v, want %v", err, want)
		}
	}
}

func TestAuthenticate(t *testing.T) {
	any := gomock.Any()
	user := gen.User{ID: userID, Email: "one@example.com", Role: gen.UserRoleUser}
	sess := session.Session{ID: "session-1", UserID: userID, ExpiresAt: expires}
	tests := []struct {
		name    string
		expect  func(m mocks)
		want    Principal
		wantErr error
	}{
		{
			name: "live",
			expect: func(m mocks) {
				m.sessions.EXPECT().Get(any, "session-1").Return(sess, nil)
				m.users.EXPECT().GetUser(any, userID).Return(user, nil)
			},
			want: Principal{User: user, SessionID: "session-1", ExpiresAt: expires},
		},
		{
			name: "no session",
			expect: func(m mocks) {
				m.sessions.EXPECT().Get(any, "session-1").Return(session.Session{}, session.ErrNotFound)
			},
			wantErr: ErrUnauthenticated,
		},
		{
			name: "user gone",
			expect: func(m mocks) {
				m.sessions.EXPECT().Get(any, "session-1").Return(sess, nil)
				m.users.EXPECT().GetUser(any, userID).Return(gen.User{}, db.ErrNotFound)
				m.sessions.EXPECT().Delete(any, "session-1").Return(nil)
			},
			wantErr: ErrUnauthenticated,
		},
		{
			name: "store failure",
			expect: func(m mocks) {
				m.sessions.EXPECT().Get(any, "session-1").Return(session.Session{}, errBoom)
			},
			wantErr: errBoom,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			m := newMocks(t)
			tc.expect(m)
			got, err := m.authenticator().Authenticate(context.Background(), "session-1")
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Authenticate() error = %v, want %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Authenticate() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestSignOut(t *testing.T) {
	m := newMocks(t)
	m.sessions.EXPECT().Delete(gomock.Any(), "session-1").Return(nil)
	if err := m.authenticator().SignOut(context.Background(), "session-1"); err != nil {
		t.Errorf("SignOut() error = %v", err)
	}
	m.sessions.EXPECT().Delete(gomock.Any(), "session-1").Return(errBoom)
	if err := m.authenticator().SignOut(context.Background(), "session-1"); !errors.Is(err, errBoom) {
		t.Errorf("SignOut() error = %v, want %v", err, errBoom)
	}
}
