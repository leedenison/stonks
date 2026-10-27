// Package datasource serves stonks.admin.datasource.v1.
package datasource

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"google.golang.org/protobuf/types/known/structpb"

	admindatasourcev1 "github.com/leedenison/stonks/proto/admin/datasource/v1"
	"github.com/leedenison/stonks/proto/admin/datasource/v1/admindatasourcev1connect"
	"github.com/leedenison/stonks/server/internal/datasource"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListDatasources(ctx context.Context) ([]gen.Datasource, error)
}

var _ Queries = (*gen.Queries)(nil)

// Sources is this package's view of the datasource registry: the address an
// integration calls by default.
type Sources interface {
	DefaultEndpoint(name string) string
}

var _ Sources = (*market.Registry)(nil)

// Updater is this package's view of the datasource service.
type Updater interface {
	Update(ctx context.Context, arg gen.UpdateDatasourceParams) (gen.Datasource, error)
	Reorder(ctx context.Context, names []string) (gen.Run, error)
}

var _ Updater = (*datasource.Service)(nil)

//go:generate go tool mockgen -source=datasource.go -destination=mock/datasource_mock.go -package=mock

// Server implements DatasourceService.
type Server struct {
	queries Queries
	sources Sources
	updater Updater
}

var _ admindatasourcev1connect.DatasourceServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries, sources Sources, updater Updater) *Server {
	return &Server{queries: queries, sources: sources, updater: updater}
}

// ListDatasources lists the registered datasources in precedence order.
func (s *Server) ListDatasources(ctx context.Context, _ *connect.Request[admindatasourcev1.ListDatasourcesRequest]) (*connect.Response[admindatasourcev1.ListDatasourcesResponse], error) {
	rows, err := s.queries.ListDatasources(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admindatasourcev1.ListDatasourcesResponse{}
	for _, row := range rows {
		d, err := s.datasource(row)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		out.Datasources = append(out.Datasources, d)
	}
	return connect.NewResponse(out), nil
}

// UpdateDatasource writes a datasource's state and settings as given; see
// [datasource.go](../../../datasource/datasource.go).
func (s *Server) UpdateDatasource(ctx context.Context, req *connect.Request[admindatasourcev1.UpdateDatasourceRequest]) (*connect.Response[admindatasourcev1.UpdateDatasourceResponse], error) {
	m := req.Msg
	cfg, err := json.Marshal(m.GetConfig().AsMap())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("config: %w", err))
	}
	arg := gen.UpdateDatasourceParams{Name: m.GetName(), Enabled: m.GetEnabled(), Endpoint: m.GetEndpoint(), Credential: m.GetCredential(), Config: cfg}
	row, err := s.updater.Update(ctx, arg)
	switch {
	case errors.Is(err, datasource.ErrRefused):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case errors.Is(err, db.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no datasource named %s", m.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	d, err := s.datasource(row)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admindatasourcev1.UpdateDatasourceResponse{Datasource: d}), nil
}

// ReorderDatasources gives each datasource the precedence of its position in
// the list, and returns the rebuild resolved tickers run it started. A list
// that does not name every datasource exactly once is refused.
func (s *Server) ReorderDatasources(ctx context.Context, req *connect.Request[admindatasourcev1.ReorderDatasourcesRequest]) (*connect.Response[admindatasourcev1.ReorderDatasourcesResponse], error) {
	started, err := s.updater.Reorder(ctx, req.Msg.GetNames())
	switch {
	case errors.Is(err, datasource.ErrOrder):
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admindatasourcev1.ReorderDatasourcesResponse{Run: to.ProtoRun(started)}), nil
}

// datasource writes a row as its proto message, with the address its
// integration calls when the row sets none.
func (s *Server) datasource(row gen.Datasource) (*admindatasourcev1.Datasource, error) {
	cfg := &structpb.Struct{}
	if err := cfg.UnmarshalJSON(row.Config); err != nil {
		return nil, fmt.Errorf("config of datasource %s: %w", row.Name, err)
	}
	return &admindatasourcev1.Datasource{
		Name:            row.Name,
		Enabled:         row.Enabled,
		Precedence:      row.Precedence,
		Endpoint:        ptr.Deref(row.Endpoint, ""),
		DefaultEndpoint: s.sources.DefaultEndpoint(row.Name),
		Credential:      ptr.Deref(row.Credential, ""),
		Config:          cfg,
	}, nil
}
