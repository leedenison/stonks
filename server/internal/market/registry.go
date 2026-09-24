package market

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/time/rate"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Entry is one enabled datasource and the integration serving it. A field
// per kind of data is set where the integration serves that kind, and nil
// where the datasource is not asked for it.
type Entry struct {
	Name        string
	Precedence  int32
	Integration Integration
	Identity    Identity

	limiter *rate.Limiter
}

// Registry holds the enabled datasources in precedence order. It is built
// once and read from then on.
type Registry struct {
	entries []*Entry
}

// New reads the datasources table and pairs each enabled row with the
// integration of its name. It fails on an enabled row this binary cannot
// serve, whether because it carries no such integration or because the row
// lacks what that integration needs.
func New(ctx context.Context, store Store, factories map[string]Factory, log *slog.Logger) (*Registry, error) {
	rows, err := store.ListDatasources(ctx)
	if err != nil {
		return nil, fmt.Errorf("list datasources: %w", err)
	}
	r := &Registry{}
	named := map[string]bool{}
	for _, row := range rows {
		named[row.Name] = true
		factory, ok := factories[row.Name]
		switch {
		case !row.Enabled:
			log.Info("datasource disabled", "datasource", row.Name, "carried", ok)
			continue
		case !ok:
			return nil, fmt.Errorf("datasource %s is enabled and this build carries no such integration", row.Name)
		}
		integration, err := factory(config(row))
		if err != nil {
			return nil, fmt.Errorf("datasource %s: %w", row.Name, err)
		}
		limit, burst := integration.Limit()
		identity, _ := integration.(Identity)
		r.entries = append(r.entries, &Entry{
			Name:        row.Name,
			Precedence:  row.Precedence,
			Integration: integration,
			Identity:    identity,
			limiter:     rate.NewLimiter(limit, burst),
		})
	}
	for name := range factories {
		if !named[name] {
			log.Info("integration has no datasource row", "datasource", name)
		}
	}
	return r, nil
}

// Enabled returns the enabled datasources in precedence order.
func (r *Registry) Enabled() []*Entry { return r.entries }

// Names returns the enabled datasources in precedence order, named.
func (r *Registry) Names() []string {
	out := make([]string, len(r.entries))
	for i, e := range r.entries {
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
