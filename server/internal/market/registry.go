package market

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Entry is one enabled datasource and the integration serving it.
type Entry struct {
	Name        string
	Precedence  int32
	Integration Integration

	limiter *limiter
}

// limiter paces the calls to one datasource. It outlives a reload, so
// concurrent fetches share one quota and a reload keeps the quota spent and
// any hold.
type limiter struct {
	rate *rate.Limiter

	mu sync.Mutex
	// hold is when the provider said calls may resume; zero when it has
	// said nothing.
	hold time.Time
}

func newLimiter(limit rate.Limit, burst int) *limiter {
	return &limiter{rate: rate.NewLimiter(limit, burst)}
}

// held returns how long a call made at now must wait for the hold.
func (l *limiter) held(now time.Time) time.Duration {
	l.mu.Lock()
	defer l.mu.Unlock()
	return max(l.hold.Sub(now), 0)
}

// holdUntil holds every call until t, unless a later hold is in force.
func (l *limiter) holdUntil(t time.Time) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if t.After(l.hold) {
		l.hold = t
	}
}

// Registry holds the enabled datasources in precedence order. It is built at
// startup and rebuilt by Reload when an administrator changes the table.
// Reloads run one at a time, so the entries come from the latest read of the
// table.
type Registry struct {
	store     Queries
	factories map[string]Factory
	log       *slog.Logger

	// reloadLck serialises reloads and guards limiters.
	reloadLck sync.Mutex
	limiters  map[string]*limiter
	entries   atomic.Pointer[[]*Entry]
}

// New reads the datasources table and pairs each enabled row with the
// integration of its name. It fails on an enabled row this binary cannot
// serve, whether because it carries no such integration or because the row
// lacks what that integration needs.
func New(ctx context.Context, store Queries, factories map[string]Factory, log *slog.Logger) (*Registry, error) {
	r := &Registry{store: store, factories: factories, log: log, limiters: map[string]*limiter{}}
	if err := r.Reload(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload reads the datasources table again and replaces the entries. It
// fails as New does, leaving the entries as they were.
func (r *Registry) Reload(ctx context.Context) error {
	r.reloadLck.Lock()
	defer r.reloadLck.Unlock()
	rows, err := r.store.ListDatasources(ctx)
	if err != nil {
		return fmt.Errorf("list datasources: %w", err)
	}
	var entries []*Entry
	named := map[string]bool{}
	for _, row := range rows {
		named[row.Name] = true
		factory, ok := r.factories[row.Name]
		switch {
		case !row.Enabled:
			r.log.Info("datasource disabled", "datasource", row.Name, "carried", ok)
			continue
		case !ok:
			return fmt.Errorf("datasource %s is enabled and this build carries no such integration", row.Name)
		}
		integration, err := factory(config(row))
		if err != nil {
			return fmt.Errorf("datasource %s: %w", row.Name, err)
		}
		entries = append(entries, &Entry{Name: row.Name, Precedence: row.Precedence, Integration: integration})
	}
	for name := range r.factories {
		if !named[name] {
			r.log.Info("integration has no datasource row", "datasource", name)
		}
	}
	for _, e := range entries {
		limit, burst := e.Integration.Limit()
		e.limiter = r.limiter(e.Name, limit, burst)
	}
	r.entries.Store(&entries)
	return nil
}

// limiter returns the datasource's limiter at the rate given, made on its
// first use. The caller holds reloadLck.
func (r *Registry) limiter(name string, limit rate.Limit, burst int) *limiter {
	l, ok := r.limiters[name]
	if !ok {
		l = newLimiter(limit, burst)
		r.limiters[name] = l
		return l
	}
	l.rate.SetLimit(limit)
	l.rate.SetBurst(burst)
	return l
}

// Carries reports whether this binary has an integration of the name.
func (r *Registry) Carries(name string) bool {
	_, ok := r.factories[name]
	return ok
}

// Enabled returns the enabled datasources in precedence order, as of the
// last reload. A reload replaces the slice rather than changing it, so the
// caller keeps the datasources it read.
func (r *Registry) Enabled() []*Entry {
	if p := r.entries.Load(); p != nil {
		return *p
	}
	return nil
}

// Names returns the enabled datasources in precedence order, named.
func (r *Registry) Names() []string {
	entries := r.Enabled()
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name
	}
	return out
}

// config reads the row as the integration it names sees it. A NULL credential
// or endpoint reaches the integration as empty.
func config(row gen.Datasource) Config {
	c := Config{Name: row.Name}
	if row.Credential != nil {
		c.Credential = *row.Credential
	}
	if row.Endpoint != nil {
		c.Endpoint = *row.Endpoint
	}
	return c
}
