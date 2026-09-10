//go:build dbtest

package session

import (
	"context"
	"errors"
	"log"
	"os"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/require"
)

var rdb *redis.Client

func TestMain(m *testing.M) {
	url := os.Getenv("STONKS_TEST_REDIS_URL")
	if url == "" {
		log.Fatal("STONKS_TEST_REDIS_URL is not set")
	}
	var err error
	rdb, err = Open(context.Background(), url)
	if err != nil {
		log.Fatalf("open: %v", err)
	}
	code := m.Run()
	if err := rdb.Close(); err != nil {
		log.Fatalf("close: %v", err)
	}
	os.Exit(code)
}

var start = time.Date(2026, 9, 9, 12, 0, 0, 0, time.UTC)

// newStore returns a store whose clock the test moves.
func newStore(t *testing.T) (*Store, *time.Time) {
	t.Helper()
	now := start
	return New(rdb, func() time.Time { return now }), &now
}

// wantTTL asserts the key's TTL is want, allowing for the seconds Redis
// counts down between the write and the read.
func wantTTL(t *testing.T, id string, want time.Duration) {
	t.Helper()
	got, err := rdb.TTL(context.Background(), prefix+id).Result()
	require.NoError(t, err)
	if got > want || want-got > 2*time.Second {
		t.Errorf("TTL of %s = %v, want %v", id, got, want)
	}
}

func TestCreateAndGet(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	userID := uuid.New()

	created, err := s.Create(ctx, userID)
	require.NoError(t, err)
	want := Session{ID: created.ID, UserID: userID, CreatedAt: start, ExpiresAt: start.Add(Idle)}
	if diff := cmp.Diff(want, created); diff != "" {
		t.Errorf("Create() mismatch (-want +got):\n%s", diff)
	}
	wantTTL(t, created.ID, Idle)

	got, err := s.Get(ctx, created.ID)
	require.NoError(t, err)
	if diff := cmp.Diff(created, got); diff != "" {
		t.Errorf("Get() mismatch (-want +got):\n%s", diff)
	}
}

func TestGetExtends(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	created, err := s.Create(ctx, uuid.New())
	require.NoError(t, err)

	*now = start.Add(2 * 24 * time.Hour)
	got, err := s.Get(ctx, created.ID)
	require.NoError(t, err)
	if want := now.Add(Idle); !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt after Get = %v, want %v", got.ExpiresAt, want)
	}
	wantTTL(t, created.ID, Idle)
	if !got.CreatedAt.Equal(start) {
		t.Errorf("CreatedAt after Get = %v, want %v", got.CreatedAt, start)
	}
}

func TestGetClampsToMax(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	created, err := s.Create(ctx, uuid.New())
	require.NoError(t, err)

	// Used every five days, the session stays live until Max from creation.
	var got Session
	for day := 5; day <= 25; day += 5 {
		*now = start.Add(time.Duration(day) * 24 * time.Hour)
		got, err = s.Get(ctx, created.ID)
		require.NoError(t, err, "day %d", day)
	}
	if want := start.Add(Max); !got.ExpiresAt.Equal(want) {
		t.Errorf("ExpiresAt on day 25 = %v, want %v", got.ExpiresAt, want)
	}
	wantTTL(t, created.ID, 5*24*time.Hour)

	*now = start.Add(Max)
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() at max: err = %v, want ErrNotFound", err)
	}
	if got, err := rdb.TTL(ctx, prefix+created.ID).Result(); err != nil || got != -2 {
		t.Errorf("key survives past max: TTL = %v (err %v), want -2 (no key)", got, err)
	}
}

func TestGetPastExpiry(t *testing.T) {
	s, now := newStore(t)
	ctx := context.Background()
	created, err := s.Create(ctx, uuid.New())
	require.NoError(t, err)

	// The key outlives its record only when the clock and Redis disagree, as
	// they do here because the clock is pinned.
	*now = start.Add(Idle)
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() at expiry: err = %v, want ErrNotFound", err)
	}
}

func TestDelete(t *testing.T) {
	s, _ := newStore(t)
	ctx := context.Background()
	created, err := s.Create(ctx, uuid.New())
	require.NoError(t, err)

	require.NoError(t, s.Delete(ctx, created.ID))
	if _, err := s.Get(ctx, created.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() after Delete: err = %v, want ErrNotFound", err)
	}
	require.NoError(t, s.Delete(ctx, created.ID))
}

func TestGetUnknown(t *testing.T) {
	s, _ := newStore(t)
	if _, err := s.Get(context.Background(), uuid.NewString()); !errors.Is(err, ErrNotFound) {
		t.Errorf("Get() of an unknown id: err = %v, want ErrNotFound", err)
	}
}
