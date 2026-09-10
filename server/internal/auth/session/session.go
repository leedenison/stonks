// Package session holds sessions in Redis.
//
// A session is an opaque bearer token, mapped to the user it belongs to and
// its timing. It is read from the process's cryptographic source and nothing
// is derived from it, so it carries no structure a client can read or forge.
// The record is stored as JSON under the key "stonks:session:<id>" with a key
// TTL equal to its remaining life:
//
//	{"user_id":"<uuid>","created_at":"<RFC 3339>","expires_at":"<RFC 3339>"}
//
// The e2e suite writes this record directly, so its shape is part of the
// contract.
//
// A session lives while it is used: each Get moves expires_at to Idle from
// now, up to Max from created_at, after which it is gone. Delete removes it
// at once, and a removed identifier is never accepted again because there is
// nothing left to accept.
//
// Redis is a hard dependency of the request path. A failure to reach it is
// returned wrapped, and is not ErrNotFound.
package session

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

const (
	// Idle is how long a session lives after its last use.
	Idle = 7 * 24 * time.Hour
	// Max is how long a session lives from its creation, however used.
	Max = 30 * 24 * time.Hour

	prefix = "stonks:session:"
)

// ErrNotFound is returned when no live session has the identifier.
var ErrNotFound = errors.New("session not found")

// Session is one stored session. ID is the key and is not part of the
// record.
type Session struct {
	ID        string    `json:"-"`
	UserID    uuid.UUID `json:"user_id"`
	CreatedAt time.Time `json:"created_at"`
	ExpiresAt time.Time `json:"expires_at"`
}

// Option configures a client.
type Option func(*options)

type options struct{ tracing bool }

// WithTracing traces every command and reports the connection pool's
// statistics through the process's OpenTelemetry providers. It is off by
// default, so a client opened by a test emits nothing.
func WithTracing() Option { return func(o *options) { o.tracing = true } }

// Open connects a client to url and verifies it with a ping.
func Open(ctx context.Context, url string, opts ...Option) (*redis.Client, error) {
	var o options
	for _, opt := range opts {
		opt(&o)
	}
	redisOpts, err := redis.ParseURL(url)
	if err != nil {
		return nil, fmt.Errorf("parse redis url: %w", err)
	}
	rdb := redis.NewClient(redisOpts)
	if o.tracing {
		if err := errors.Join(redisotel.InstrumentTracing(rdb), redisotel.InstrumentMetrics(rdb)); err != nil {
			return nil, errors.Join(fmt.Errorf("instrument redis: %w", err), rdb.Close())
		}
	}
	if err := rdb.Ping(ctx).Err(); err != nil {
		return nil, errors.Join(fmt.Errorf("ping: %w", err), rdb.Close())
	}
	return rdb, nil
}

// Store creates, reads and deletes sessions.
type Store struct {
	rdb   *redis.Client
	clock func() time.Time
}

// New returns a Store over rdb reading the time from clock.
func New(rdb *redis.Client, clock func() time.Time) *Store {
	return &Store{rdb: rdb, clock: clock}
}

// Create starts a session for userID.
func (s *Store) Create(ctx context.Context, userID uuid.UUID) (Session, error) {
	now := s.clock().UTC()
	sess := Session{ID: rand.Text(), UserID: userID, CreatedAt: now, ExpiresAt: now.Add(Idle)}
	if err := s.set(ctx, sess, now); err != nil {
		return Session{}, err
	}
	return sess, nil
}

// Get returns the live session with id, extended by its use.
func (s *Store) Get(ctx context.Context, id string) (Session, error) {
	val, err := s.rdb.Get(ctx, prefix+id).Bytes()
	if errors.Is(err, redis.Nil) {
		return Session{}, ErrNotFound
	}
	if err != nil {
		return Session{}, fmt.Errorf("get session: %w", err)
	}
	var sess Session
	if err := json.Unmarshal(val, &sess); err != nil {
		return Session{}, fmt.Errorf("decode session: %w", err)
	}
	sess.ID = id
	now := s.clock().UTC()
	limit := sess.CreatedAt.Add(Max)
	if !now.Before(limit) || !now.Before(sess.ExpiresAt) {
		if err := s.Delete(ctx, id); err != nil {
			return Session{}, err
		}
		return Session{}, ErrNotFound
	}
	exp := now.Add(Idle)
	if exp.After(limit) {
		exp = limit
	}
	if exp.After(sess.ExpiresAt) {
		sess.ExpiresAt = exp
		if err := s.set(ctx, sess, now); err != nil {
			return Session{}, err
		}
	}
	return sess, nil
}

// Delete ends the session with id. An unknown id is not an error.
func (s *Store) Delete(ctx context.Context, id string) error {
	if err := s.rdb.Del(ctx, prefix+id).Err(); err != nil {
		return fmt.Errorf("delete session: %w", err)
	}
	return nil
}

func (s *Store) set(ctx context.Context, sess Session, now time.Time) error {
	payload, err := json.Marshal(sess)
	if err != nil {
		return fmt.Errorf("encode session: %w", err)
	}
	if err := s.rdb.Set(ctx, prefix+sess.ID, payload, sess.ExpiresAt.Sub(now)).Err(); err != nil {
		return fmt.Errorf("set session: %w", err)
	}
	return nil
}
