package datasource

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"testing"

	"connectrpc.com/connect"
	"github.com/google/go-cmp/cmp"
	"go.uber.org/mock/gomock"
	"google.golang.org/protobuf/testing/protocmp"
	"google.golang.org/protobuf/types/known/structpb"

	admindatasourcev1 "github.com/leedenison/stonks/proto/admin/datasource/v1"
	"github.com/leedenison/stonks/proto/admin/datasource/v1/admindatasourcev1connect"
	"github.com/leedenison/stonks/server/internal/datasource"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/service/servicetest"
)

type fixture struct {
	queries *MockQueries
	sources *MockSources
	updater *MockUpdater
	client  admindatasourcev1connect.DatasourceServiceClient
}

// newFixture mounts a Server with the real handler chain, and a client whose
// session belongs to an administrator.
func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	t.Cleanup(ctrl.Finish)
	authn := servicetest.AsAdmin(ctrl)
	f := &fixture{queries: NewMockQueries(ctrl), sources: NewMockSources(ctrl), updater: NewMockUpdater(ctrl)}
	opts := servicetest.Options(t, authn)
	srv := servicetest.Serve(t, func(mux *http.ServeMux) {
		mux.Handle(admindatasourcev1connect.NewDatasourceServiceHandler(New(f.queries, f.sources, f.updater), opts...))
	})
	f.client = admindatasourcev1connect.NewDatasourceServiceClient(srv.Client, srv.URL)
	return f
}

func TestListDatasources(t *testing.T) {
	f := newFixture(t)
	f.queries.EXPECT().ListDatasources(gomock.Any()).Return([]gen.Datasource{
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

// TestUpdateDatasource checks that the request is written as the service's
// argument, the row returned as its message, and each error of the service
// mapped to its code.
func TestUpdateDatasource(t *testing.T) {
	stub := gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Config: []byte("{}")}
	empty := []byte("{}")
	tests := []struct {
		name     string
		req      *admindatasourcev1.UpdateDatasourceRequest
		wantArg  gen.UpdateDatasourceParams
		row      gen.Datasource
		err      error
		want     *admindatasourcev1.Datasource
		wantCode connect.Code
	}{
		{
			name:    "enabled with an endpoint and a credential",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true, Endpoint: "http://stub", Credential: "secret"},
			wantArg: gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Endpoint: "http://stub", Credential: "secret", Config: empty},
			row:     gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: ptr.To("http://stub"), Credential: ptr.To("secret"), Config: []byte("{}")},
			want:    &admindatasourcev1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: "http://stub", DefaultEndpoint: "https://api.stub", Credential: "secret", Config: config(t, nil)},
		},
		{
			name:    "a request without a config writes the empty object",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true},
			wantArg: gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: empty},
			row:     stub,
			want:    &admindatasourcev1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Endpoint: "http://stub", DefaultEndpoint: "https://api.stub", Config: config(t, nil)},
		},
		{
			name:    "a config is written as JSON",
			req:     &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true, Config: config(t, map[string]any{"plan": "basic"})},
			wantArg: gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: []byte(`{"plan":"basic"}`)},
			row:     gen.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, Config: []byte(`{"plan": "basic"}`)},
			want:    &admindatasourcev1.Datasource{Name: "openfigi", Enabled: true, Precedence: 10, DefaultEndpoint: "https://api.stub", Config: config(t, map[string]any{"plan": "basic"})},
		},
		{
			name:     "a datasource the registry refuses",
			req:      &admindatasourcev1.UpdateDatasourceRequest{Name: "absent", Enabled: true},
			wantArg:  gen.UpdateDatasourceParams{Name: "absent", Enabled: true, Config: empty},
			err:      fmt.Errorf("x: %w", datasource.ErrRefused),
			wantCode: connect.CodeFailedPrecondition,
		},
		{
			name:     "no such datasource",
			req:      &admindatasourcev1.UpdateDatasourceRequest{Name: "gone"},
			wantArg:  gen.UpdateDatasourceParams{Name: "gone", Config: empty},
			err:      fmt.Errorf("x: %w", db.ErrNotFound),
			wantCode: connect.CodeNotFound,
		},
		{
			name:     "any other failure",
			req:      &admindatasourcev1.UpdateDatasourceRequest{Name: "openfigi", Enabled: true},
			wantArg:  gen.UpdateDatasourceParams{Name: "openfigi", Enabled: true, Config: empty},
			err:      errors.New("boom"),
			wantCode: connect.CodeInternal,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			f.updater.EXPECT().Update(gomock.Any(), tc.wantArg).Return(tc.row, tc.err)
			if tc.want != nil {
				f.sources.EXPECT().DefaultEndpoint("openfigi").Return("https://api.stub")
			}
			res, err := f.client.UpdateDatasource(context.Background(), connect.NewRequest(tc.req))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Fatalf("UpdateDatasource() code = %v (err %v), want %v", servicetest.CodeOf(err), err, tc.wantCode)
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

// TestReorderDatasources checks that the names reach the service in the
// order given, and that each refusal is mapped to its code.
func TestReorderDatasources(t *testing.T) {
	tests := []struct {
		name     string
		names    []string
		err      error
		wantCode connect.Code
	}{
		{name: "reversed", names: []string{"beta", "alpha"}},
		{name: "a list the service refuses", names: []string{"beta", "beta"}, err: fmt.Errorf("x: %w", datasource.ErrOrder), wantCode: connect.CodeInvalidArgument},
		{name: "any other failure", names: []string{"beta", "alpha"}, err: errors.New("boom"), wantCode: connect.CodeInternal},
		{name: "an empty list", names: nil, wantCode: connect.CodeInvalidArgument},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t)
			if tc.names != nil {
				f.updater.EXPECT().Reorder(gomock.Any(), tc.names).Return(tc.err)
			}
			_, err := f.client.ReorderDatasources(context.Background(), connect.NewRequest(&admindatasourcev1.ReorderDatasourcesRequest{Names: tc.names}))
			if servicetest.CodeOf(err) != tc.wantCode {
				t.Errorf("ReorderDatasources(%v) code = %v (err %v), want %v", tc.names, servicetest.CodeOf(err), err, tc.wantCode)
			}
		})
	}
}
