// Package config is the service's whole environment surface.
// Nothing outside this package reads the environment.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"

	"github.com/leedenison/stonks/server/internal/auth/allowlist"
	"github.com/leedenison/stonks/server/internal/logger"
)

// Config is the service configuration.
type Config struct {
	// ListenAddr is STONKS_LISTEN_ADDR, the address the HTTP server listens
	// on. Default ":8090".
	ListenAddr string
	// DBURL is STONKS_DB_URL, the Postgres connection URL. Required.
	DBURL string
	// RedisURL is STONKS_REDIS_URL, the Redis connection URL. Required.
	RedisURL string
	// GoogleClientID is STONKS_GOOGLE_OAUTH_CLIENT_ID, the OAuth client an ID
	// token must be issued for. Required.
	GoogleClientID string
	// AllowedEmails is STONKS_ALLOWED_EMAILS, a comma-separated list of glob
	// patterns an email must match for an account to be created, such as
	// "*@example.com". Default empty, which creates no accounts.
	AllowedEmails allowlist.List
	// CookieSecure is STONKS_COOKIE_SECURE, whether the session cookie is
	// marked Secure. Default true; false only where the service is reached
	// over plain HTTP.
	CookieSecure bool
	// LogLevel is STONKS_LOG_LEVEL, a level spec as described in package
	// logger. Default "info".
	LogLevel logger.Levels
	// OTLPEndpoint is STONKS_OTLP_ENDPOINT, the base URL of the OpenTelemetry
	// collector's OTLP/HTTP receiver, such as "http://otel-collector:4318".
	// Default empty, which exports no traces or metrics.
	OTLPEndpoint string
	// Environment is STONKS_ENVIRONMENT, the deployment this process belongs
	// to, reported as the deployment.environment.name resource attribute.
	// Default "development".
	Environment string
}

func Load() (Config, error) {
	var errs []error
	cfg := Config{
		ListenAddr:     env("STONKS_LISTEN_ADDR", ":8090"),
		DBURL:          os.Getenv("STONKS_DB_URL"),
		RedisURL:       os.Getenv("STONKS_REDIS_URL"),
		GoogleClientID: os.Getenv("STONKS_GOOGLE_OAUTH_CLIENT_ID"),
		OTLPEndpoint:   os.Getenv("STONKS_OTLP_ENDPOINT"),
		Environment:    env("STONKS_ENVIRONMENT", "development"),
		AllowedEmails:  allowlist.Parse(os.Getenv("STONKS_ALLOWED_EMAILS")),
	}
	secure, err := strconv.ParseBool(env("STONKS_COOKIE_SECURE", "true"))
	if err != nil {
		errs = append(errs, fmt.Errorf("STONKS_COOKIE_SECURE: %w", err))
	}
	cfg.CookieSecure = secure
	levels, err := logger.ParseLevels(env("STONKS_LOG_LEVEL", "info"))
	if err != nil {
		errs = append(errs, fmt.Errorf("STONKS_LOG_LEVEL: %w", err))
	}
	cfg.LogLevel = levels
	if err := errors.Join(append(errs, cfg.Validate())...); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (c Config) Validate() error {
	var errs []error
	for _, r := range []struct{ key, val string }{
		{"STONKS_DB_URL", c.DBURL},
		{"STONKS_REDIS_URL", c.RedisURL},
		{"STONKS_GOOGLE_OAUTH_CLIENT_ID", c.GoogleClientID},
	} {
		if r.val == "" {
			errs = append(errs, fmt.Errorf("%s is required", r.key))
		}
	}
	if err := validateEndpoint(c.OTLPEndpoint); err != nil {
		errs = append(errs, fmt.Errorf("STONKS_OTLP_ENDPOINT: %w", err))
	}
	return errors.Join(errs...)
}

// validateEndpoint accepts an empty endpoint, which exports nothing, and
// otherwise requires an absolute http or https URL.
func validateEndpoint(endpoint string) error {
	if endpoint == "" {
		return nil
	}
	u, err := url.Parse(endpoint)
	switch {
	case err != nil:
		return err
	case u.Scheme != "http" && u.Scheme != "https":
		return fmt.Errorf("%q is not an http or https URL", endpoint)
	case u.Host == "":
		return fmt.Errorf("%q has no host", endpoint)
	}
	return nil
}

func env(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
