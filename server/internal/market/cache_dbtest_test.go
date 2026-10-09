//go:build dbtest

package market

import (
	"context"
	"fmt"
	"log"
	"os"
	"testing"
	"time"

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
	opts, err := redis.ParseURL(url)
	if err != nil {
		log.Fatalf("parse redis url: %v", err)
	}
	rdb = redis.NewClient(opts)
	code := m.Run()
	if err := rdb.Close(); err != nil {
		log.Fatalf("close: %v", err)
	}
	os.Exit(code)
}

// lifetime is the entry lifetime the tests give the cache.
const lifetime = time.Minute

// datasourceOf invents a datasource name. Tests share one Redis, so each
// writes under its own datasource name.
func datasourceOf() string { return "ds-" + uuid.NewString() }

// keysOf returns n keys of datasource.
func keysOf(datasource string, n int) []string {
	out := make([]string, n)
	for i := range out {
		out[i] = fmt.Sprintf("%s%d", dropPrefix(datasource), i)
	}
	return out
}

func TestCacheRoundTrip(t *testing.T) {
	ctx := context.Background()
	c := NewCache(rdb, lifetime)
	keys := keysOf(datasourceOf(), 2)

	require.NoError(t, c.Set(ctx, keys[0], []byte(`{"a":1}`)))
	got, err := c.Get(ctx, keys)
	require.NoError(t, err)
	if string(got[0]) != `{"a":1}` || got[1] != nil {
		t.Errorf("Get() = %q, %q, want the entry and nil for the miss", got[0], got[1])
	}
	ttl, err := rdb.TTL(ctx, keys[0]).Result()
	require.NoError(t, err)
	if ttl > lifetime || lifetime-ttl > 2*time.Second {
		t.Errorf("TTL = %v, want %v", ttl, lifetime)
	}
	empty, err := c.Get(ctx, nil)
	require.NoError(t, err)
	if len(empty) != 0 {
		t.Errorf("Get(nil) = %v, want nothing", empty)
	}
}

// TestCacheDrop checks that a drop removes every key of the datasource,
// across more than one SCAN page, and leaves another datasource's keys.
func TestCacheDrop(t *testing.T) {
	ctx := context.Background()
	c := NewCache(rdb, lifetime)
	dropped, kept := datasourceOf(), datasourceOf()
	droppedKeys, keptKeys := keysOf(dropped, 250), keysOf(kept, 3)
	for _, k := range append(droppedKeys, keptKeys...) {
		require.NoError(t, c.Set(ctx, k, []byte("x")))
	}

	require.NoError(t, c.Drop(ctx, dropped))
	got, err := c.Get(ctx, droppedKeys)
	require.NoError(t, err)
	for i, v := range got {
		if v != nil {
			t.Fatalf("key %d of the dropped datasource survived", i)
		}
	}
	got, err = c.Get(ctx, keptKeys)
	require.NoError(t, err)
	for i, v := range got {
		if v == nil {
			t.Errorf("key %d of the other datasource was dropped", i)
		}
	}
	require.NoError(t, c.Drop(ctx, datasourceOf()))
}
