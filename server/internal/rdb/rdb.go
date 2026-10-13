// Package rdb opens the process's Redis client. Sessions and the fetch cache
// share it.
package rdb

import (
	"context"
	"errors"
	"fmt"

	"github.com/redis/go-redis/extra/redisotel/v9"
	"github.com/redis/go-redis/v9"
)

// Option configures a client.
type Option func(*options)

type options struct{ tracing bool }

// WithTracing traces every command and reports the connection pool's
// statistics through the process's OpenTelemetry providers. It is off by
// default, and the service turns it on.
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
	c := redis.NewClient(redisOpts)
	if o.tracing {
		if err := errors.Join(redisotel.InstrumentTracing(c), redisotel.InstrumentMetrics(c)); err != nil {
			return nil, errors.Join(fmt.Errorf("instrument redis: %w", err), c.Close())
		}
	}
	if err := c.Ping(ctx).Err(); err != nil {
		return nil, errors.Join(fmt.Errorf("ping: %w", err), c.Close())
	}
	return c, nil
}
