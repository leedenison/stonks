// Package statement serves stonks.statement.v1.
package statement

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/proto/statement/v1/statementv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/identity/validity"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/statement"
	"github.com/leedenison/stonks/server/internal/types"
)

// Ingester starts the ingestion of a statement.
type Ingester interface {
	Create(ctx context.Context, userID uuid.UUID, msg *statementv1.Statement) (gen.Run, error)
}

// Confirmer lists and confirms the candidates for one of a user's keys.
type Confirmer interface {
	Candidates(ctx context.Context, user, key uuid.UUID) (resolve.Offer, error)
	Confirm(ctx context.Context, user, key uuid.UUID, p resolve.Pick) (gen.ResolutionKey, error)
}

// Reader is this package's view of the statement queries. It embeds the
// validity queries, which mark reads for dated keys.
type Reader interface {
	validity.FactQueries
	ListStatements(ctx context.Context, userID uuid.UUID) ([]gen.ListStatementsRow, error)
	GetStatement(ctx context.Context, arg gen.GetStatementParams) (gen.GetStatementRow, error)
	ListStatementItems(ctx context.Context, arg gen.ListStatementItemsParams) ([]gen.StatementItem, error)
	ListStatedKeys(ctx context.Context, arg gen.ListStatedKeysParams) ([]gen.StatedKey, error)
	ListLatestResolutions(ctx context.Context, arg gen.ListLatestResolutionsParams) ([]gen.ResolutionKey, error)
	GetStatedKey(ctx context.Context, arg gen.GetStatedKeyParams) (gen.StatedKey, error)
	ListStatementTransactions(ctx context.Context, arg gen.ListStatementTransactionsParams) ([]gen.ListStatementTransactionsRow, error)
}

var (
	_ Ingester  = (*statement.Service)(nil)
	_ Confirmer = (*statement.Service)(nil)
	_ Reader    = (*gen.Queries)(nil)
)

//go:generate go tool mockgen -source=statement.go -destination=mock/statement_mock.go -package=mock

// Server implements StatementService.
type Server struct {
	ingester  Ingester
	confirmer Confirmer
	reader    Reader
	mics      mic.MICMap
}

var _ statementv1connect.StatementServiceHandler = (*Server)(nil)

// New returns a Server.
func New(ingester Ingester, confirmer Confirmer, reader Reader, mics mic.MICMap) *Server {
	return &Server{ingester: ingester, confirmer: confirmer, reader: reader, mics: mics}
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
		out.Statements = append(out.Statements, to.ProtoStatementSummary(r.Statement, r.Run, r.Rejected))
	}
	return connect.NewResponse(out), nil
}

// GetStatement reads one of the caller's statements with its items, its keys
// and its transactions. The query carries the caller's user id, so another
// user's statement is not found rather than forbidden.
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
	out := &statementv1.GetStatementResponse{Statement: to.ProtoStatementSummary(row.Statement, row.Run, row.Rejected)}
	for _, it := range items {
		item, err := statement.ItemToProto(it)
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		out.Items = append(out.Items, item)
	}
	if out.Keys, err = s.keys(ctx, id, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if out.Transactions, err = s.transactions(ctx, id, p.User.ID); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(out), nil
}

// FindCandidates lists the candidates for one of the caller's keys. A key
// the caller does not own is not found, and a key with an association is a
// failed precondition.
func (s *Server) FindCandidates(ctx context.Context, req *connect.Request[statementv1.FindCandidatesRequest]) (*connect.Response[statementv1.FindCandidatesResponse], error) {
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
	out := &statementv1.FindCandidatesResponse{Reasons: offer.Reasons}
	for _, c := range offer.Candidates {
		out.Candidates = append(out.Candidates, c.ToProto())
	}
	return connect.NewResponse(out), nil
}

// ConfirmCandidate takes the named candidate as the instrument of one of
// the caller's keys. A key the caller does not own is not found; a key with
// an association, or answers that no longer offer the candidate, is a failed
// precondition.
func (s *Server) ConfirmCandidate(ctx context.Context, req *connect.Request[statementv1.ConfirmCandidateRequest]) (*connect.Response[statementv1.ConfirmCandidateResponse], error) {
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
	return connect.NewResponse(&statementv1.ConfirmCandidateResponse{Key: to.ProtoResolutionItem(key, &rk)}), nil
}

// keyError translates an error of the confirmer into its code.
func keyError(err error) error {
	switch {
	case errors.Is(err, db.ErrNotFound):
		return connect.NewError(connect.CodeNotFound, errors.New("no such key"))
	case errors.Is(err, statement.ErrAssociated), errors.Is(err, resolve.ErrNoCandidate):
		return connect.NewError(connect.CodeFailedPrecondition, err)
	}
	return connect.NewError(connect.CodeInternal, err)
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
		var latest *gen.ResolutionKey
		if r, ok := outcome[k.ID]; ok {
			latest = &r
		}
		out = append(out, to.ProtoResolutionItem(k, latest))
	}
	return out, nil
}

// transactions answers the statement's transactions, each with its mark. The
// facts of every dated key's ticker are read at once.
func (s *Server) transactions(ctx context.Context, statement, user uuid.UUID) ([]*statementv1.Transaction, error) {
	rows, err := s.reader.ListStatementTransactions(ctx, gen.ListStatementTransactionsParams{StatementID: statement, UserID: user})
	if err != nil {
		return nil, err
	}
	var tickers []types.MarketTicker
	for _, r := range rows {
		if t, ok := s.datedTicker(r.StatedKey); ok {
			tickers = append(tickers, t)
		}
	}
	read, err := validity.ReadTickers(ctx, s.reader, tickers)
	if err != nil {
		return nil, fmt.Errorf("read the facts of the dated keys' tickers: %w", err)
	}
	out := make([]*statementv1.Transaction, 0, len(rows))
	for _, r := range rows {
		var t *validity.Ticker
		if v, ok := s.datedTicker(r.StatedKey); ok {
			t = read[v]
		}
		out = append(out, to.ProtoTransaction(r.Transaction, mark(r.StatedKey.Arbiter, r.FixedViaType, t, r.Transaction.AsAt)))
	}
	return out, nil
}

// datedTicker returns the market ticker of k's dated association.
func (s *Server) datedTicker(k gen.StatedKey) (types.MarketTicker, bool) {
	v, ok := to.AssociatedTicker(k)
	if !ok {
		return types.MarketTicker{}, false
	}
	return s.mics.MarketTicker(v)
}
