// Package googletest stands in for Google as the issuer of ID tokens: a
// signing key, the JWKS that publishes it, and the tokens it signs.
package googletest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"math/big"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

// KID is the key id under which the JWKS publishes Key.
const KID = "key-1"

// Key signs every token unless the token names another.
var Key = NewKey()

// NewKey returns a fresh signing key, which no JWKS publishes.
func NewKey() *rsa.PrivateKey {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}
	return key
}

// JWKS serves the public half of Key under KID.
type JWKS struct {
	*httptest.Server
	// Fetches counts the requests served.
	Fetches atomic.Int32
	// Status is the status of every answer. Any status other than 200 has
	// an empty body.
	Status int
	// Cache is the Cache-Control header of an answer. Empty omits the header.
	Cache string
}

// NewJWKS starts a JWKS that stops with the test.
func NewJWKS(t *testing.T) *JWKS {
	t.Helper()
	j := &JWKS{Status: http.StatusOK}
	j.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		j.Fetches.Add(1)
		if j.Status != http.StatusOK {
			w.WriteHeader(j.Status)
			return
		}
		if j.Cache != "" {
			w.Header().Set("Cache-Control", j.Cache)
		}
		pub := Key.PublicKey
		require.NoError(t, json.NewEncoder(w).Encode(map[string]any{"keys": []map[string]string{{
			"kty": "RSA",
			"kid": KID,
			"n":   base64.RawURLEncoding.EncodeToString(pub.N.Bytes()),
			"e":   base64.RawURLEncoding.EncodeToString(big.NewInt(int64(pub.E)).Bytes()),
		}}}))
	}))
	t.Cleanup(j.Close)
	return j
}

// Token is an ID token before it is signed.
type Token struct {
	Key *rsa.PrivateKey
	KID string
	// Method is the signing method. HS256 signs with a fixed secret in place
	// of Key.
	Method        jwt.SigningMethod
	Issuer        string
	Audience      string
	Subject       string
	Email         string
	EmailVerified bool
	Expires       time.Time
}

// Valid returns a token that Google would issue to an OAuth client for an
// account, at a given moment. It expires an hour later.
func Valid(audience, email string, at time.Time) Token {
	return Token{
		Key:           Key,
		KID:           KID,
		Method:        jwt.SigningMethodRS256,
		Issuer:        "https://accounts.google.com",
		Audience:      audience,
		Subject:       "subject-" + email,
		Email:         email,
		EmailVerified: true,
		Expires:       at.Add(time.Hour),
	}
}

// Sign returns the token in its compact form, with the name claim "One".
func (o Token) Sign(t *testing.T) string {
	t.Helper()
	tok := jwt.NewWithClaims(o.Method, jwt.MapClaims{
		"iss":            o.Issuer,
		"sub":            o.Subject,
		"aud":            []string{o.Audience},
		"exp":            o.Expires.Unix(),
		"email":          o.Email,
		"email_verified": o.EmailVerified,
		"name":           "One",
	})
	tok.Header["kid"] = o.KID
	var key any = o.Key
	if o.Method == jwt.SigningMethodHS256 {
		key = []byte("secret")
	}
	s, err := tok.SignedString(key)
	require.NoError(t, err)
	return s
}
