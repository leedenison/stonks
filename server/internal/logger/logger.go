// Package logger builds the service's slog logger and applies a level per
// category.
//
// A category is the package path below server/, such as internal/db. Each
// subsystem takes a logger tagged with its category by WithCategory. A
// record's threshold is the level of the most specific configured prefix of
// its category, compared segment-wise on "/", and the default level when no
// prefix matches or the record carries no category.
//
// A level spec is a comma-separated list in which a bare level is the default
// and category=level is an override, for example "info,internal/db=debug".
// Levels are debug, info, warn and error, case-insensitive.
//
// A record made while a span is in flight carries that span's trace_id and
// span_id, so the lines of one request can be gathered out of an interleaved
// log.
package logger

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"

	"go.opentelemetry.io/otel/trace"
)

const categoryKey = "category"

type override struct {
	segs  []string
	level slog.Level
}

// Levels is a parsed level spec.
type Levels struct {
	def       slog.Level
	overrides []override
}

// ParseLevels parses a level spec. An empty spec is info with no overrides.
func ParseLevels(spec string) (Levels, error) {
	l := Levels{def: slog.LevelInfo}
	if strings.TrimSpace(spec) == "" {
		return l, nil
	}
	seenDefault := false
	for _, entry := range strings.Split(spec, ",") {
		entry = strings.TrimSpace(entry)
		cat, name, isOverride := strings.Cut(entry, "=")
		if !isOverride {
			name = cat
		}
		level, ok := parseLevel(name)
		if !ok {
			return Levels{}, fmt.Errorf("unknown level %q", name)
		}
		if !isOverride {
			if seenDefault {
				return Levels{}, fmt.Errorf("second default level %q", entry)
			}
			seenDefault = true
			l.def = level
			continue
		}
		segs := split(cat)
		if len(segs) == 0 {
			return Levels{}, fmt.Errorf("empty category in %q", entry)
		}
		l.overrides = append(l.overrides, override{segs: segs, level: level})
	}
	return l, nil
}

// For returns the level in force for category.
func (l Levels) For(category string) slog.Level {
	segs := split(category)
	level, best := l.def, 0
	for _, o := range l.overrides {
		if len(o.segs) > best && isPrefix(o.segs, segs) {
			level, best = o.level, len(o.segs)
		}
	}
	return level
}

// String renders the spec in canonical form, e.g. "info internal/db=debug".
func (l Levels) String() string {
	parts := []string{levelName(l.def)}
	for _, o := range l.overrides {
		parts = append(parts, strings.Join(o.segs, "/")+"="+levelName(o.level))
	}
	return strings.Join(parts, " ")
}

// New returns a logger writing text records to w, filtered by levels.
func New(w io.Writer, levels Levels) *slog.Logger {
	inner := slog.NewTextHandler(w, &slog.HandlerOptions{Level: slog.LevelDebug})
	return slog.New(&handler{inner: inner, levels: levels})
}

// WithCategory returns log tagged with category, so levels applies to it.
func WithCategory(log *slog.Logger, category string) *slog.Logger {
	return log.With(categoryKey, category)
}

// handler filters by the category captured from WithAttrs and delegates the
// rest to inner.
type handler struct {
	inner    slog.Handler
	levels   Levels
	category string
}

func (h *handler) Enabled(_ context.Context, level slog.Level) bool {
	return level >= h.levels.For(h.category)
}

func (h *handler) Handle(ctx context.Context, r slog.Record) error {
	if sc := trace.SpanContextFromContext(ctx); sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *handler) WithAttrs(attrs []slog.Attr) slog.Handler {
	next := &handler{inner: h.inner.WithAttrs(attrs), levels: h.levels, category: h.category}
	for _, a := range attrs {
		if a.Key == categoryKey && a.Value.Kind() == slog.KindString {
			next.category = a.Value.String()
		}
	}
	return next
}

func (h *handler) WithGroup(name string) slog.Handler {
	return &handler{inner: h.inner.WithGroup(name), levels: h.levels, category: h.category}
}

func parseLevel(name string) (slog.Level, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "debug":
		return slog.LevelDebug, true
	case "info":
		return slog.LevelInfo, true
	case "warn":
		return slog.LevelWarn, true
	case "error":
		return slog.LevelError, true
	}
	return 0, false
}

func levelName(l slog.Level) string {
	return strings.ToLower(l.String())
}

func split(category string) []string {
	var segs []string
	for _, s := range strings.Split(category, "/") {
		if s = strings.TrimSpace(s); s != "" {
			segs = append(segs, s)
		}
	}
	return segs
}

func isPrefix(prefix, segs []string) bool {
	if len(prefix) > len(segs) {
		return false
	}
	for i := range prefix {
		if prefix[i] != segs[i] {
			return false
		}
	}
	return true
}
