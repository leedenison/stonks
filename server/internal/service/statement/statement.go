// Package statement serves stonks.statement.v1.
//
// A statement is read with the caller's user id in the query, so another
// user's is not found rather than forbidden. A statement the ingester cannot
// read is an invalid argument; a row it rejects is not, and is read back as
// an item.
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
	runsvc "github.com/leedenison/stonks/server/internal/service/run"
	"github.com/leedenison/stonks/server/internal/statement"
)

// Ingester starts the ingestion of a statement.
type Ingester interface {
	Create(ctx context.Context, userID uuid.UUID, msg *statementv1.Statement) (gen.Run, error)
}

// Reader is the view of the statement queries this package depends on.
type Reader interface {
	ListStatements(ctx context.Context, userID uuid.UUID) ([]gen.ListStatementsRow, error)
	GetStatement(ctx context.Context, arg gen.GetStatementParams) (gen.GetStatementRow, error)
	ListStatementItems(ctx context.Context, arg gen.ListStatementItemsParams) ([]gen.StatementItem, error)
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

// CreateStatement starts ingesting the statement and answers with its run.
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
	return connect.NewResponse(&statementv1.CreateStatementResponse{Run: runsvc.ToProto(run)}), nil
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

// GetStatement reads one of the caller's statements with its items.
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
	return connect.NewResponse(out), nil
}

func summary(st gen.Statement, run gen.Run, rejected int32) *statementv1.StatementSummary {
	return &statementv1.StatementSummary{
		Run:         runsvc.ToProto(run),
		Broker:      db.ToProto[typev1.Broker](st.Broker),
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
