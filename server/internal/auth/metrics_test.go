package auth

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"

	"github.com/google/go-cmp/cmp"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/auth/google"
	"github.com/leedenison/stonks/server/internal/auth/session"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// reader is the package's one metric reader. The API binds an instrument to
// the first provider installed and ignores a second, so every case here reads
// this one. Delta temporality makes each collection report what the case just
// did rather than everything before it.
var reader = sdkmetric.NewManualReader(
	sdkmetric.WithTemporalitySelector(func(sdkmetric.InstrumentKind) metricdata.Temporality {
		return metricdata.DeltaTemporality
	}),
)

func TestMain(m *testing.M) {
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	os.Exit(m.Run())
}

// counts collects what has been recorded since the last collection, rendered
// as "<name>{<attribute>=<value>}" so a case states its expectation in one
// literal.
func counts(t *testing.T) map[string]int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	if err := reader.Collect(context.Background(), &rm); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	got := map[string]int64{}
	for _, scope := range rm.ScopeMetrics {
		for _, metric := range scope.Metrics {
			sum, ok := metric.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, dp := range sum.DataPoints {
				parts := make([]string, 0, dp.Attributes.Len())
				for _, kv := range dp.Attributes.ToSlice() {
					parts = append(parts, fmt.Sprintf("%s=%s", kv.Key, kv.Value.String()))
				}
				sort.Strings(parts)
				name := metric.Name
				if len(parts) > 0 {
					name = fmt.Sprintf("%s{%s}", name, joinComma(parts))
				}
				got[name] += dp.Value
			}
		}
	}
	return got
}

func joinComma(parts []string) string {
	out := ""
	for i, p := range parts {
		if i > 0 {
			out += ","
		}
		out += p
	}
	return out
}

func TestCounters(t *testing.T) {
	any := gomock.Any()
	bound := gen.User{ID: userID, GoogleSubject: &subject, Email: "one@example.com", Name: "One", Role: gen.UserRoleUser}
	live := session.Session{ID: "live", UserID: userID, ExpiresAt: expires}

	tests := []struct {
		name string
		set  func(m mocks)
		call func(a *Authenticator)
		want map[string]int64
	}{
		{
			name: "sign in, account found by subject",
			set: func(m mocks) {
				m.verifier.EXPECT().Verify(any, "token").Return(claims, nil)
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(live, nil)
			},
			call: func(a *Authenticator) { _, _ = a.SignIn(context.Background(), "token") },
			want: map[string]int64{
				"stonks.auth.sign_ins{outcome=success}":         1,
				"stonks.auth.users_provisioned{reason=subject}": 1,
				"stonks.session.creations":                      1,
			},
		},
		{
			name: "sign in, token does not verify",
			set: func(m mocks) {
				m.verifier.EXPECT().Verify(any, "token").Return(google.Claims{}, google.ErrInvalid)
			},
			call: func(a *Authenticator) { _, _ = a.SignIn(context.Background(), "token") },
			want: map[string]int64{"stonks.auth.sign_ins{outcome=invalid_token}": 1},
		},
		{
			name: "sign in, email off the allowlist",
			set: func(m mocks) {
				m.verifier.EXPECT().Verify(any, "token").Return(google.Claims{Subject: subject, Email: "one@elsewhere.org"}, nil)
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().GetUserByEmail(any, "one@elsewhere.org").Return(gen.User{}, db.ErrNotFound)
			},
			call: func(a *Authenticator) { _, _ = a.SignIn(context.Background(), "token") },
			want: map[string]int64{"stonks.auth.sign_ins{outcome=not_allowed}": 1},
		},
		{
			name: "sign in, user store fails",
			set: func(m mocks) {
				m.verifier.EXPECT().Verify(any, "token").Return(claims, nil)
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, errBoom)
			},
			call: func(a *Authenticator) { _, _ = a.SignIn(context.Background(), "token") },
			want: map[string]int64{"stonks.auth.sign_ins{outcome=error}": 1},
		},
		{
			name: "sign in, account matched by email",
			set: func(m mocks) {
				m.verifier.EXPECT().Verify(any, "token").Return(claims, nil)
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().GetUserByEmail(any, claims.Email).Return(bound, nil)
				m.users.EXPECT().BindGoogleSubject(any, any).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(live, nil)
			},
			call: func(a *Authenticator) { _, _ = a.SignIn(context.Background(), "token") },
			want: map[string]int64{
				"stonks.auth.sign_ins{outcome=success}":       1,
				"stonks.auth.users_provisioned{reason=email}": 1,
				"stonks.session.creations":                    1,
			},
		},
		{
			name: "sign in, account created",
			set: func(m mocks) {
				m.verifier.EXPECT().Verify(any, "token").Return(claims, nil)
				m.users.EXPECT().GetUserByGoogleSubject(any, subject).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().GetUserByEmail(any, claims.Email).Return(gen.User{}, db.ErrNotFound)
				m.users.EXPECT().CreateUser(any, any).Return(bound, nil)
				m.sessions.EXPECT().Create(any, userID).Return(live, nil)
			},
			call: func(a *Authenticator) { _, _ = a.SignIn(context.Background(), "token") },
			want: map[string]int64{
				"stonks.auth.sign_ins{outcome=success}":         1,
				"stonks.auth.users_provisioned{reason=created}": 1,
				"stonks.session.creations":                      1,
			},
		},
		{
			name: "authenticate, live session",
			set: func(m mocks) {
				m.sessions.EXPECT().Get(any, "live").Return(live, nil)
				m.users.EXPECT().GetUser(any, userID).Return(bound, nil)
			},
			call: func(a *Authenticator) { _, _ = a.Authenticate(context.Background(), "live") },
			want: map[string]int64{"stonks.session.lookups{result=live}": 1},
		},
		{
			name: "authenticate, no session",
			set: func(m mocks) {
				m.sessions.EXPECT().Get(any, "gone").Return(session.Session{}, session.ErrNotFound)
			},
			call: func(a *Authenticator) { _, _ = a.Authenticate(context.Background(), "gone") },
			want: map[string]int64{"stonks.session.lookups{result=not_found}": 1},
		},
		{
			name: "authenticate, user gone",
			set: func(m mocks) {
				m.sessions.EXPECT().Get(any, "live").Return(live, nil)
				m.users.EXPECT().GetUser(any, userID).Return(gen.User{}, db.ErrNotFound)
				m.sessions.EXPECT().Delete(any, "live").Return(nil)
			},
			call: func(a *Authenticator) { _, _ = a.Authenticate(context.Background(), "live") },
			want: map[string]int64{
				"stonks.session.lookups{result=user_gone}":   1,
				"stonks.session.deletions{reason=user_gone}": 1,
			},
		},
		{
			name: "authenticate, session store unreachable",
			set: func(m mocks) {
				m.sessions.EXPECT().Get(any, "live").Return(session.Session{}, errBoom)
			},
			call: func(a *Authenticator) { _, _ = a.Authenticate(context.Background(), "live") },
			want: map[string]int64{"stonks.session.lookups{result=error}": 1},
		},
		{
			name: "sign out",
			set: func(m mocks) {
				m.sessions.EXPECT().Delete(any, "live").Return(nil)
			},
			call: func(a *Authenticator) { _ = a.SignOut(context.Background(), "live") },
			want: map[string]int64{"stonks.session.deletions{reason=sign_out}": 1},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			counts(t) // discard what earlier cases recorded
			m := newMocks(t)
			tc.set(m)
			tc.call(m.authenticator())
			if diff := cmp.Diff(tc.want, counts(t)); diff != "" {
				t.Errorf("counters (-want +got):\n%s", diff)
			}
		})
	}
}
