// Package statement serves stonks.statement.v1.
package statement

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/encoding/protojson"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/proto/statement/v1/statementv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/statement"
)

// Ingester starts the ingestion of a statement.
type Ingester interface {
	Create(ctx context.Context, userID uuid.UUID, msg *statementv1.Statement) (gen.Run, error)
}

// Reader is this package's view of the statement queries.
type Reader interface {
	ListStatements(ctx context.Context, userID uuid.UUID) ([]gen.ListStatementsRow, error)
	GetStatement(ctx context.Context, arg gen.GetStatementParams) (gen.GetStatementRow, error)
	ListStatementItems(ctx context.Context, arg gen.ListStatementItemsParams) ([]gen.StatementItem, error)
	ListStatedKeys(ctx context.Context, arg gen.ListStatedKeysParams) ([]gen.StatedKey, error)
	ListLatestResolutions(ctx context.Context, arg gen.ListLatestResolutionsParams) ([]gen.ResolutionKey, error)
}

var (
	_ Ingester = (*statement.Service)(nil)
	_ Reader   = (*gen.Queries)(nil)
)

//go:generate go tool mockgen -source=statement.go -destination=mock/statement_mock.go -package=mock

// Server implements StatementService.
type Server struct {
	ingester Ingester
	reader   Reader
}

var _ statementv1connect.StatementServiceHandler = (*Server)(nil)

// New returns a Server.
func New(ingester Ingester, reader Reader) *Server {
	return &Server{ingester: ingester, reader: reader}
}

// CreateStatement starts ingesting the statement and answers with its run. A
// statement the ingester cannot read is an invalid argument. A row it rejects
// is not an error; the row is read back as an item of the statement.
func (s *Server) CreateStatement(ctx context.Context, req *connect.Request[statementv1.CreateStatementRequest]) (*connect.Response[statementv1.CreateStatementResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	run, err := s.ingester.Create(ctx, p.User.ID, req.Msg.GetStatement())
	if errors.Is(err, statement.ErrInvalid) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&statementv1.CreateStatementResponse{Run: to.ProtoRun(run)}), nil
}

// ListStatements reads the caller's statements, newest first.
func (s *Server) ListStatements(ctx context.Context, _ *connect.Request[statementv1.ListStatementsRequest]) (*connect.Response[statementv1.ListStatementsResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	rows, err := s.reader.ListStatements(ctx, p.User.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &statementv1.ListStatementsResponse{}
	for _, r := range rows {
		out.Statements = append(out.Statements, summary(r.Statement, r.Run, r.Rejected))
	}
	return connect.NewResponse(out), nil
}

// GetStatement reads one of the caller's statements with its items and its
// keys. The query carries the caller's user id, so another user's statement
// is not found rather than forbidden.
func (s *Server) GetStatement(ctx context.Context, req *connect.Request[statementv1.GetStatementRequest]) (*connect.Response[statementv1.GetStatementResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	id, err := uuid.Parse(req.Msg.GetRunId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := s.reader.GetStatement(ctx, gen.GetStatementParams{ID: id, UserID: p.User.ID})
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such statement"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	items, err := s.reader.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: id, UserID: p.User.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &statementv1.GetStatementResponse{Statement: summary(row.Statement, row.Run, row.Rejected)}
	for _, it := range items {
		item, err := ItemToProto(it)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		out.Items = append(out.Items, item)
	}
	if out.Keys, err = s.keys(ctx, id, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(out), nil
}

// keys answers the statement's keys, each with the outcome of its latest
// resolution where one has recorded it.
func (s *Server) keys(ctx context.Context, statement, user uuid.UUID) ([]*typev1.ResolutionItem, error) {
	keys, err := s.reader.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: statement, UserID: user})
	if err != nil || len(keys) == 0 {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	latest, err := s.reader.ListLatestResolutions(ctx, gen.ListLatestResolutionsParams{Ids: ids, UserID: user})
	if err != nil {
		return nil, err
	}
	outcome := make(map[uuid.UUID]gen.ResolutionKey, len(latest))
	for _, r := range latest {
		outcome[r.StatedKeyID] = r
	}
	out := make([]*typev1.ResolutionItem, 0, len(keys))
	for _, k := range keys {
		item := &typev1.ResolutionItem{StatedKey: to.ProtoStatedKey(k), StatedKeyId: k.ID.String()}
		if r, ok := outcome[k.ID]; ok {
			item.Outcome = types.ToProto[typev1.ResolutionOutcome](r.Outcome)
			item.Reason = r.Reason
		}
		out = append(out, item)
	}
	return out, nil
}

func summary(st gen.Statement, run gen.Run, rejected int32) *statementv1.StatementSummary {
	return &statementv1.StatementSummary{
		Run:         to.ProtoRun(run),
		Broker:      types.ToProto[typev1.Broker](st.Broker),
		OrderFrom:   st.OrderFrom.Format("2006-01-02"),
		OrderBefore: st.OrderBefore.Format("2006-01-02"),
		Rows:        st.RowCount,
		Rejected:    rejected,
	}
}

// ItemToProto converts a rejected row to its message.
func ItemToProto(it gen.StatementItem) (*statementv1.StatementItem, error) {
	row := &statementv1.Row{}
	if err := protojson.Unmarshal(it.Stated, row); err != nil {
		return nil, fmt.Errorf("read item %d: %w", it.Ordinal, err)
	}
	return &statementv1.StatementItem{Ordinal: it.Ordinal, Reason: it.Reason, Row: row}, nil
}
