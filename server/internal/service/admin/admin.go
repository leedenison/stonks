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
	"slices"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminv1 "github.com/leedenison/stonks/proto/admin/v1"
	"github.com/leedenison/stonks/proto/admin/v1/adminv1connect"
	typev1 "github.com/leedenison/stonks/proto/type/v1"
	"github.com/leedenison/stonks/server/internal/auth"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/replay"
	stmtsvc "github.com/leedenison/stonks/server/internal/service/statement"
)

// defaultPageSize is the page size of a request that names none.
const defaultPageSize = 50

// Reader is this package's view of the queries.
type Reader interface {
	ListUserRuns(ctx context.Context, arg gen.ListUserRunsParams) ([]gen.ListUserRunsRow, error)
	ListRootRuns(ctx context.Context, arg gen.ListRootRunsParams) ([]gen.ListRootRunsRow, error)
	GetUserRun(ctx context.Context, id uuid.UUID) (gen.GetUserRunRow, error)
	ListRunAncestors(ctx context.Context, id uuid.UUID) ([]gen.ListRunAncestorsRow, error)
	ListRunDescendants(ctx context.Context, parentID *uuid.UUID) ([]gen.ListRunDescendantsRow, error)
	ListRunFindings(ctx context.Context, runIds []uuid.UUID) ([]gen.ListRunFindingsRow, error)
	ListStatementItems(ctx context.Context, arg gen.ListStatementItemsParams) ([]gen.StatementItem, error)
	ListResolutionItems(ctx context.Context, runID uuid.UUID) ([]gen.ListResolutionItemsRow, error)
	ListFetchItems(ctx context.Context, fetchID uuid.UUID) ([]gen.ListFetchItemsRow, error)
	GetReplay(ctx context.Context, id uuid.UUID) (gen.GetReplayRow, error)
	GetFinding(ctx context.Context, id uuid.UUID) (gen.Finding, error)
	ClearFinding(ctx context.Context, id uuid.UUID) error
	ListDatasourceSettings(ctx context.Context) ([]gen.ListDatasourceSettingsRow, error)
	ListDatasources(ctx context.Context) ([]gen.Datasource, error)
	UpdateDatasource(ctx context.Context, arg gen.UpdateDatasourceParams) (gen.Datasource, error)
	SetDatasourcePrecedence(ctx context.Context, arg gen.SetDatasourcePrecedenceParams) error
	ListBlocks(ctx context.Context, arg gen.ListBlocksParams) ([]gen.ListBlocksRow, error)
	GetDatasourceBlock(ctx context.Context, id uuid.UUID) (gen.DatasourceBlock, error)
	ClearDatasourceBlock(ctx context.Context, id uuid.UUID) error
}

var _ Reader = (*gen.Queries)(nil)

// Sources is this package's view of the datasource registry: whether the
// build carries an integration, and the reload that makes a change to the
// table take effect.
type Sources interface {
	Carries(name string) bool
	Reload(ctx context.Context) error
}

// Replayer is this package's view of the replay service.
type Replayer interface {
	Start(ctx context.Context, admin uuid.UUID, source gen.Run, scope replay.Scope) (gen.Run, error)
}

var _ Replayer = (*replay.Service)(nil)

//go:generate go tool mockgen -source=admin.go -destination=mock/admin_mock.go -package=mock

// Server implements AdminService.
type Server struct {
	reader  Reader
	sources Sources
	replays Replayer
}

var _ adminv1connect.AdminServiceHandler = (*Server)(nil)

// New returns a Server.
func New(reader Reader, sources Sources, replays Replayer) *Server {
	return &Server{reader: reader, sources: sources, replays: replays}
}

// ListRuns lists every user's runs matching the filters, newest first,
// each under its top-level ancestor.
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
	rows, err := s.listRuns(ctx, arg)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminv1.ListRunsResponse{}
	rows, out.NextPageToken = trimTrees(p, rows)
	byID := map[uuid.UUID]*adminv1.UserRun{}
	for _, r := range rows {
		msg := userRun(r.Run, r.Email, r.OpenFindings)
		msg.Matched = r.Matched
		byID[r.Run.ID] = msg
		if r.Run.ParentID != nil {
			if parent := byID[*r.Run.ParentID]; parent != nil {
				parent.Children = append(parent.Children, msg)
				continue
			}
		}
		out.Runs = append(out.Runs, msg)
	}
	return connect.NewResponse(out), nil
}

// listRuns reads the runs arg selects. When no filter is set, it reads the
// page from its top-level runs down and spares the walk up from every run.
func (s *Server) listRuns(ctx context.Context, arg gen.ListUserRunsParams) ([]gen.ListUserRunsRow, error) {
	if arg.Kind != nil || arg.Trigger != nil || arg.State != nil || arg.UserID != nil {
		return s.reader.ListUserRuns(ctx, arg)
	}
	rows, err := s.reader.ListRootRuns(ctx, gen.ListRootRunsParams{Before: arg.Before, Lim: arg.Lim})
	if err != nil {
		return nil, err
	}
	out := make([]gen.ListUserRunsRow, len(rows))
	for i, r := range rows {
		out[i] = gen.ListUserRunsRow(r)
	}
	return out, nil
}

// trimTrees cuts rows, grouped by top-level run, to the page's top-level
// runs and answers the token of the next page, empty when there is none.
func trimTrees(p page, rows []gen.ListUserRunsRow) ([]gen.ListUserRunsRow, string) {
	roots := 0
	var last uuid.UUID
	for i, r := range rows {
		if i == 0 || r.RootID != last {
			roots++
			last = r.RootID
		}
		if roots > p.size {
			return rows[:i], rows[i-1].RootID.String()
		}
	}
	return rows, ""
}

// GetRun reads any user's run with the runs above and below it, its findings and the items
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
	out := &adminv1.GetRunResponse{Run: userRun(row.Run, row.Email, row.OpenFindings)}
	if err := s.fill(ctx, row.Run, out); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(out), nil
}

func (s *Server) fill(ctx context.Context, run gen.Run, out *adminv1.GetRunResponse) error {
	ancestors, err := s.reader.ListRunAncestors(ctx, run.ID)
	if err != nil {
		return err
	}
	for _, a := range ancestors {
		out.Ancestors = append(out.Ancestors, userRun(a.Run, a.Email, a.OpenFindings))
	}
	descendants, err := s.reader.ListRunDescendants(ctx, &run.ID)
	if err != nil {
		return err
	}
	byID := map[uuid.UUID]*adminv1.UserRun{run.ID: out.Run}
	ids := []uuid.UUID{run.ID}
	for _, d := range descendants {
		ids = append(ids, d.Run.ID)
		msg := userRun(d.Run, d.Email, d.OpenFindings)
		byID[d.Run.ID] = msg
		if parent := byID[*d.Run.ParentID]; parent != nil {
			parent.Children = append(parent.Children, msg)
		}
	}
	findings, err := s.reader.ListRunFindings(ctx, ids)
	if err != nil {
		return err
	}
	order := treeOrder(out.Run)
	slices.SortStableFunc(findings, func(a, b gen.ListRunFindingsRow) int {
		return order[a.Finding.RunID.String()] - order[b.Finding.RunID.String()]
	})
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
			out.ResolutionItems = append(out.ResolutionItems, &typev1.ResolutionItem{
				StatedKey:   to.ProtoStatedKey(it.StatedKey),
				StatedKeyId: k.StatedKeyID.String(),
				Outcome:     types.ToProto[typev1.ResolutionOutcome](k.Outcome),
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
				StatedKey:   to.ProtoStatedKey(it.StatedKey),
				StatedKeyId: k.StatedKeyID.String(),
				Outcome:     types.ToProto[adminv1.FetchOutcome](k.Outcome),
				Attempts:    int32(k.Attempts),
				Reason:      k.Reason,
			}
			if sent, ok := to.Sent(k); ok {
				item.Sent = sent.ToProto()
			}
			out.FetchItems = append(out.FetchItems, item)
		}
	case gen.RunKindReplay:
		r, err := s.reader.GetReplay(ctx, run.ID)
		if err != nil {
			return err
		}
		out.Replay = &adminv1.Replay{SourceRunId: r.Replay.SourceID.String(), StartedBy: r.StartedByEmail}
		if r.Replay.Datasource != nil {
			out.Replay.Scope = &adminv1.Replay_Datasource{Datasource: *r.Replay.Datasource}
		} else {
			out.Replay.Scope = &adminv1.Replay_Unavailable{Unavailable: true}
		}
	}
	return nil
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
		out.Datasources = append(out.Datasources, datasource(r.Name, r.Enabled, r.Precedence, r.Endpoint, r.HasCredential))
	}
	return connect.NewResponse(out), nil
}

// UpdateDatasource sets a datasource's state, endpoint and credential, and
// reloads the registry so the change takes effect. An empty endpoint is the
// provider's default.
func (s *Server) UpdateDatasource(ctx context.Context, req *connect.Request[adminv1.UpdateDatasourceRequest]) (*connect.Response[adminv1.UpdateDatasourceResponse], error) {
	m := req.Msg
	if m.GetEnabled() && !s.sources.Carries(m.GetName()) {
		return nil, connect.NewError(connect.CodeFailedPrecondition, fmt.Errorf("this build carries no integration named %s", m.GetName()))
	}
	arg := gen.UpdateDatasourceParams{Name: m.GetName(), Enabled: m.GetEnabled(), Credential: m.Credential}
	if m.GetEndpoint() != "" {
		arg.Endpoint = m.Endpoint
	}
	row, err := s.reader.UpdateDatasource(ctx, arg)
	switch {
	case errors.Is(err, db.ErrNotFound):
		return nil, connect.NewError(connect.CodeNotFound, fmt.Errorf("no datasource named %s", m.GetName()))
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.sources.Reload(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reload datasources: %w", err))
	}
	out := &adminv1.UpdateDatasourceResponse{Datasource: datasource(row.Name, row.Enabled, row.Precedence, row.Endpoint, row.Credential != nil)}
	return connect.NewResponse(out), nil
}

// ReorderDatasources gives each datasource the precedence of its position in
// the list, and reloads the registry. A list that does not name every
// datasource exactly once is refused.
func (s *Server) ReorderDatasources(ctx context.Context, req *connect.Request[adminv1.ReorderDatasourcesRequest]) (*connect.Response[adminv1.ReorderDatasourcesResponse], error) {
	names := req.Msg.GetNames()
	rows, err := s.reader.ListDatasources(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	seen := make(map[string]bool, len(names))
	for _, n := range names {
		if seen[n] {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s is named twice", n))
		}
		seen[n] = true
	}
	for _, r := range rows {
		if !seen[r.Name] {
			return nil, connect.NewError(connect.CodeInvalidArgument, fmt.Errorf("%s is not named", r.Name))
		}
	}
	if len(names) != len(rows) {
		return nil, connect.NewError(connect.CodeInvalidArgument, errors.New("a name is no datasource"))
	}
	precedences := make([]int32, len(names))
	for i := range names {
		precedences[i] = int32(i + 1)
	}
	if err := s.reader.SetDatasourcePrecedence(ctx, gen.SetDatasourcePrecedenceParams{Names: names, Precedences: precedences}); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.sources.Reload(ctx); err != nil {
		return nil, connect.NewError(connect.CodeInternal, fmt.Errorf("reload datasources: %w", err))
	}
	return connect.NewResponse(&adminv1.ReorderDatasourcesResponse{}), nil
}

func datasource(name string, enabled bool, precedence int32, endpoint *string, held bool) *adminv1.Datasource {
	return &adminv1.Datasource{Name: name, Enabled: enabled, Precedence: precedence, Endpoint: endpoint, HasCredential: held}
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

// StartReplay starts a replay over the keys of a run, as a run of that run's
// user started by the caller.
func (s *Server) StartReplay(ctx context.Context, req *connect.Request[adminv1.StartReplayRequest]) (*connect.Response[adminv1.StartReplayResponse], error) {
	p, err := auth.Admin(ctx)
	if err != nil {
		return nil, connect.NewError(connect.CodePermissionDenied, err)
	}
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
	scope := replay.Scope{Datasource: req.Msg.GetDatasource()}
	started, err := s.replays.Start(ctx, p.User.ID, row.Run, scope)
	switch {
	case errors.Is(err, replay.ErrKind), errors.Is(err, replay.ErrEmpty), errors.Is(err, replay.ErrDisabled), errors.Is(err, replay.ErrNoIdentity):
		return nil, connect.NewError(connect.CodeFailedPrecondition, err)
	case err != nil:
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	return connect.NewResponse(&adminv1.StartReplayResponse{Run: to.ProtoRun(started)}), nil
}

func block(b gen.DatasourceBlock, runID uuid.UUID) *adminv1.Block {
	out := &adminv1.Block{
		Id:         b.ID.String(),
		Datasource: b.Datasource,
		Kind:       types.ToProto[adminv1.FetchKind](b.Kind),
		Scope:      types.ToProto[adminv1.BlockScope](b.Scope),
		Reason:     b.Reason,
		RunId:      runID.String(),
		CreatedAt:  timestamppb.New(b.CreatedAt),
	}
	if sent, ok := to.BlockSent(b); ok {
		out.Sent = sent.ToProto()
	}
	if b.ClearedAt != nil {
		out.ClearedAt = timestamppb.New(*b.ClearedAt)
	}
	return out
}

// finding writes row as its message, with what the key concerned states.
func finding(row gen.ListRunFindingsRow) *adminv1.Finding {
	f := row.Finding
	out := &adminv1.Finding{
		Id:        f.ID.String(),
		RunId:     f.RunID.String(),
		Kind:      types.ToProto[adminv1.FindingKind](f.Kind),
		CreatedAt: timestamppb.New(f.CreatedAt),
	}
	if row.KeyID != nil {
		out.StatedKey = to.ProtoStatedKey(gen.StatedKey{
			Identifiers: row.KeyIdentifiers, AssetClass: row.KeyAssetClass, Currency: row.KeyCurrency,
		})
	}
	if f.BlockID != nil {
		id := f.BlockID.String()
		out.BlockId = &id
	}
	if f.StatedKeyID != nil {
		id := f.StatedKeyID.String()
		out.StatedKeyId = &id
	}
	if f.FetchKeyID != nil {
		id := f.FetchKeyID.String()
		out.FetchKeyId = &id
	}
	if f.Step != nil {
		step := types.ToProto[adminv1.DropStep](*f.Step)
		out.Step = &step
	}
	out.Detail = f.Detail
	if f.Kind == gen.FindingKindBlock {
		out.Detail = row.BlockReason
	}
	if f.ClearedAt != nil {
		out.ClearedAt = timestamppb.New(*f.ClearedAt)
	}
	return out
}

// treeOrder numbers the runs of the tree under root in tree order: each run
// before the runs below it, and those before its next sibling.
func treeOrder(root *adminv1.UserRun) map[string]int {
	order := map[string]int{}
	var walk func(r *adminv1.UserRun)
	walk = func(r *adminv1.UserRun) {
		order[r.GetRun().GetId()] = len(order)
		for _, c := range r.GetChildren() {
			walk(c)
		}
	}
	walk(root)
	return order
}

func userRun(r gen.Run, email string, open int32) *adminv1.UserRun {
	return &adminv1.UserRun{Run: to.ProtoRun(r), UserId: r.UserID.String(), UserEmail: email, OpenFindings: open, Matched: true}
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
func filter[T types.Enum, P types.ProtoEnum](p *P) (*T, error) {
	if p == nil {
		return nil, nil
	}
	v, ok := types.FromProto[T](*p)
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
