package google

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/go-cmp/cmp"
	"github.com/stretchr/testify/require"
)

const (
	clientID = "client-id.apps.googleusercontent.com"
	kid      = "key-1"
)

var (
	now    = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)
	signer = mustKey()
	other  = mustKey()
)

func mustKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}

// jwks serves signer's public key under kid and counts the fetches. Its
// status and Cache-Control header are settable per test.
type jwks struct {
	*httptest.Server
	fetches atomic.Int32
	status  int
	cache   string
}

func newJWKS(t *testing.T) *jwks {
	t.Helper()
	j := &jwks{status: http.StatusOK}
	j.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		j.fetches.Add(1)
		if j.status != http.StatusOK {
			w.WriteHeader(j.status)
			return
		}
		if j.cache != "" {
			w.Header().Set("Cache-Control", j.cache)
		}
		pub := signer.PublicKey
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": kid,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}}))
	}))
	t.Cleanup(j.Close)
	return j
}

type tokenOpts struct {
	key           *rsa.PrivateKey
	kid           string
	method        jwt.SigningMethod
	issuer        string
	audience      string
	subject       string
	email         string
	emailVerified bool
	expires       time.Time
}

func defaults() tokenOpts {
	return tokenOpts{
		key:           signer,
		kid:           kid,
		method:        jwt.SigningMethodRS256,
		issuer:        "https://accounts.google.com",
		audience:      clientID,
		subject:       "subject-1",
		email:         "one@example.com",
		emailVerified: true,
		expires:       now.Add(time.Hour),
	}
}

func sign(t *testing.T, o tokenOpts) string {
	t.Helper()
	tok := jwt.NewWithClaims(o.method, claims{
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    o.issuer,
			Subject:   o.subject,
			Audience:  jwt.ClaimStrings{o.audience},
			ExpiresAt: jwt.NewNumericDate(o.expires),
			IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
		},
		Email:         o.email,
		EmailVerified: o.emailVerified,
		Name:          "One",
	})
	tok.Header["kid"] = o.kid
	var key any = o.key
	if o.method == jwt.SigningMethodHS256 {
		key = []byte("secret")
	}
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}

func newVerifier(j *jwks, clock func() time.Time) *Verifier {
	return New(clientID, WithHTTPClient(j.Client()), WithJWKSURL(j.URL), WithClock(clock))
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
			token: func(t *testing.T) string { return sign(t, defaults()) },
			want:  Claims{Subject: "subject-1", Email: "one@example.com", Name: "One"},
		},
		{
			name:  "bare issuer",
			token: func(t *testing.T) string { o := defaults(); o.issuer = "accounts.google.com"; return sign(t, o) },
			want:  Claims{Subject: "subject-1", Email: "one@example.com", Name: "One"},
		},
		{name: "garbage", token: func(*testing.T) string { return "not a token" }, wantErr: ErrMalformed},
		{name: "empty", token: func(*testing.T) string { return "" }, wantErr: ErrMalformed},
		{
			name:    "wrong key",
			token:   func(t *testing.T) string { o := defaults(); o.key = other; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "unknown kid",
			token:   func(t *testing.T) string { o := defaults(); o.kid = "key-2"; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "no kid",
			token:   func(t *testing.T) string { o := defaults(); o.kid = ""; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "hmac",
			token:   func(t *testing.T) string { o := defaults(); o.method = jwt.SigningMethodHS256; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "expired",
			token:   func(t *testing.T) string { o := defaults(); o.expires = now.Add(-2 * time.Minute); return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "wrong audience",
			token:   func(t *testing.T) string { o := defaults(); o.audience = "other-client"; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "wrong issuer",
			token:   func(t *testing.T) string { o := defaults(); o.issuer = "https://example.com"; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "no subject",
			token:   func(t *testing.T) string { o := defaults(); o.subject = ""; return sign(t, o) },
			wantErr: ErrInvalid,
		},
		{
			name:    "email unverified",
			token:   func(t *testing.T) string { o := defaults(); o.emailVerified = false; return sign(t, o) },
			wantErr: ErrEmailUnverified,
		},
		{
			name:    "no email",
			token:   func(t *testing.T) string { o := defaults(); o.email = ""; return sign(t, o) },
			wantErr: ErrEmailUnverified,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := newVerifier(newJWKS(t), func() time.Time { return now })
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

func TestVerifyFetchFailure(t *testing.T) {
	j := newJWKS(t)
	j.status = http.StatusInternalServerError
	v := newVerifier(j, func() time.Time { return now })
	_, err := v.Verify(context.Background(), sign(t, defaults()))
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
	j := newJWKS(t)
	j.cache = "public, max-age=3600"
	v := newVerifier(j, func() time.Time { return clock })
	ctx := context.Background()
	valid := func() string { o := defaults(); o.expires = now.Add(24 * time.Hour); return sign(t, o) }()
	unknown := func() string {
		o := defaults()
		o.kid = "key-2"
		o.expires = now.Add(24 * time.Hour)
		return sign(t, o)
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
		if got := j.fetches.Load(); got != s.wantFetches {
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
