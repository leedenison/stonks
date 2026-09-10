package config

import (
	"strings"
	"testing"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"

	"github.com/leedenison/stonks/server/internal/auth/allowlist"
)

var keys = []string{
	"STONKS_LISTEN_ADDR",
	"STONKS_DB_URL",
	"STONKS_REDIS_URL",
	"STONKS_GOOGLE_OAUTH_CLIENT_ID",
	"STONKS_ALLOWED_EMAILS",
	"STONKS_COOKIE_SECURE",
	"STONKS_LOG_LEVEL",
	"STONKS_OTLP_ENDPOINT",
	"STONKS_ENVIRONMENT",
}

var required = map[string]string{
	"STONKS_DB_URL":                 "postgres://db",
	"STONKS_REDIS_URL":              "redis://cache",
	"STONKS_GOOGLE_OAUTH_CLIENT_ID": "client-id",
}

func with(overrides map[string]string) map[string]string {
	env := map[string]string{}
	for k, v := range required {
		env[k] = v
	}
	for k, v := range overrides {
		env[k] = v
	}
	return env
}

func TestLoad(t *testing.T) {
	tests := []struct {
		name     string
		env      map[string]string
		want     Config
		wantErrs []string
	}{
		{
			name: "defaults",
			env:  with(nil),
			want: Config{ListenAddr: ":8090", DBURL: "postgres://db", RedisURL: "redis://cache", GoogleClientID: "client-id", CookieSecure: true, Environment: "development"},
		},
		{
			name: "explicit",
			env: with(map[string]string{
				"STONKS_LISTEN_ADDR":    ":9000",
				"STONKS_ALLOWED_EMAILS": "*@example.com, one@example.org",
				"STONKS_COOKIE_SECURE":  "false",
				"STONKS_LOG_LEVEL":      "warn,internal/db=debug",
				"STONKS_OTLP_ENDPOINT":  "http://otel-collector:4318",
				"STONKS_ENVIRONMENT":    "staging",
			}),
			want: Config{
				ListenAddr:     ":9000",
				DBURL:          "postgres://db",
				RedisURL:       "redis://cache",
				GoogleClientID: "client-id",
				AllowedEmails:  allowlist.List{"*@example.com", "one@example.org"},
				OTLPEndpoint:   "http://otel-collector:4318",
				Environment:    "staging",
			},
		},
		{
			name:     "missing db url",
			env:      with(map[string]string{"STONKS_DB_URL": ""}),
			wantErrs: []string{"STONKS_DB_URL is required"},
		},
		{
			name:     "missing redis url",
			env:      with(map[string]string{"STONKS_REDIS_URL": ""}),
			wantErrs: []string{"STONKS_REDIS_URL is required"},
		},
		{
			name:     "missing client id",
			env:      with(map[string]string{"STONKS_GOOGLE_OAUTH_CLIENT_ID": ""}),
			wantErrs: []string{"STONKS_GOOGLE_OAUTH_CLIENT_ID is required"},
		},
		{
			name:     "endpoint not a url",
			env:      with(map[string]string{"STONKS_OTLP_ENDPOINT": "://collector"}),
			wantErrs: []string{"STONKS_OTLP_ENDPOINT:"},
		},
		{
			name:     "endpoint wrong scheme",
			env:      with(map[string]string{"STONKS_OTLP_ENDPOINT": "grpc://otel-collector:4317"}),
			wantErrs: []string{`STONKS_OTLP_ENDPOINT: "grpc://otel-collector:4317" is not an http or https URL`},
		},
		{
			name:     "endpoint no host",
			env:      with(map[string]string{"STONKS_OTLP_ENDPOINT": "http://"}),
			wantErrs: []string{`STONKS_OTLP_ENDPOINT: "http://" has no host`},
		},
		{
			name:     "bad cookie secure",
			env:      with(map[string]string{"STONKS_COOKIE_SECURE": "yes"}),
			wantErrs: []string{`STONKS_COOKIE_SECURE: strconv.ParseBool: parsing "yes"`},
		},
		{
			name:     "bad log level",
			env:      with(map[string]string{"STONKS_LOG_LEVEL": "trace"}),
			wantErrs: []string{`STONKS_LOG_LEVEL: unknown level "trace"`},
		},
		{
			name: "every problem named",
			env:  map[string]string{"STONKS_LOG_LEVEL": "trace", "STONKS_COOKIE_SECURE": "yes"},
			wantErrs: []string{
				`STONKS_LOG_LEVEL: unknown level "trace"`,
				`STONKS_COOKIE_SECURE: strconv.ParseBool: parsing "yes"`,
				"STONKS_DB_URL is required",
				"STONKS_REDIS_URL is required",
				"STONKS_GOOGLE_OAUTH_CLIENT_ID is required",
			},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			// Every variable is set, so what the test process inherits does
			// not leak in.
			for _, key := range keys {
				t.Setenv(key, tc.env[key])
			}
			got, err := Load()
			if len(tc.wantErrs) > 0 {
				if err == nil {
					t.Fatalf("Load() = %+v, want error naming %q", got, tc.wantErrs)
				}
				for _, want := range tc.wantErrs {
					if !strings.Contains(err.Error(), want) {
						t.Errorf("Load() error = %q, want it to name %q", err, want)
					}
				}
				return
			}
			if err != nil {
				t.Fatalf("Load() error = %v", err)
			}
			wantLevels := tc.env["STONKS_LOG_LEVEL"]
			if wantLevels == "" {
				wantLevels = "info"
			}
			if got.LogLevel.String() != strings.ReplaceAll(wantLevels, ",", " ") {
				t.Errorf("LogLevel = %q, want %q", got.LogLevel, wantLevels)
			}
			if diff := cmp.Diff(tc.want, got, cmpopts.IgnoreFields(Config{}, "LogLevel")); diff != "" {
				t.Errorf("Load() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}
