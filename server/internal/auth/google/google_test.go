package google

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-cmp/cmp"

	"github.com/leedenison/stonks/server/internal/testutil/googletest"
)

const (
	clientID    = "client-id.apps.googleusercontent.com"
	cliClientID = "cli-client-id.apps.googleusercontent.com"
)

var (
	now   = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	other = googletest.NewKey()
)

func defaults() googletest.Token {
	return googletest.Valid(clientID, "one@example.com", now)
}

func newVerifier(j *googletest.JWKS, clock func() time.Time, ids ...string) *Verifier {
	if ids == nil {
		ids = []string{clientID}
	}
	return New(ids, WithHTTPClient(j.Client()), WithJWKSURL(j.URL), WithClock(clock))
}

func TestVerify(t *testing.T) {
	tests := []struct {
		name    string
		token   func(t *testing.T) string
		want    Claims
		wantErr error
	}{
		{
			name:  "valid",
			token: func(t *testing.T) string { return defaults().Sign(t) },
			want:  Claims{Subject: "subject-one@example.com", Email: "one@example.com", Name: "One"},
		},
		{
			name:  "bare issuer",
			token: func(t *testing.T) string { o := defaults(); o.Issuer = "accounts.google.com"; return o.Sign(t) },
			want:  Claims{Subject: "subject-one@example.com", Email: "one@example.com", Name: "One"},
		},
		{name: "garbage", token: func(*testing.T) string { return "not a token" }, wantErr: ErrMalformed},
		{name: "empty", token: func(*testing.T) string { return "" }, wantErr: ErrMalformed},
		{
			name:    "wrong key",
			token:   func(t *testing.T) string { o := defaults(); o.Key = other; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "unknown kid",
			token:   func(t *testing.T) string { o := defaults(); o.KID = "key-2"; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "no kid",
			token:   func(t *testing.T) string { o := defaults(); o.KID = ""; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "hmac",
			token:   func(t *testing.T) string { o := defaults(); o.Method = jwt.SigningMethodHS256; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "expired",
			token:   func(t *testing.T) string { o := defaults(); o.Expires = now.Add(-2 * time.Minute); return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "wrong audience",
			token:   func(t *testing.T) string { o := defaults(); o.Audience = "other-client"; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "empty audience",
			token:   func(t *testing.T) string { o := defaults(); o.Audience = ""; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "wrong issuer",
			token:   func(t *testing.T) string { o := defaults(); o.Issuer = "https://example.com"; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "no subject",
			token:   func(t *testing.T) string { o := defaults(); o.Subject = ""; return o.Sign(t) },
			wantErr: ErrInvalid,
		},
		{
			name:    "email unverified",
			token:   func(t *testing.T) string { o := defaults(); o.EmailVerified = false; return o.Sign(t) },
			wantErr: ErrEmailUnverified,
		},
		{
			name:    "no email",
			token:   func(t *testing.T) string { o := defaults(); o.Email = ""; return o.Sign(t) },
			wantErr: ErrEmailUnverified,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(googletest.NewJWKS(t), func() time.Time { return now })
			got, err := v.Verify(context.Background(), tc.token(t))
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("Verify() error = %v, want %v", err, tc.wantErr)
			}
			if diff := cmp.Diff(tc.want, got); diff != "" {
				t.Errorf("Verify() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

func TestVerifyAudiences(t *testing.T) {
	tests := []struct {
		name     string
		ids      []string
		audience string
		wantErr  error
	}{
		{name: "web client", ids: []string{clientID, cliClientID}, audience: clientID},
		{name: "command line client", ids: []string{clientID, cliClientID}, audience: cliClientID},
		{name: "a third client", ids: []string{clientID, cliClientID}, audience: "other-client", wantErr: ErrInvalid},
		{name: "command line client when only the web client is configured", ids: []string{clientID}, audience: cliClientID, wantErr: ErrInvalid},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(googletest.NewJWKS(t), func() time.Time { return now }, tc.ids...)
			_, err := v.Verify(context.Background(), googletest.Valid(tc.audience, "one@example.com", now).Sign(t))
			if !errors.Is(err, tc.wantErr) {
				t.Errorf("Verify() with audience %q error = %v, want %v", tc.audience, err, tc.wantErr)
			}
		})
	}
}

func TestVerifyFetchFailure(t *testing.T) {
	j := googletest.NewJWKS(t)
	j.Status = http.StatusInternalServerError
	v := newVerifier(j, func() time.Time { return now })
	_, err := v.Verify(context.Background(), defaults().Sign(t))
	if err == nil {
		t.Fatal("Verify() error = nil, want a fetch failure")
	}
	for _, sentinel := range []error{ErrMalformed, ErrInvalid, ErrEmailUnverified} {
		if errors.Is(err, sentinel) {
			t.Errorf("Verify() error = %v, want it not to match %v", err, sentinel)
		}
	}
}

func TestKeysetCaching(t *testing.T) {
	clock := now
	j := googletest.NewJWKS(t)
	j.Cache = "public, max-age=3600"
	v := newVerifier(j, func() time.Time { return clock })
	ctx := context.Background()
	valid := func() string { o := defaults(); o.Expires = now.Add(24 * time.Hour); return o.Sign(t) }()
	unknown := func() string {
		o := defaults()
		o.KID = "key-2"
		o.Expires = now.Add(24 * time.Hour)
		return o.Sign(t)
	}()

	steps := []struct {
		name        string
		advance     time.Duration
		token       string
		wantErr     error
		wantFetches int32
	}{
		{name: "first verify fetches", token: valid, wantFetches: 1},
		{name: "second verify is served from cache", token: valid, wantFetches: 1},
		{name: "unknown kid within a minute of a fetch does not refetch", advance: 30 * time.Second, token: unknown, wantErr: ErrInvalid, wantFetches: 1},
		{name: "unknown kid a minute after a fetch refetches", advance: 30 * time.Second, token: unknown, wantErr: ErrInvalid, wantFetches: 2},
		{name: "unknown kid again within a minute does not refetch", advance: 30 * time.Second, token: unknown, wantErr: ErrInvalid, wantFetches: 2},
		{name: "known kid within max-age is cached", advance: 30 * time.Minute, token: valid, wantFetches: 2},
		{name: "stale set refetches", advance: time.Hour, token: valid, wantFetches: 3},
	}
	for _, s := range steps {
		clock = clock.Add(s.advance)
		_, err := v.Verify(ctx, s.token)
		if !errors.Is(err, s.wantErr) {
			t.Fatalf("%s: Verify() error = %v, want %v", s.name, err, s.wantErr)
		}
		if got := j.Fetches.Load(); got != s.wantFetches {
			t.Errorf("%s: fetches = %d, want %d", s.name, got, s.wantFetches)
		}
	}
}

func TestMaxAge(t *testing.T) {
	tests := []struct {
		header string
		want   time.Duration
	}{
		{header: "", want: time.Hour},
		{header: "public, max-age=21600, must-revalidate", want: 6 * time.Hour},
		{header: "max-age=0", want: time.Hour},
		{header: "max-age=soon", want: time.Hour},
	}
	for _, tc := range tests {
		if got := maxAge(tc.header); got != tc.want {
			t.Errorf("maxAge(%q) = %v, want %v", tc.header, got, tc.want)
		}
	}
}
