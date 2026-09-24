// Package admin serves stonks.admin.v1.
//
// Every RPC reads across users, and the handler chain refuses a caller
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
	size := int(m.GetPageSize())
	if size == 0 {
		size = defaultPageSize
	}
	arg := gen.ListUserRunsParams{Lim: int32(size + 1)}
	var err error
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
	if t := m.GetPageToken(); t != "" {
		if arg.Before, err = optionalID(&t); err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
	}
	rows, err := s.reader.ListUserRuns(ctx, arg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListRunsResponse{}
	if len(rows) > size {
		rows = rows[:size]
		out.NextPageToken = rows[size-1].Run.ID.String()
	}
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
