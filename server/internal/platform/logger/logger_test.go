package logger

import (
	"bytes"
	"context"
	"log/slog"
	"strings"
	"testing"

	"go.opentelemetry.io/otel/trace"
)

func TestParseLevels(t *testing.T) {
	tests := []struct {
		name    string
		spec    string
		want    string
		wantErr bool
	}{
		{name: "empty", spec: "", want: "info"},
		{name: "default only", spec: "debug", want: "debug"},
		{name: "case and space", spec: " WARN ", want: "warn"},
		{name: "override", spec: "info,internal/db=debug", want: "info internal/db=debug"},
		{name: "override before default", spec: "internal/db=debug, warn", want: "warn internal/db=debug"},
		{name: "override only", spec: "internal/db=debug", want: "info internal/db=debug"},
		{name: "unknown level", spec: "trace", wantErr: true},
		{name: "unknown override level", spec: "internal/db=loud", wantErr: true},
		{name: "two defaults", spec: "info,debug", wantErr: true},
		{name: "empty category", spec: "=debug", wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := ParseLevels(tc.spec)
			if (err != nil) != tc.wantErr {
				t.Fatalf("ParseLevels(%q) error = %v, wantErr %v", tc.spec, err, tc.wantErr)
			}
			if err == nil && got.String() != tc.want {
				t.Errorf("ParseLevels(%q) = %q, want %q", tc.spec, got, tc.want)
			}
		})
	}
}

func TestFor(t *testing.T) {
	levels, err := ParseLevels("info,internal=warn,internal/db=debug")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		category string
		want     slog.Level
	}{
		{category: "internal/db", want: slog.LevelDebug},
		{category: "internal/db/gen", want: slog.LevelDebug},
		{category: "internal/service", want: slog.LevelWarn},
		{category: "internal/dbx", want: slog.LevelWarn},
		{category: "cmd/stonks", want: slog.LevelInfo},
		{category: "", want: slog.LevelInfo},
	}
	for _, tc := range tests {
		if got := levels.For(tc.category); got != tc.want {
			t.Errorf("For(%q) = %v, want %v", tc.category, got, tc.want)
		}
	}
}

func TestNew(t *testing.T) {
	levels, err := ParseLevels("info,internal/db=debug")
	if err != nil {
		t.Fatal(err)
	}
	var buf bytes.Buffer
	log := New(&buf, levels)
	WithCategory(log, "internal/db").Debug("db debug")
	WithCategory(log, "internal/service").Debug("service debug")
	WithCategory(log, "internal/service").WithGroup("g").Info("grouped info")
	log.Debug("untagged debug")
	log.Info("untagged info")

	out := buf.String()
	for _, want := range []string{"msg=\"db debug\"", "msg=\"grouped info\"", "msg=\"untagged info\""} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %s:\n%s", want, out)
		}
	}
	for _, unwanted := range []string{"service debug", "untagged debug"} {
		if strings.Contains(out, unwanted) {
			t.Errorf("output contains %q:\n%s", unwanted, out)
		}
	}
}

// TestTraceIDs asserts a record made under a span carries that span's
// identifiers, and one made without a span carries neither.
func TestTraceIDs(t *testing.T) {
	traceID, err := trace.TraceIDFromHex("0102030405060708090a0b0c0d0e0f10")
	if err != nil {
		t.Fatalf("TraceIDFromHex() error = %v", err)
	}
	spanID, err := trace.SpanIDFromHex("0102030405060708")
	if err != nil {
		t.Fatalf("SpanIDFromHex() error = %v", err)
	}
	traced := trace.ContextWithSpanContext(context.Background(), trace.NewSpanContext(trace.SpanContextConfig{
		TraceID: traceID,
		SpanID:  spanID,
	}))

	tests := []struct {
		name string
		ctx  context.Context
		want []string
		omit []string
	}{
		{
			name: "in a span",
			ctx:  traced,
			want: []string{"trace_id=" + traceID.String(), "span_id=" + spanID.String()},
		},
		{
			name: "no span",
			ctx:  context.Background(),
			omit: []string{"trace_id", "span_id"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			buf := &bytes.Buffer{}
			levels, err := ParseLevels("info")
			if err != nil {
				t.Fatalf("ParseLevels() error = %v", err)
			}
			New(buf, levels).InfoContext(tc.ctx, "hello")

			got := buf.String()
			for _, want := range tc.want {
				if !strings.Contains(got, want) {
					t.Errorf("record = %q, want it to contain %q", got, want)
				}
			}
			for _, omit := range tc.omit {
				if strings.Contains(got, omit) {
					t.Errorf("record = %q, want it not to mention %q", got, omit)
				}
			}
		})
	}
}
