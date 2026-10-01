package market

import (
	"context"
	"fmt"
	"log/slog"
	"sync"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Entry is one enabled datasource and the integration serving it. A field
// per kind of data is set where the integration serves that kind, and nil
// where the datasource is not sent requests for it.
type Entry struct {
	Name        string
	Precedence  int32
	Integration Integration
	Identity    Identity

	limiter *rate.Limiter
}

// Registry holds the enabled datasources in precedence order. It is built at
// startup and rebuilt by Reload when an administrator changes the table. A
// datasource's rate limiter outlives a reload, so a change does not reset
// the quota spent.
type Registry struct {
	store     Store
	factories map[string]Factory
	log       *slog.Logger

	mu       sync.RWMutex
	entries  []*Entry
	limiters map[string]*rate.Limiter
}

// New reads the datasources table and pairs each enabled row with the
// integration of its name. It fails on an enabled row this binary cannot
// serve, whether because it carries no such integration or because the row
// lacks what that integration needs.
func New(ctx context.Context, store Store, factories map[string]Factory, log *slog.Logger) (*Registry, error) {
	r := &Registry{store: store, factories: factories, log: log, limiters: map[string]*rate.Limiter{}}
	if err := r.Reload(ctx); err != nil {
		return nil, err
	}
	return r, nil
}

// Reload reads the datasources table again and replaces the entries. It
// fails as New does, leaving the entries as they were.
func (r *Registry) Reload(ctx context.Context) error {
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
		identity, _ := integration.(Identity)
		entries = append(entries, &Entry{
			Name:        row.Name,
			Precedence:  row.Precedence,
			Integration: integration,
			Identity:    identity,
		})
	}
	for name := range r.factories {
		if !named[name] {
			r.log.Info("integration has no datasource row", "datasource", name)
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, e := range entries {
		limit, burst := e.Integration.Limit()
		e.limiter = r.limiter(e.Name, limit, burst)
	}
	r.entries = entries
	return nil
}

// limiter returns the datasource's limiter at the rate given, made on its
// first use. The caller holds mu.
func (r *Registry) limiter(name string, limit rate.Limit, burst int) *rate.Limiter {
	l, ok := r.limiters[name]
	if !ok {
		l = rate.NewLimiter(limit, burst)
		r.limiters[name] = l
		return l
	}
	l.SetLimit(limit)
	l.SetBurst(burst)
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
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.entries
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
