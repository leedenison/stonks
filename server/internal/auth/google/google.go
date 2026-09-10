// Package google verifies Google ID tokens.
//
// A token is accepted when its RS256 signature checks against a key in
// Google's published JWKS, its issuer is Google, its audience is the
// configured OAuth client id, it has not expired, and Google reports its email
// as verified. Only the subject, email and name are taken from it.
//
// Failures cross the package boundary as sentinels: ErrMalformed for a token
// that is not a JWT, ErrInvalid for one that fails verification, and
// ErrEmailUnverified for a verified token whose email cannot be relied on. A
// failure to fetch the JWKS is none of these; it is wrapped and returned as
// is.
package google

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

const (
	jwksURL = "https://www.googleapis.com/oauth2/v3/certs"
	leeway  = time.Minute
)

var (
	// ErrMalformed is returned for a token that is not a JWT.
	ErrMalformed = errors.New("malformed token")
	// ErrInvalid is returned for a token that fails verification.
	ErrInvalid = errors.New("invalid token")
	// ErrEmailUnverified is returned for a valid token whose email is
	// missing or not verified by Google.
	ErrEmailUnverified = errors.New("email not verified")
)

// Claims is the identity a valid token asserts.
type Claims struct {
	// Subject is the stable identifier of the Google account.
	Subject string
	Email   string
	// Name is the display name Google reported; empty when it reported none.
	Name string
}

type claims struct {
	jwt.RegisteredClaims
	Email         string `json:"email"`
	EmailVerified bool   `json:"email_verified"`
	Name          string `json:"name"`
}

// Verifier verifies tokens issued for one OAuth client.
type Verifier struct {
	parser *jwt.Parser
	keys   *keyset
}

// Option configures a Verifier.
type Option func(*Verifier)

// WithHTTPClient sets the client that fetches the JWKS.
func WithHTTPClient(c *http.Client) Option {
	return func(v *Verifier) { v.keys.http = c }
}

// WithJWKSURL sets where the JWKS is fetched from.
func WithJWKSURL(url string) Option {
	return func(v *Verifier) { v.keys.url = url }
}

// WithClock sets the clock used for expiry and cache freshness.
func WithClock(clock func() time.Time) Option {
	return func(v *Verifier) { v.keys.clock = clock }
}

// New returns a Verifier accepting tokens whose audience is clientID.
func New(clientID string, opts ...Option) *Verifier {
	v := &Verifier{keys: &keyset{url: jwksURL, http: http.DefaultClient, clock: time.Now}}
	for _, o := range opts {
		o(v)
	}
	v.parser = jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodRS256.Alg()}),
		jwt.WithExpirationRequired(),
		jwt.WithAudience(clientID),
		jwt.WithLeeway(leeway),
		jwt.WithTimeFunc(v.keys.clock),
	)
	return v
}

// Verify checks token and returns the identity it asserts.
func (v *Verifier) Verify(ctx context.Context, token string) (Claims, error) {
	var c claims
	_, err := v.parser.ParseWithClaims(token, &c, func(t *jwt.Token) (any, error) {
		kid, _ := t.Header["kid"].(string)
		if kid == "" {
			return nil, errors.New("no kid in header")
		}
		return v.keys.key(ctx, kid)
	})
	switch {
	case errors.Is(err, errFetch):
		return Claims{}, fmt.Errorf("verify: %w", err)
	case errors.Is(err, jwt.ErrTokenMalformed):
		return Claims{}, fmt.Errorf("%w: %v", ErrMalformed, err)
	case err != nil:
		return Claims{}, fmt.Errorf("%w: %v", ErrInvalid, err)
	}
	if c.Issuer != "https://accounts.google.com" && c.Issuer != "accounts.google.com" {
		return Claims{}, fmt.Errorf("%w: issuer %q", ErrInvalid, c.Issuer)
	}
	if c.Subject == "" {
		return Claims{}, fmt.Errorf("%w: no subject", ErrInvalid)
	}
	if c.Email == "" || !c.EmailVerified {
		return Claims{}, ErrEmailUnverified
	}
	return Claims{Subject: c.Subject, Email: c.Email, Name: c.Name}, nil
}
