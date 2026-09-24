// Package admin serves stonks.admin.v1.
//
// Every RPC acts across users, and the handler chain refuses a caller
// without the admin role before any of them runs; see
// [service.go](../service.go). A listing is paged newest first on the row's
// id, which is ordered by creation time, so the page token is the id of the
// last row served.
package admin

import (
	"context"
	"errors"
	"fmt"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/leedenison/stonks/proto/admin/v1"
	"github.com/leedenison/stonks/proto/admin/v1/adminv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	runsvc "github.com/leedenison/stonks/server/internal/service/run"
	stmtsvc "github.com/leedenison/stonks/server/internal/service/statement"
)

// defaultPageSize is the page size of a request that names none.
const defaultPageSize = 50

// Reader is the view of the queries this package depends on.
type Reader interface {
	ListUserRuns(ctx context.Context, arg gen.ListUserRunsParams) ([]gen.ListUserRunsRow, error)
	GetUserRun(ctx context.Context, id uuid.UUID) (gen.GetUserRunRow, error)
	ListChildRuns(ctx context.Context, arg gen.ListChildRunsParams) ([]gen.Run, error)
	ListRunFindings(ctx context.Context, runID uuid.UUID) ([]gen.Finding, error)
	ListStatementItems(ctx context.Context, arg gen.ListStatementItemsParams) ([]gen.StatementItem, error)
	ListResolutionItems(ctx context.Context, runID uuid.UUID) ([]gen.ListResolutionItemsRow, error)
	ListFetchItems(ctx context.Context, fetchID uuid.UUID) ([]gen.ListFetchItemsRow, error)
	ListFindings(ctx context.Context, arg gen.ListFindingsParams) ([]gen.Finding, error)
	GetFinding(ctx context.Context, id uuid.UUID) (gen.Finding, error)
	ClearFinding(ctx context.Context, id uuid.UUID) error
	ListDatasourceSettings(ctx context.Context) ([]gen.ListDatasourceSettingsRow, error)
	ListBlocks(ctx context.Context, arg gen.ListBlocksParams) ([]gen.ListBlocksRow, error)
	GetDatasourceBlock(ctx context.Context, id uuid.UUID) (gen.DatasourceBlock, error)
	ClearDatasourceBlock(ctx context.Context, id uuid.UUID) error
}

var _ Reader = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=admin.go -destination=mock/admin_mock.go -package=mock

// Server implements AdminService.
type Server struct {
	reader Reader
}

var _ adminv1connect.AdminServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader) *Server {
	return &Server{reader: reader}
}

// ListRuns lists every user's runs matching the filters, newest first.
func (s *Server) ListRuns(ctx context.Context, req *connect.Request[adminv1.ListRunsRequest]) (*connect.Response[adminv1.ListRunsResponse], error) {
	m := req.Msg
	p, err := newPage(m.GetPageSize(), m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	arg := gen.ListUserRunsParams{Before: p.before, Lim: p.lim()}
	if arg.Kind, err = filter[gen.RunKind](m.Kind); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if arg.Trigger, err = filter[gen.RunTrigger](m.Trigger); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if arg.State, err = filter[gen.RunState](m.State); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if arg.UserID, err = optionalID(m.UserId); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rows, err := s.reader.ListUserRuns(ctx, arg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListRunsResponse{}
	rows, out.NextPageToken = trim(p, rows, func(r gen.ListUserRunsRow) uuid.UUID { return r.Run.ID })
	for _, r := range rows {
		out.Runs = append(out.Runs, userRun(r.Run, r.Email))
	}
	return connect.NewResponse(out), nil
}

// GetRun reads any user's run with its children, its findings and the items
// of its kind.
func (s *Server) GetRun(ctx context.Context, req *connect.Request[adminv1.GetRunRequest]) (*connect.Response[adminv1.GetRunResponse], error) {
	id, err := uuid.Parse(req.Msg.GetRunId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := s.reader.GetUserRun(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such run"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.GetRunResponse{Run: userRun(row.Run, row.Email)}
	if err := s.fill(ctx, row.Run, out); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(out), nil
}

func (s *Server) fill(ctx context.Context, run gen.Run, out *adminv1.GetRunResponse) error {
	children, err := s.reader.ListChildRuns(ctx, gen.ListChildRunsParams{ParentID: &run.ID, UserID: run.UserID})
	if err != nil {
		return err
	}
	for _, c := range children {
		out.Children = append(out.Children, runsvc.ToProto(c))
	}
	findings, err := s.reader.ListRunFindings(ctx, run.ID)
	if err != nil {
		return err
	}
	for _, f := range findings {
		out.Findings = append(out.Findings, finding(f))
	}
	switch run.Kind {
	case gen.RunKindStatement:
		items, err := s.reader.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: run.ID, UserID: run.UserID})
		if err != nil {
			return err
		}
		for _, it := range items {
			item, err := stmtsvc.ItemToProto(it)
			if err != nil {
				return err
			}
			out.StatementItems = append(out.StatementItems, item)
		}
	case gen.RunKindResolution:
		items, err := s.reader.ListResolutionItems(ctx, run.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			k := it.ResolutionKey
			out.ResolutionItems = append(out.ResolutionItems, &adminv1.ResolutionItem{
				StatedKey:   statedKey(it.StatedKey),
				StatedKeyId: k.StatedKeyID.String(),
				Outcome:     db.ToProto[adminv1.ResolutionOutcome](k.Outcome),
				Reason:      k.Reason,
			})
		}
	case gen.RunKindFetch:
		items, err := s.reader.ListFetchItems(ctx, run.ID)
		if err != nil {
			return err
		}
		for _, it := range items {
			k := it.FetchKey
			item := &adminv1.FetchItem{
				StatedKey:   statedKey(it.StatedKey),
				StatedKeyId: k.StatedKeyID.String(),
				Outcome:     db.ToProto[adminv1.FetchOutcome](k.Outcome),
				Attempts:    int32(k.Attempts),
				Reason:      k.Reason,
			}
			if k.SentType != nil && k.SentValue != nil {
				item.Sent = identifier(types.Identifier{Type: *k.SentType, Domain: k.SentDomain, Value: *k.SentValue})
			}
			out.FetchItems = append(out.FetchItems, item)
		}
	}
	return nil
}

// ListFindings lists findings across every run, newest first.
func (s *Server) ListFindings(ctx context.Context, req *connect.Request[adminv1.ListFindingsRequest]) (*connect.Response[adminv1.ListFindingsResponse], error) {
	m := req.Msg
	p, err := newPage(m.GetPageSize(), m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	arg := gen.ListFindingsParams{IncludeCleared: m.GetIncludeCleared(), Before: p.before, Lim: p.lim()}
	if arg.RunID, err = optionalID(m.RunId); err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rows, err := s.reader.ListFindings(ctx, arg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListFindingsResponse{}
	rows, out.NextPageToken = trim(p, rows, func(f gen.Finding) uuid.UUID { return f.ID })
	for _, f := range rows {
		out.Findings = append(out.Findings, finding(f))
	}
	return connect.NewResponse(out), nil
}

// ClearFinding clears a finding that reports no block.
func (s *Server) ClearFinding(ctx context.Context, req *connect.Request[adminv1.ClearFindingRequest]) (*connect.Response[adminv1.ClearFindingResponse], error) {
	id, err := uuid.Parse(req.Msg.GetFindingId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	f, err := s.reader.GetFinding(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such finding"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if f.BlockID != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the finding reports a block; clear the block"))
	}
	if err := s.reader.ClearFinding(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminv1.ClearFindingResponse{}), nil
}

// ListDatasources lists the registered datasources in precedence order.
func (s *Server) ListDatasources(ctx context.Context, _ *connect.Request[adminv1.ListDatasourcesRequest]) (*connect.Response[adminv1.ListDatasourcesResponse], error) {
	rows, err := s.reader.ListDatasourceSettings(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListDatasourcesResponse{}
	for _, r := range rows {
		out.Datasources = append(out.Datasources, &adminv1.Datasource{
			Name: r.Name, Enabled: r.Enabled, Precedence: r.Precedence, Endpoint: r.Endpoint,
		})
	}
	return connect.NewResponse(out), nil
}

// ListBlocks lists datasource blocks, newest first.
func (s *Server) ListBlocks(ctx context.Context, req *connect.Request[adminv1.ListBlocksRequest]) (*connect.Response[adminv1.ListBlocksResponse], error) {
	m := req.Msg
	p, err := newPage(m.GetPageSize(), m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	rows, err := s.reader.ListBlocks(ctx, gen.ListBlocksParams{IncludeCleared: m.GetIncludeCleared(), Before: p.before, Lim: p.lim()})
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListBlocksResponse{}
	rows, out.NextPageToken = trim(p, rows, func(r gen.ListBlocksRow) uuid.UUID { return r.DatasourceBlock.ID })
	for _, r := range rows {
		out.Blocks = append(out.Blocks, block(r.DatasourceBlock, r.FetchID))
	}
	return connect.NewResponse(out), nil
}

// ClearBlock clears a block and the finding reporting it.
func (s *Server) ClearBlock(ctx context.Context, req *connect.Request[adminv1.ClearBlockRequest]) (*connect.Response[adminv1.ClearBlockResponse], error) {
	id, err := uuid.Parse(req.Msg.GetBlockId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	if _, err := s.reader.GetDatasourceBlock(ctx, id); errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such block"))
	} else if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.reader.ClearDatasourceBlock(ctx, id); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminv1.ClearBlockResponse{}), nil
}

func block(b gen.DatasourceBlock, runID uuid.UUID) *adminv1.Block {
	out := &adminv1.Block{
		Id:         b.ID.String(),
		Datasource: b.Datasource,
		Kind:       db.ToProto[adminv1.FetchKind](b.Kind),
		Scope:      db.ToProto[adminv1.BlockScope](b.Scope),
		Reason:     b.Reason,
		RunId:      runID.String(),
		CreatedAt:  timestamppb.New(b.CreatedAt),
	}
	if b.SentType != nil && b.SentValue != nil {
		out.Sent = identifier(types.Identifier{Type: *b.SentType, Domain: b.SentDomain, Value: *b.SentValue})
	}
	if b.ClearedAt != nil {
		out.ClearedAt = timestamppb.New(*b.ClearedAt)
	}
	return out
}

func finding(f gen.Finding) *adminv1.Finding {
	out := &adminv1.Finding{
		Id:        f.ID.String(),
		RunId:     f.RunID.String(),
		Kind:      db.ToProto[adminv1.FindingKind](f.Kind),
		CreatedAt: timestamppb.New(f.CreatedAt),
	}
	if f.BlockID != nil {
		id := f.BlockID.String()
		out.BlockId = &id
	}
	if f.ClearedAt != nil {
		out.ClearedAt = timestamppb.New(*f.ClearedAt)
	}
	return out
}

func userRun(r gen.Run, email string) *adminv1.UserRun {
	return &adminv1.UserRun{Run: runsvc.ToProto(r), UserId: r.UserID.String(), UserEmail: email}
}

func statedKey(k gen.StatedKey) *typev1.StatedKey {
	out := &typev1.StatedKey{Currency: k.Currency, Description: k.Description}
	if k.AssetClass != nil {
		out.AssetClass = db.ToProto[typev1.AssetClass](*k.AssetClass)
	}
	for _, i := range k.Identifiers {
		out.Identifiers = append(out.Identifiers, identifier(i))
	}
	return out
}

func identifier(i types.Identifier) *typev1.Identifier {
	return &typev1.Identifier{Type: db.ToProto[typev1.IdentifierType](i.Type), Domain: i.Domain, Value: i.Value}
}

// page is one page of a listing: its size, and the id every row it holds
// precedes.
type page struct {
	size   int
	before *uuid.UUID
}

func newPage(size int32, token string) (page, error) {
	p := page{size: int(size)}
	if p.size == 0 {
		p.size = defaultPageSize
	}
	if token == "" {
		return p, nil
	}
	id, err := uuid.Parse(token)
	if err != nil {
		return page{}, err
	}
	p.before = &id
	return p, nil
}

// lim is one more row than the page holds, so a full page is known to have a
// successor.
func (p page) lim() int32 { return int32(p.size + 1) }

// trim cuts rows to the page and answers the token of the next page, empty
// when there is none.
func trim[T any](p page, rows []T, id func(T) uuid.UUID) ([]T, string) {
	if len(rows) <= p.size {
		return rows, ""
	}
	rows = rows[:p.size]
	return rows, id(rows[p.size-1]).String()
}

// filter converts an optional enum field to the database value it matches.
func filter[T db.Enum, P db.ProtoEnum](p *P) (*T, error) {
	if p == nil {
		return nil, nil
	}
	v, ok := db.FromProto[T](*p)
	if !ok {
		return nil, fmt.Errorf("undefined value %v", *p)
	}
	return &v, nil
}

// optionalID parses an optional UUID field.
func optionalID(s *string) (*uuid.UUID, error) {
	if s == nil {
		return nil, nil
	}
	id, err := uuid.Parse(*s)
	if err != nil {
		return nil, err
	}
	return &id, nil
}
