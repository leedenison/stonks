package market

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/redis/go-redis/v9"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// keyPrefix leads every cache key.
const keyPrefix = "stonks:fetch:"

// scanCount is how many keys one SCAN page asks for.
const scanCount = 100

// cacheKey is the key of one request: "stonks:fetch:" then the datasource,
// the kind, the type, domain and value of the identifier sent, and each
// parameter the integration derives from the request, such as the currency
// that OpenFIGI uses as a filter, joined by colons.
func cacheKey(datasource string, kind gen.FetchKind, sent types.Identifier, params []string) string {
	parts := []string{datasource, string(kind), string(sent.Type), sent.Domain, sent.Value}
	parts = append(parts, params...)
	return keyPrefix + strings.Join(parts, ":")
}

// dropPrefix is the prefix every key of datasource shares.
func dropPrefix(datasource string) string {
	return keyPrefix + datasource + ":"
}

// redisCache is a Cache in Redis whose entries live for ttl.
type redisCache struct {
	rdb *redis.Client
	ttl time.Duration
}

// NewCache returns a Cache over rdb whose entries live for ttl.
func NewCache(rdb *redis.Client, ttl time.Duration) Cache {
	return &redisCache{rdb: rdb, ttl: ttl}
}

func (c *redisCache) Get(ctx context.Context, keys []string) ([][]byte, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	vals, err := c.rdb.MGet(ctx, keys...).Result()
	if err != nil {
		return nil, fmt.Errorf("read cache: %w", err)
	}
	out := make([][]byte, len(keys))
	for i, v := range vals {
		if s, ok := v.(string); ok {
			out[i] = []byte(s)
		}
	}
	return out, nil
}

func (c *redisCache) Set(ctx context.Context, key string, value []byte) error {
	if err := c.rdb.Set(ctx, key, value, c.ttl).Err(); err != nil {
		return fmt.Errorf("write cache: %w", err)
	}
	return nil
}

func (c *redisCache) Drop(ctx context.Context, datasource string) error {
	iter := c.rdb.Scan(ctx, 0, dropPrefix(datasource)+"*", scanCount).Iterator()
	var keys []string
	unlink := func() error {
		if len(keys) == 0 {
			return nil
		}
		err := c.rdb.Unlink(ctx, keys...).Err()
		keys = keys[:0]
		return err
	}
	for iter.Next(ctx) {
		keys = append(keys, iter.Val())
		if len(keys) >= scanCount {
			if err := unlink(); err != nil {
				return fmt.Errorf("drop cache: %w", err)
			}
		}
	}
	if err := iter.Err(); err != nil {
		return fmt.Errorf("scan cache: %w", err)
	}
	if err := unlink(); err != nil {
		return fmt.Errorf("drop cache: %w", err)
	}
	return nil
}
