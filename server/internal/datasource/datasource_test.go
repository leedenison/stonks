package datasource

import (
	"context"
	"errors"
	"testing"

	"go.uber.org/mock/gomock"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

type fixture struct {
	store    *MockStore
	sources  *MockSources
	cache    *MockDropper
	rebuilds *MockRebuilder
	svc      *Service
	// txErrs is what each transaction returned, so a test sees one rolled
	// back.
	txErrs []error
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	f := &fixture{store: NewMockStore(ctrl), sources: NewMockSources(ctrl), cache: NewMockDropper(ctrl), rebuilds: NewMockRebuilder(ctrl)}
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error {
		err := fn(f.store)
		f.txErrs = append(f.txErrs, err)
		return err
	}).AnyTimes()
	f.svc = New(f.store, f.sources, f.cache, f.rebuilds)
	return f
}

// TestUpdate checks that the row is written, the registry reloaded and the
// cache dropped, and that a change the registry would refuse is rolled back.
func TestUpdate(t *testing.T) {
	enabled := gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Config: []byte("{}")}
	disabled := gen.Datasource{Name: "absent", Precedence: 20, Config: []byte("{}")}
	boom := errors.New("boom")
	tests := []struct {
		name      string
		row       gen.Datasource
		err       error
		checkErr  error
		reloadErr error
		dropErr   error
		// wantIs are the errors the result must match.
		wantIs []error
		// wantErr is whether the update fails at all.
		wantErr bool
	}{
		{name: "an enabled row is checked", row: enabled},
		{name: "a disabled row is not checked", row: disabled},
		{name: "a row the registry refuses", row: gen.Datasource{Name: "absent", Enabled: true}, checkErr: market.ErrNoIntegration, wantIs: []error{ErrRefused, market.ErrNoIntegration}, wantErr: true},
		{name: "no such datasource", err: db.ErrNotFound, wantIs: []error{db.ErrNotFound}, wantErr: true},
		{name: "the reload fails", row: enabled, reloadErr: boom, wantIs: []error{boom}, wantErr: true},
		{name: "the drop fails", row: enabled, dropErr: boom, wantIs: []error{boom}, wantErr: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			arg := gen.UpdateDatasourceParams{Name: tc.row.Name, Enabled: tc.row.Enabled, Config: []byte("{}")}
			f.store.EXPECT().UpdateDatasource(gomock.Any(), arg).Return(tc.row, tc.err)
			if tc.err == nil && tc.row.Enabled {
				f.sources.EXPECT().Check(market.ConfigOf(tc.row)).Return(tc.checkErr)
			}
			if tc.err == nil && tc.checkErr == nil {
				f.sources.EXPECT().Reload(gomock.Any()).Return(tc.reloadErr)
			}
			// Drop only after the registry accepts the row.
			if tc.err == nil && tc.checkErr == nil && tc.reloadErr == nil {
				f.cache.EXPECT().Drop(gomock.Any(), tc.row.Name).Return(tc.dropErr)
			}
			got, err := f.svc.Update(context.Background(), arg)
			if (err != nil) != tc.wantErr {
				t.Fatalf("Update() error = %v, want error %v", err, tc.wantErr)
			}
			for _, want := range tc.wantIs {
				if !errors.Is(err, want) {
					t.Errorf("Update() error = %v, want it to match %v", err, want)
				}
			}
			if tc.reloadErr != nil || tc.dropErr != nil {
				if errors.Is(err, ErrRefused) || errors.Is(err, ErrOrder) || errors.Is(err, db.ErrNotFound) {
					t.Errorf("Update() error = %v, want no sentinel", err)
				}
			}
			if tc.checkErr != nil && (len(f.txErrs) != 1 || f.txErrs[0] == nil) {
				t.Errorf("transactions returned %v, want the one rolled back", f.txErrs)
			}
			if !tc.wantErr && got.Name != tc.row.Name {
				t.Errorf("Update() = %+v, want %+v", got, tc.row)
			}
		})
	}
}

// TestReorder checks that the names are written in the order given, that the
// write starts a rebuild of every ticker's names and returns its run, and that
// a list naming the datasources wrongly is refused before a rebuild starts.
func TestReorder(t *testing.T) {
	rows := []gen.Datasource{{Name: "alpha", Precedence: 1}, {Name: "beta", Precedence: 2}}
	tests := []struct {
		name    string
		names   []string
		writes  bool
		refused bool
	}{
		{name: "reversed", names: []string{"beta", "alpha"}, writes: true},
		{name: "one named twice", names: []string{"beta", "beta"}, refused: true},
		{name: "one left out", names: []string{"beta"}, refused: true},
		{name: "one that is no datasource", names: []string{"beta", "alpha", "gamma"}, refused: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.store.EXPECT().ListDatasources(gomock.Any()).Return(rows, nil)
			if tc.writes {
				f.store.EXPECT().SetDatasourcePrecedence(gomock.Any(), tc.names).Return(nil)
				f.sources.EXPECT().Reload(gomock.Any()).Return(nil)
				f.rebuilds.EXPECT().Start(gomock.Any()).Return(rebuild, nil)
			}
			got, err := f.svc.Reorder(context.Background(), tc.names)
			if tc.writes && got.ID != rebuild.ID {
				t.Errorf("Reorder(%v) = %+v, want the rebuild run", tc.names, got)
			}
			if got := errors.Is(err, ErrOrder); got != tc.refused {
				t.Errorf("Reorder(%v) error = %v, want ErrOrder %v", tc.names, err, tc.refused)
			}
			if !tc.refused && err != nil {
				t.Errorf("Reorder(%v) error = %v", tc.names, err)
			}
		})
	}
}

// TestReorderReloadFails checks that a failed reload comes back as itself,
// not as ErrRefused or ErrOrder.
func TestReorderReloadFails(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("boom")
	f.store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{{Name: "alpha"}}, nil)
	f.store.EXPECT().SetDatasourcePrecedence(gomock.Any(), []string{"alpha"}).Return(nil)
	f.sources.EXPECT().Reload(gomock.Any()).Return(boom)
	_, err := f.svc.Reorder(context.Background(), []string{"alpha"})
	if !errors.Is(err, boom) || errors.Is(err, ErrOrder) {
		t.Errorf("Reorder() error = %v, want %v and no sentinel", err, boom)
	}
}

// TestReorderRebuildFails checks that a rebuild that fails to start comes
// back as itself, after the new precedence has committed.
func TestReorderRebuildFails(t *testing.T) {
	f := newFixture(t)
	boom := errors.New("boom")
	f.store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{{Name: "alpha"}}, nil)
	f.store.EXPECT().SetDatasourcePrecedence(gomock.Any(), []string{"alpha"}).Return(nil)
	f.sources.EXPECT().Reload(gomock.Any()).Return(nil)
	f.rebuilds.EXPECT().Start(gomock.Any()).Return(gen.Run{}, boom)
	_, err := f.svc.Reorder(context.Background(), []string{"alpha"})
	if !errors.Is(err, boom) || errors.Is(err, ErrOrder) {
		t.Errorf("Reorder() error = %v, want %v and no sentinel", err, boom)
	}
	if len(f.txErrs) != 1 || f.txErrs[0] != nil {
		t.Errorf("transactions returned %v, want the precedence committed", f.txErrs)
	}
}

// rebuild is the run a reorder starts.
var rebuild = gen.Run{ID: db.NewID(), Kind: gen.RunKindRebuildTickerValidity}
