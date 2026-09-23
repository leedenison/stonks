package datasource

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"

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
			store := NewMockStore(ctrl)
			store.EXPECT().ListDatasources(gomock.Any()).Return(tc.rows, nil)

			r, err := New(ctx, store, tc.factories, discard())
			if (err != nil) != tc.wantErr {
				t.Fatalf("New() error = %v, wantErr %v", err, tc.wantErr)
			}
			if tc.wantErr {
				return
			}
			var got []string
			for _, e := range r.Enabled() {
				got = append(got, e.Name)
			}
			if len(got) != len(tc.want) {
				t.Fatalf("Enabled() = %v, want %v", got, tc.want)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("Enabled() = %v, want %v", got, tc.want)
				}
			}
		})
	}
}

// TestRegistryConfig checks that the row reaches the integration that it
// names, with a NULL credential and endpoint read as empty.
func TestRegistryConfig(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockStore(ctrl)
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
	for i := range want {
		if seen[i] != want[i] {
			t.Errorf("config %d = %+v, want %+v", i, seen[i], want[i])
		}
	}
}

// TestRegistryStoreFails checks that a table that cannot be read stops the
// boot rather than yielding an empty registry.
func TestRegistryStoreFails(t *testing.T) {
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	store := NewMockStore(ctrl)
	store.EXPECT().ListDatasources(gomock.Any()).Return(nil, errors.New("no database"))
	if _, err := New(context.Background(), store, nil, discard()); err == nil {
		t.Error("New() with an unreadable table: err = nil, want an error")
	}
}
