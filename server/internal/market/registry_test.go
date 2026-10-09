package market

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"sync"
	"testing"
	"time"

	"github.com/google/go-cmp/cmp"
	"github.com/google/go-cmp/cmp/cmpopts"
	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func discard() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func row(name string, enabled bool, precedence int32) gen.Datasource {
	return gen.Datasource{Name: name, Enabled: enabled, Precedence: precedence}
}

// TestRegistry checks which rows the boot accepts, and in what order.
func TestRegistry(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name      string
		rows      []gen.Datasource
		factories map[string]Factory
		wantErr   bool
		want      []string
	}{
		{
			name:      "an enabled row the binary carries",
			rows:      []gen.Datasource{row("one", true, 10)},
			factories: map[string]Factory{"one": factoryOf(&fake{})},
			want:      []string{"one"},
		},
		{
			name:      "an enabled row naming no integration",
			rows:      []gen.Datasource{row("absent", true, 10)},
			factories: map[string]Factory{"one": factoryOf(&fake{})},
			wantErr:   true,
		},
		{
			name:      "an enabled row the integration refuses",
			rows:      []gen.Datasource{row("one", true, 10)},
			factories: map[string]Factory{"one": failingFactory("no credential")},
			wantErr:   true,
		},
		{
			name:      "a disabled row naming no integration",
			rows:      []gen.Datasource{row("absent", false, 10), row("one", true, 20)},
			factories: map[string]Factory{"one": factoryOf(&fake{})},
			want:      []string{"one"},
		},
		{
			name:      "an integration with no row",
			rows:      nil,
			factories: map[string]Factory{"one": factoryOf(&fake{})},
			want:      nil,
		},
		{
			name:      "the table's order is kept",
			rows:      []gen.Datasource{row("alpha", true, 10), row("beta", true, 10), row("gamma", true, 5)},
			factories: map[string]Factory{"alpha": factoryOf(&fake{}), "beta": factoryOf(&fake{}), "gamma": factoryOf(&fake{})},
			want:      []string{"alpha", "beta", "gamma"},
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			t.Cleanup(ctrl.Finish)
			store := NewMockQueries(ctrl)
			store.EXPECT().ListDatasources(gomock.Any()).Return(tc.rows, nil)

			r, err := New(ctx, store, tc.factories, discard())
			if (err != nil) != tc.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			if diff := cmp.Diff(tc.want, r.Names(), cmpopts.EquateEmpty()); diff != "" {
				t.Errorf("Names() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestRegistryConfig checks that the row reaches the integration that it
// names, with a NULL credential and endpoint read as empty.
func TestRegistryConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockQueries(ctrl)
	store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{
		{Name: "one", Enabled: true, Credential: ptr.To("secret"), Endpoint: ptr.To("http://stub")},
		{Name: "two", Enabled: true},
	}, nil)

	var seen []Config
	factory := func(c Config) (Integration, error) {
		seen = append(seen, c)
		return &fake{}, nil
	}
	if _, err := New(context.Background(), store, map[string]Factory{"one": factory, "two": factory}, discard()); err != nil {
		t.Fatalf("New() error = %v", err)
	}
	want := []Config{{Name: "one", Credential: "secret", Endpoint: "http://stub"}, {Name: "two"}}
	if diff := cmp.Diff(want, seen); diff != "" {
		t.Errorf("configs mismatch (-want +got):\n%s", diff)
	}
}

// TestRegistryStoreFails checks that a table that cannot be read stops the
// boot rather than yielding an empty registry.
func TestRegistryStoreFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockQueries(ctrl)
	store.EXPECT().ListDatasources(gomock.Any()).Return(nil, errors.New("no database"))
	if _, err := New(context.Background(), store, nil, discard()); err == nil {
		t.Error("New() with an unreadable table: err = nil, want an error")
	}
}

// TestRegistryReload checks that a reload replaces the entries, keeps a
// datasource's limiter with its wait and pause, and leaves the entries as they
// were when it fails.
func TestRegistryReload(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockQueries(ctrl)
	factories := map[string]Factory{"one": factoryOf(&fake{}), "two": factoryOf(&fake{})}
	store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{row("one", true, 10)}, nil)
	r, err := New(ctx, store, factories, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	before := r.Enabled()[0].limiter
	before.waitUntil(start.Add(time.Second))
	before.pause(start.Add(time.Hour), "quota spent")

	store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{row("two", true, 5), row("one", true, 10)}, nil)
	if err := r.Reload(ctx); err != nil {
		t.Fatalf("Reload() error = %v", err)
	}
	if diff := cmp.Diff([]string{"two", "one"}, r.Names()); diff != "" {
		t.Errorf("Names() after reload mismatch (-want +got):\n%s", diff)
	}
	kept := r.Enabled()[1].limiter
	if _, paused := kept.paused(start); kept != before || kept.waits(start) != time.Second || !paused {
		t.Error("the limiter of one, its wait or its pause was replaced by the reload, want all kept")
	}

	store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{row("absent", true, 1)}, nil)
	if err := r.Reload(ctx); err == nil {
		t.Error("Reload() with an enabled row the build lacks: err = nil, want an error")
	}
	if got := r.Names(); len(got) != 2 {
		t.Errorf("Names() after a failed reload = %v, want the two kept", got)
	}
}

// TestRegistryReloadOverlap checks that when two reloads overlap, the entries
// come from the reload that read the table second, whichever reload stores
// its entries last.
func TestRegistryReloadOverlap(t *testing.T) {
	ctx := context.Background()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockQueries(ctrl)
	factories := map[string]Factory{"one": factoryOf(&fake{}), "two": factoryOf(&fake{})}
	store.EXPECT().ListDatasources(gomock.Any()).Return(nil, nil)
	r, err := New(ctx, store, factories, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	first := store.EXPECT().ListDatasources(gomock.Any()).DoAndReturn(func(context.Context) ([]gen.Datasource, error) {
		close(entered)
		<-release
		return []gen.Datasource{row("one", true, 1)}, nil
	})
	store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{row("two", true, 1)}, nil).After(first)

	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		if err := r.Reload(ctx); err != nil {
			t.Errorf("first Reload() error = %v", err)
		}
	}()
	<-entered
	go func() {
		defer wg.Done()
		if err := r.Reload(ctx); err != nil {
			t.Errorf("second Reload() error = %v", err)
		}
	}()
	close(release)
	wg.Wait()
	if diff := cmp.Diff([]string{"two"}, r.Names()); diff != "" {
		t.Errorf("Names() after overlapping reloads mismatch (-want +got):\n%s", diff)
	}
}

// TestRegistryCheck checks that Check refuses an unknown datasource name and
// a config its integration rejects, and installs nothing.
func TestRegistryCheck(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockQueries(ctrl)
	store.EXPECT().ListDatasources(gomock.Any()).Return(nil, nil)
	factories := map[string]Factory{"one": factoryOf(&fake{}), "refusing": failingFactory("no credential")}
	r, err := New(context.Background(), store, factories, discard())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if err := r.Check(Config{Name: "one"}); err != nil {
		t.Errorf("Check(one) error = %v, want nil", err)
	}
	if err := r.Check(Config{Name: "absent"}); !errors.Is(err, ErrNoIntegration) {
		t.Errorf("Check(absent) error = %v, want ErrNoIntegration", err)
	}
	if err := r.Check(Config{Name: "refusing"}); err == nil {
		t.Error("Check(refusing) error = nil, want the factory's")
	}
	if len(r.Enabled()) != 0 {
		t.Errorf("Enabled() = %v after Check, want nothing installed", r.Names())
	}
}
