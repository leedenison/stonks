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
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListDatasourceSettings(ctx context.Context) ([]gen.ListDatasourceSettingsRow, error)
	ListDatasources(ctx context.Context) ([]gen.Datasource, error)
	UpdateDatasource(ctx context.Context, arg gen.UpdateDatasourceParams) (gen.Datasource, error)
	SetDatasourcePrecedence(ctx context.Context, names []string) error
}

var _ Queries = (*gen.Queries)(nil)

// Store is the database as this package sees it: the queries, and Tx, which
// runs a set of them in one transaction.
type Store interface {
	Queries
	Tx(ctx context.Context, fn func(Queries) error) error
}

var _ Store = (*db.DB[Queries])(nil)

// Sources is this package's view of the datasource registry: the check that
// the registry would accept a row, and the reload that makes a change to the
// table take effect.
type Sources interface {
	Check(cfg market.Config) error
	Reload(ctx context.Context) error
}

var _ Sources = (*market.Registry)(nil)

// Dropper is this package's view of the fetch cache: the drop of a
// datasource's entries once its settings change.
type Dropper interface {
	Drop(ctx context.Context, datasource string) error
}

var _ Dropper = market.Cache(nil)

//go:generate go tool mockgen -source=datasource.go -destination=datasource_mock_test.go -package=datasource

// Server implements DatasourceService.
type Server struct {
	store   Store
	sources Sources
	cache   Dropper
}

var _ admindatasourcev1connect.DatasourceServiceHandler = (*Server)(nil)

// New returns a Server.
func New(store Store, sources Sources, cache Dropper) *Server {
	return &Server{store: store, sources: sources, cache: cache}
}

// ListDatasources lists the registered datasources in precedence order.
func (s *Server) ListDatasources(ctx context.Context, _ *connect.Request[admindatasourcev1.ListDatasourcesRequest]) (*connect.Response[admindatasourcev1.ListDatasourcesResponse], error) {
	rows, err := s.store.ListDatasourceSettings(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &admindatasourcev1.ListDatasourcesResponse{}
	for _, r := range rows {
		d, err := datasource(r.Name, r.Enabled, r.Precedence, r.Endpoint, r.HasCredential, r.Config)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		out.Datasources = append(out.Datasources, d)
	}
	return connect.NewResponse(out), nil
}

// UpdateDatasource sets a datasource's state, endpoint, credential and
// config, reloads the registry so the change takes effect, and drops the
// datasource's cached answers. If the registry would refuse the row, the
// row stays as it was.
func (s *Server) UpdateDatasource(ctx context.Context, req *connect.Request[admindatasourcev1.UpdateDatasourceRequest]) (*connect.Response[admindatasourcev1.UpdateDatasourceResponse], error) {
	m := req.Msg
	arg := gen.UpdateDatasourceParams{Name: m.GetName(), Enabled: m.GetEnabled(), Endpoint: m.Endpoint, Credential: m.Credential}
	if m.Config != nil {
		cfg, err := json.Marshal(m.Config.AsMap())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("config: %w", err))
		}
		arg.Config = cfg
	}
	var row gen.Datasource
	var refused error
	err := s.store.Tx(ctx, func(q Queries) error {
		var err error
		if row, err = q.UpdateDatasource(ctx, arg); err != nil {
			return err
		}
		if row.Enabled {
			refused = s.sources.Check(market.ConfigOf(row))
		}
		return refused
	})
	switch {
	case refused != nil:
		return nil, connect.NewError(connect.CodeFailedPrecondition, refused)
	case errors.Is(err, db.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no datasource named %s", m.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	if err := s.cache.Drop(context.WithoutCancel(ctx), row.Name); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("drop cached answers: %w", err))
	}
	d, err := datasource(row.Name, row.Enabled, row.Precedence, row.Endpoint, row.Credential != nil, row.Config)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&admindatasourcev1.UpdateDatasourceResponse{Datasource: d}), nil
}

// reload rebuilds the registry once the table has changed. It runs to the
// end whether or not the caller has gone, so the table and the registry stay
// in step.
func (s *Server) reload(ctx context.Context) error {
	if err := s.sources.Reload(context.WithoutCancel(ctx)); err != nil {
		return connect.NewError(connect.CodeInternal, fmt.Errorf("reload datasources: %w", err))
	}
	return nil
}

// ReorderDatasources gives each datasource the precedence of its position in
// the list, and reloads the registry. The cache keeps its entries, because
// precedence orders answers after they are served. A list that does not
// name every datasource exactly once is refused.
func (s *Server) ReorderDatasources(ctx context.Context, req *connect.Request[admindatasourcev1.ReorderDatasourcesRequest]) (*connect.Response[admindatasourcev1.ReorderDatasourcesResponse], error) {
	names := req.Msg.GetNames()
	var invalid error
	err := s.store.Tx(ctx, func(q Queries) error {
		rows, err := q.ListDatasources(ctx)
		if err != nil {
			return err
		}
		if invalid = everyOnce(names, rows); invalid != nil {
			return invalid
		}
		return q.SetDatasourcePrecedence(ctx, names)
	})
	switch {
	case invalid != nil:
		return nil, connect.NewError(connect.CodeInvalidArgument, invalid)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.reload(ctx); err != nil {
		return nil, err
	}
	return connect.NewResponse(&admindatasourcev1.ReorderDatasourcesResponse{}), nil
}

// everyOnce reports why names does not name every datasource of rows exactly
// once, if it does not.
func everyOnce(names []string, rows []gen.Datasource) error {
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return fmt.Errorf("%s is named twice", n)
		}
		seen[n] = true
	}
	for _, r := range rows {
		if !seen[r.Name] {
			return fmt.Errorf("%s is not named", r.Name)
		}
	}
	if len(names) != len(rows) {
		return errors.New("a name is no datasource")
	}
	return nil
}

func datasource(name string, enabled bool, precedence int32, endpoint *string, held bool, config []byte) (*admindatasourcev1.Datasource, error) {
	cfg := &structpb.Struct{}
	if err := cfg.UnmarshalJSON(config); err != nil {
		return nil, fmt.Errorf("config of datasource %s: %w", name, err)
	}
	return &admindatasourcev1.Datasource{Name: name, Enabled: enabled, Precedence: precedence, Endpoint: endpoint, HasCredential: held, Config: cfg}, nil
}
