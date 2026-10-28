// Package stmt serves stonks.stmt.v1.
package stmt

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	"github.com/leedenison/stonks/proto/stmt/v1/stmtv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/stmt"
	"github.com/leedenison/stonks/server/internal/types"
)

// Ingester starts the ingestion of a statement.
type Ingester interface {
	Create(ctx context.Context, userID uuid.UUID, msg *stmtv1.Stmt) (gen.Run, error)
}

// Confirmer lists and confirms the candidates for one of a user's keys.
type Confirmer interface {
	Candidates(ctx context.Context, user, key uuid.UUID) (resolve.Offer, error)
	Confirm(ctx context.Context, user, key uuid.UUID, p resolve.Pick) (gen.ResolutionKey, error)
}

// Reader is this package's view of the statement queries.
type Reader interface {
	tickers.Queries
	ListStmts(ctx context.Context, userID uuid.UUID) ([]gen.ListStmtsRow, error)
	GetStmt(ctx context.Context, arg gen.GetStmtParams) (gen.GetStmtRow, error)
	ListStmtItems(ctx context.Context, arg gen.ListStmtItemsParams) ([]gen.StmtItem, error)
	ListStatedKeys(ctx context.Context, arg gen.ListStatedKeysParams) ([]gen.StatedKey, error)
	ListLatestResolutions(ctx context.Context, arg gen.ListLatestResolutionsParams) ([]gen.ResolutionKey, error)
	GetStatedKey(ctx context.Context, arg gen.GetStatedKeyParams) (gen.StatedKey, error)
	ListStmtTxns(ctx context.Context, arg gen.ListStmtTxnsParams) ([]gen.ListStmtTxnsRow, error)
}

var (
	_ Ingester  = (*stmt.Service)(nil)
	_ Confirmer = (*stmt.Service)(nil)
	_ Reader    = (*gen.Queries)(nil)
)

//go:generate go tool mockgen -source=stmt.go -destination=mock/stmt_mock.go -package=mock

// Server implements StmtService.
type Server struct {
	ingester  Ingester
	confirmer Confirmer
	reader    Reader
	mics      mic.MICMap
}

var _ stmtv1connect.StmtServiceHandler = (*Server)(nil)

// New returns a Server.
func New(ingester Ingester, confirmer Confirmer, reader Reader, mics mic.MICMap) *Server {
	return &Server{ingester: ingester, confirmer: confirmer, reader: reader, mics: mics}
}

// CreateStmt starts ingesting the statement and answers with its run. A
// statement the ingester cannot read is an invalid argument. A row it rejects
// is not an error; the row is read back as an item of the statement.
func (s *Server) CreateStmt(ctx context.Context, req *connect.Request[stmtv1.CreateStmtRequest]) (*connect.Response[stmtv1.CreateStmtResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	run, err := s.ingester.Create(ctx, p.User.ID, req.Msg.GetStmt())
	if errors.Is(err, stmt.ErrInvalid) {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&stmtv1.CreateStmtResponse{Run: to.ProtoRun(run)}), nil
}

// ListStmts reads the caller's statements, newest first.
func (s *Server) ListStmts(ctx context.Context, _ *connect.Request[stmtv1.ListStmtsRequest]) (*connect.Response[stmtv1.ListStmtsResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	rows, err := s.reader.ListStmts(ctx, p.User.ID)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &stmtv1.ListStmtsResponse{}
	for _, r := range rows {
		out.Stmts = append(out.Stmts, to.ProtoStmtSummary(r.Stmt, r.Run, r.Rejected))
	}
	return connect.NewResponse(out), nil
}

// GetStmt reads one of the caller's statements with its items, its keys
// and its transactions. The query carries the caller's user id, so another
// user's statement is not found rather than forbidden.
func (s *Server) GetStmt(ctx context.Context, req *connect.Request[stmtv1.GetStmtRequest]) (*connect.Response[stmtv1.GetStmtResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	id, err := uuid.Parse(req.Msg.GetRunId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := s.reader.GetStmt(ctx, gen.GetStmtParams{ID: id, UserID: p.User.ID})
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such statement"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	items, err := s.reader.ListStmtItems(ctx, gen.ListStmtItemsParams{StmtID: id, UserID: p.User.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &stmtv1.GetStmtResponse{Stmt: to.ProtoStmtSummary(row.Stmt, row.Run, row.Rejected)}
	for _, it := range items {
		item, err := stmt.ItemToProto(it)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		out.Items = append(out.Items, item)
	}
	if out.Keys, err = s.keys(ctx, id, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if out.Txns, err = s.txns(ctx, id, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(out), nil
}

// FindCandidates lists the candidates for one of the caller's keys. A key
// the caller does not own is not found, and a key with an association is a
// failed precondition.
func (s *Server) FindCandidates(ctx context.Context, req *connect.Request[stmtv1.FindCandidatesRequest]) (*connect.Response[stmtv1.FindCandidatesResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	id, err := uuid.Parse(req.Msg.GetStatedKeyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	offer, err := s.confirmer.Candidates(ctx, p.User.ID, id)
	if err != nil {
		return nil, keyError(err)
	}
	out := &stmtv1.FindCandidatesResponse{Reasons: offer.Reasons}
	for _, c := range offer.Candidates {
		out.Candidates = append(out.Candidates, c.ToProto())
	}
	return connect.NewResponse(out), nil
}

// ConfirmCandidate takes the named candidate as the instrument of one of
// the caller's keys. A key the caller does not own is not found; a key with
// an association, or answers that no longer offer the candidate, is a failed
// precondition.
func (s *Server) ConfirmCandidate(ctx context.Context, req *connect.Request[stmtv1.ConfirmCandidateRequest]) (*connect.Response[stmtv1.ConfirmCandidateResponse], error) {
	p, err := auth.User(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeUnauthenticated, err)
	}
	m := req.Msg
	id, err := uuid.Parse(m.GetStatedKeyId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	typ, ok := types.FromProto[types.IdentifierType](m.GetIdentifier().GetType())
	if !ok || typ == "" {
		return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("identifier type %v", m.GetIdentifier().GetType()))
	}
	pick := resolve.Pick{Datasource: m.GetDatasource(), Identifier: types.Identifier{Type: typ, Domain: m.GetIdentifier().GetDomain(), Value: m.GetIdentifier().GetValue()}}
	rk, err := s.confirmer.Confirm(ctx, p.User.ID, id, pick)
	if err != nil {
		return nil, keyError(err)
	}
	key, err := s.reader.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: id, UserID: p.User.ID})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	dated, err := tickers.OfKeys(ctx, s.reader, []gen.StatedKey{key})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&stmtv1.ConfirmCandidateResponse{Key: to.ProtoResolutionItem(key, &rk, tickers.DatedAt(key, dated))}), nil
}

// keyError translates an error of the confirmer into its code.
func keyError(err error) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("no such key"))
	case errors.Is(err, stmt.ErrAssociated), errors.Is(err, resolve.ErrNoCandidate):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeInternal, err)
}

// keys answers the statement's keys, each with the outcome of its latest
// resolution where one has recorded it.
func (s *Server) keys(ctx context.Context, id, user uuid.UUID) ([]*typev1.ResolutionItem, error) {
	keys, err := s.reader.ListStatedKeys(ctx, gen.ListStatedKeysParams{StmtID: id, UserID: user})
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
	dated, err := tickers.OfKeys(ctx, s.reader, keys)
	if err != nil {
		return nil, err
	}
	out := make([]*typev1.ResolutionItem, 0, len(keys))
	for _, k := range keys {
		var latest *gen.ResolutionKey
		if r, ok := outcome[k.ID]; ok {
			latest = &r
		}
		out = append(out, to.ProtoResolutionItem(k, latest, tickers.DatedAt(k, dated)))
	}
	return out, nil
}

// txns answers the statement's transactions, each with its mark.
func (s *Server) txns(ctx context.Context, id, user uuid.UUID) ([]*stmtv1.Txn, error) {
	rows, err := s.reader.ListStmtTxns(ctx, gen.ListStmtTxnsParams{StmtID: id, UserID: user})
	if err != nil {
		return nil, err
	}
	out := make([]*stmtv1.Txn, 0, len(rows))
	for _, r := range rows {
		out = append(out, to.ProtoTxn(r.Txn, mark(r.Validity)))
	}
	return out, nil
}
