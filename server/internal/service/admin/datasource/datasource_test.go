package datasource

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"github.com/google/uuid"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	admindatasourcev1 "github.com/leedenison/stonks/proto/admin/datasource/v1"
	"github.com/leedenison/stonks/proto/admin/datasource/v1/admindatasourcev1connect"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
	servicemock "github.com/leedenison/stonks/server/internal/service/mock"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

var (
	adminID   = uuid.MustParse("00000000-0000-0000-0000-000000000002")
	principal = auth.Principal{User: gen.User{ID: adminID, Email: "admin@example.com", Role: gen.UserRoleAdmin}, SessionID: servicetest.Session}
)

type fixture struct {
	store   *MockStore
	sources *MockSources
	cache   *MockDropper
	client  admindatasourcev1connect.DatasourceServiceClient
	// txErrs is what each transaction returned, so a test sees one rolled
	// back.
	txErrs []error
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicemock.NewMockAuthenticator(ctrl)
	authn.EXPECT().Authenticate(gomock.Any(), servicetest.Session).Return(principal, nil).AnyTimes()
	f := &fixture{store: NewMockStore(ctrl), sources: NewMockSources(ctrl), cache: NewMockDropper(ctrl)}
	f.store.EXPECT().Tx(gomock.Any(), gomock.Any()).DoAndReturn(func(_ context.Context, fn func(Queries) error) error {
		err := fn(f.store)
		f.txErrs = append(f.txErrs, err)
		return err
	}).AnyTimes()
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(admindatasourcev1connect.NewDatasourceServiceHandler(New(f.store, f.sources, f.cache), opts...))
	})
	f.client = admindatasourcev1connect.NewDatasourceServiceClient(srv.Client, srv.URL)
	return f
}

func TestListDatasources(t *testing.T) {
	f := newFixture(t)
	f.store.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{
		{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Credential: ptr.To("secret"), Config: []byte(`{"plan": "basic"}`)},
		{Name: "other", Precedence: 20, Config: []byte("{}")},
	}, nil)
	f.sources.EXPECT().DefaultEndpoint("openfigi").Return("https://api.stub")
	f.sources.EXPECT().DefaultEndpoint("other").Return("")
	res, err := f.client.ListDatasources(context.Background(), connect.NewRequest(&admindatasourcev1.ListDatasourcesRequest{}))
	if err != nil {
		t.Fatalf("ListDatasources() error = %v", err)
	}
	want := &admindatasourcev1.ListDatasourcesResponse{Datasources: []*admindatasourcev1.Datasource{
		{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: "http://stub", DefaultEndpoint: "https://api.stub", Credential: "secret", Config: config(t, map[string]any{"plan": "basic"})},
		{Name: "other", Precedence: 20, Config: config(t, nil)},
	}}
	if diff := cmp.Diff(want, res.Msg, protocmp.Transform()); diff != "" {
		t.Errorf("ListDatasources() mismatch (-want +got):\n%s", diff)
	}
}

// config returns the struct of m.
func config(t *testing.T, m map[string]any) *structpb.Struct {
	t.Helper()
	s, err := structpb.NewStruct(m)
	if err != nil {
		t.Fatalf("NewStruct(%v): %v", m, err)
	}
	return s
}

// TestUpdateDatasource checks that the row is written as the request
// states it and the registry reloaded, and that a change the registry would
// refuse is rolled back.
func TestUpdateDatasource(t *testing.T) {
	stub := gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Config: []byte("{}")}
	empty := []byte("{}")
	tests := []struct {
		name     string
		req      *admindatasourcev1.UpdateDatasourceRequest
		wantArg  gen.UpdateDatasourceParams
		row      gen.Datasource
		err      error
		checkErr error
		// checked is whether the registry is asked to check the row, which
		// it is when the row is enabled.
		checked   bool
		reloadErr error
		dropErr   error
		want      *admindatasourcev1.Datasource
		wantCode  connect.Code
	}{
		{
			name:    "enabled with an endpoint and a credential",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true, Endpoint: "http://stub", Credential: "secret"},
			wantArg: gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Endpoint: "http://stub", Credential: "secret", Config: empty},
			row:     gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Credential: ptr.To("secret"), Config: []byte("{}")},
			checked: true,
			want:    &admindatasourcev1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: "http://stub", DefaultEndpoint: "https://api.stub", Credential: "secret", Config: config(t, nil)},
		},
		{
			name:    "a request without a config writes the empty object",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true},
			wantArg: gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: empty},
			row:     stub,
			checked: true,
			want:    &admindatasourcev1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: "http://stub", DefaultEndpoint: "https://api.stub", Config: config(t, nil)},
		},
		{
			name:    "a config is written as JSON",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true, Config: config(t, map[string]any{"plan": "basic"})},
			wantArg: gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: []byte(`{"plan":"basic"}`)},
			row:     gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Config: []byte(`{"plan": "basic"}`)},
			checked: true,
			want:    &admindatasourcev1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, DefaultEndpoint: "https://api.stub", Config: config(t, map[string]any{"plan": "basic"})},
		},
		{
			name:    "disabled with the endpoint and credential empty",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "absent"},
			wantArg: gen.UpdateDatasourceParams{Name: "absent", Config: empty},
			row:     gen.Datasource{Name: "absent", Precedence: 20, Config: []byte("{}")},
			want:    &admindatasourcev1.Datasource{Name: "absent", Precedence: 20, Config: config(t, nil)},
		},
		{
			name:     "enabling a datasource the registry refuses",
			req:      &admindatasourcev1.UpdateDatasourceRequest{Name: "absent", Enabled: true},
			wantArg:  gen.UpdateDatasourceParams{Name: "absent", Enabled: true, Config: empty},
			row:      gen.Datasource{Name: "absent", Enabled: true, Precedence: 20},
			checked:  true,
			checkErr: market.ErrNoIntegration,
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "no such datasource",
			req:      &admindatasourcev1.UpdateDatasourceRequest{Name: "gone"},
			wantArg:  gen.UpdateDatasourceParams{Name: "gone", Config: empty},
			err:      db.ErrNotFound,
			wantCode: connect.CodeNotFound,
		},
		{
			name:      "the reload fails",
			req:       &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true},
			wantArg:   gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: empty},
			row:       stub,
			checked:   true,
			reloadErr: errors.New("boom"),
			wantCode:  connect.CodeInternal,
		},
		{
			name:     "the drop fails",
			req:      &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true},
			wantArg:  gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: empty},
			row:      stub,
			checked:  true,
			dropErr:  errors.New("boom"),
			wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.store.EXPECT().UpdateDatasource(gomock.Any(), tc.wantArg).Return(tc.row, tc.err)
			if tc.checked {
				f.sources.EXPECT().Check(market.ConfigOf(tc.row)).Return(tc.checkErr)
			}
			if tc.err == nil && tc.checkErr == nil {
				f.sources.EXPECT().Reload(gomock.Any()).Return(tc.reloadErr)
			}
			// Drop only after the registry accepts the row.
			if tc.err == nil && tc.checkErr == nil && tc.reloadErr == nil {
				f.cache.EXPECT().Drop(gomock.Any(), tc.row.Name).Return(tc.dropErr)
			}
			if tc.want != nil {
				f.sources.EXPECT().DefaultEndpoint("openfigi").Return("https://api.stub").AnyTimes()
				f.sources.EXPECT().DefaultEndpoint("absent").Return("").AnyTimes()
			}
			res, err := f.client.UpdateDatasource(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("UpdateDatasource() code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
			}
			if tc.checkErr != nil && (len(f.txErrs) != 1 || f.txErrs[0] == nil) {
				t.Errorf("transactions returned %v, want the one rolled back", f.txErrs)
			}
			if tc.want == nil {
				return
			}
			if diff := cmp.Diff(&admindatasourcev1.UpdateDatasourceResponse{Datasource: tc.want}, res.Msg, protocmp.Transform()); diff != "" {
				t.Errorf("UpdateDatasource() mismatch (-want +got):\n%s", diff)
			}
		})
	}
}

// TestReorderDatasources checks that the names are written in the order
// given and that a list naming the datasources wrongly is refused.
func TestReorderDatasources(t *testing.T) {
	rows := []gen.Datasource{{Name: "alpha", Precedence: 1}, {Name: "beta", Precedence: 2}}
	tests := []struct {
		name     string
		names    []string
		writes   bool
		wantCode connect.Code
	}{
		{name: "reversed", names: []string{"beta", "alpha"}, writes: true},
		{name: "one named twice", names: []string{"beta", "beta"}, wantCode: connect.CodeInvalidArgument},
		{name: "one left out", names: []string{"beta"}, wantCode: connect.CodeInvalidArgument},
		{name: "one that is no datasource", names: []string{"beta", "alpha", "gamma"}, wantCode: connect.CodeInvalidArgument},
		{name: "an empty list", names: nil, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.names != nil {
				f.store.EXPECT().ListDatasources(gomock.Any()).Return(rows, nil)
			}
			if tc.writes {
				f.store.EXPECT().SetDatasourcePrecedence(gomock.Any(), tc.names).Return(nil)
				f.sources.EXPECT().Reload(gomock.Any()).Return(nil)
			}
			_, err := f.client.ReorderDatasources(context.Background(), connect.NewRequest(&admindatasourcev1.ReorderDatasourcesRequest{Names: tc.names}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("ReorderDatasources(%v) code = %v (err %v), want %v", tc.names, servicetest.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
