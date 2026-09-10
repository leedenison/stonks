package google

import (
	"context"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	defaultMaxAge = time.Hour
	// minRefetch bounds how often an unknown kid can trigger a fetch while
	// the cached set is still fresh.
	minRefetch = time.Minute
)

var (
	errFetch      = errors.New("fetch jwks")
	errUnknownKey = errors.New("unknown key")
)

// keyset caches the RSA public keys of a JWKS, refreshing when the set is
// stale by its Cache-Control max-age, or when a kid is missing from a fresh
// set and the last fetch was at least minRefetch ago. Fetches are lazy and
// serialised, so a burst of requests during a refresh waits rather than
// fetching in parallel.
type keyset struct {
	url   string
	http  *http.Client
	clock func() time.Time

	mu      sync.Mutex
	keys    map[string]*rsa.PublicKey
	fresh   time.Time
	fetched time.Time
}

func (k *keyset) key(ctx context.Context, kid string) (*rsa.PublicKey, error) {
	k.mu.Lock()
	defer k.mu.Unlock()
	now := k.clock()
	if pub, ok := k.keys[kid]; ok && now.Before(k.fresh) {
		return pub, nil
	}
	if now.Before(k.fresh) && now.Sub(k.fetched) < minRefetch {
		return nil, errUnknownKey
	}
	if err := k.fetch(ctx, now); err != nil {
		return nil, fmt.Errorf("%w: %w", errFetch, err)
	}
	if pub, ok := k.keys[kid]; ok {
		return pub, nil
	}
	return nil, errUnknownKey
}

func (k *keyset) fetch(ctx context.Context, now time.Time) (err error) {
	k.fetched = now
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, k.url, nil)
	if err != nil {
		return err
	}
	res, err := k.http.Do(req)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, res.Body.Close()) }()
	if res.StatusCode != http.StatusOK {
		return fmt.Errorf("status %s", res.Status)
	}
	var body struct {
		Keys []struct {
			Kty string `json:"kty"`
			Kid string `json:"kid"`
			N   string `json:"n"`
			E   string `json:"e"`
		} `json:"keys"`
	}
	if err := json.NewDecoder(res.Body).Decode(&body); err != nil {
		return fmt.Errorf("decode: %w", err)
	}
	keys := make(map[string]*rsa.PublicKey, len(body.Keys))
	for _, jwk := range body.Keys {
		if jwk.Kty != "RSA" {
			continue
		}
		pub, err := rsaKey(jwk.N, jwk.E)
		if err != nil {
			return fmt.Errorf("key %q: %w", jwk.Kid, err)
		}
		keys[jwk.Kid] = pub
	}
	k.keys = keys
	k.fresh = now.Add(maxAge(res.Header.Get("Cache-Control")))
	return nil
}

func rsaKey(n, e string) (*rsa.PublicKey, error) {
	nb, err := base64.RawURLEncoding.DecodeString(n)
	if err != nil {
		return nil, fmt.Errorf("modulus: %w", err)
	}
	eb, err := base64.RawURLEncoding.DecodeString(e)
	if err != nil {
		return nil, fmt.Errorf("exponent: %w", err)
	}
	exp := new(big.Int).SetBytes(eb)
	if !exp.IsInt64() || exp.Int64() <= 0 {
		return nil, errors.New("exponent out of range")
	}
	return &rsa.PublicKey{N: new(big.Int).SetBytes(nb), E: int(exp.Int64())}, nil
}

// maxAge reads the max-age directive of a Cache-Control header, falling back
// to defaultMaxAge.
func maxAge(header string) time.Duration {
	for _, d := range strings.Split(header, ",") {
		if v, ok := strings.CutPrefix(strings.TrimSpace(d), "max-age="); ok {
			if secs, err := strconv.Atoi(v); err == nil && secs > 0 {
				return time.Duration(secs) * time.Second
			}
		}
	}
	return defaultMaxAge
}
