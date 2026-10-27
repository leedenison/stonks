// Package run serves stonks.admin.run.v1.
package run

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strconv"

	"connectrpc.com/connect"
	"github.com/google/uuid"
	"google.golang.org/protobuf/types/known/timestamppb"

	adminblockv1 "github.com/leedenison/stonks/proto/admin/block/v1"
	adminreplayv1 "github.com/leedenison/stonks/proto/admin/replay/v1"
	adminrunv1 "github.com/leedenison/stonks/proto/admin/run/v1"
	"github.com/leedenison/stonks/proto/admin/run/v1/adminrunv1connect"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
	"github.com/leedenison/stonks/server/internal/statement"
	"github.com/leedenison/stonks/server/internal/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListUserRuns(ctx context.Context, arg gen.ListUserRunsParams) ([]gen.ListUserRunsRow, error)
	ListRootRuns(ctx context.Context, arg gen.ListRootRunsParams) ([]gen.ListRootRunsRow, error)
	GetUserRun(ctx context.Context, id uuid.UUID) (gen.GetUserRunRow, error)
	ListRunAncestors(ctx context.Context, id uuid.UUID) ([]gen.ListRunAncestorsRow, error)
	ListRunDescendants(ctx context.Context, parentID *uuid.UUID) ([]gen.ListRunDescendantsRow, error)
	ListRunFindings(ctx context.Context, runIds []uuid.UUID) ([]gen.ListRunFindingsRow, error)
	ListStatementItems(ctx context.Context, arg gen.ListStatementItemsParams) ([]gen.StatementItem, error)
	ListResolutionItems(ctx context.Context, arg gen.ListResolutionItemsParams) ([]gen.ListResolutionItemsRow, error)
	ListFetchItems(ctx context.Context, arg gen.ListFetchItemsParams) ([]gen.ListFetchItemsRow, error)
	ListRunAssertions(ctx context.Context, arg gen.ListRunAssertionsParams) ([]gen.ListRunAssertionsRow, error)
	GetReplay(ctx context.Context, id uuid.UUID) (gen.GetReplayRow, error)
	ClearFinding(ctx context.Context, id uuid.UUID) (*uuid.UUID, error)
}

var _ Queries = (*gen.Queries)(nil)

//go:generate go tool mockgen -source=run.go -destination=mock/run_mock.go -package=mock

// Server implements RunService.
type Server struct {
	queries Queries
}

var _ adminrunv1connect.RunServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries) *Server {
	return &Server{queries: queries}
}

// ListRuns lists every user's runs matching the filters, newest first,
// each under its top-level ancestor. The page token is the id of the last
// top-level run served.
func (s *Server) ListRuns(ctx context.Context, req *connect.Request[adminrunv1.ListRunsRequest]) (*connect.Response[adminrunv1.ListRunsResponse], error) {
	m := req.Msg
	before, err := page.ID(m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	p := page.New(m.GetPageSize())
	arg := gen.ListUserRunsParams{Before: before, Lim: p.Lim()}
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
	out := &adminrunv1.ListRunsResponse{}
	rows, out.NextPageToken = trimTrees(p, rows)
	byID := map[uuid.UUID]*adminrunv1.UserRun{}
	for _, r := range rows {
		msg := userRun(r.Run, r.Email, r.OpenFindings, runFetch(r.Datasource, r.Endpoint, r.FetchKind))
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
		return s.queries.ListUserRuns(ctx, arg)
	}
	rows, err := s.queries.ListRootRuns(ctx, gen.ListRootRunsParams{Before: arg.Before, Lim: arg.Lim})
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
func trimTrees(p page.Page, rows []gen.ListUserRunsRow) ([]gen.ListUserRunsRow, string) {
	roots := 0
	var last uuid.UUID
	for i, r := range rows {
		if i == 0 || r.RootID != last {
			roots++
			last = r.RootID
		}
		if roots > p.Size {
			return rows[:i], rows[i-1].RootID.String()
		}
	}
	return rows, ""
}

// GetRun reads any user's run with the runs above and below it and its
// findings.
func (s *Server) GetRun(ctx context.Context, req *connect.Request[adminrunv1.GetRunRequest]) (*connect.Response[adminrunv1.GetRunResponse], error) {
	row, err := s.userRun(ctx, req.Msg.GetRunId())
	if err != nil {
		return nil, err
	}
	out := &adminrunv1.GetRunResponse{Run: userRun(row.Run, row.Email, row.OpenFindings, runFetch(row.Datasource, row.Endpoint, row.FetchKind))}
	ids, err := s.tree(ctx, row.Run, out)
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if err := s.findings(ctx, ids, out); err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if row.Run.Kind == gen.RunKindReplayRun {
		if out.Replay, err = s.replay(ctx, row.Run.ID); err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
	}
	return connect.NewResponse(out), nil
}

// userRun reads the run id names. A failure comes back as a connect error
// that the caller returns unchanged.
func (s *Server) userRun(ctx context.Context, id string) (gen.GetUserRunRow, error) {
	runID, err := uuid.Parse(id)
	if err != nil {
		return gen.GetUserRunRow{}, connect.NewError(connect.CodeInvalidArgument, err)
	}
	row, err := s.queries.GetUserRun(ctx, runID)
	if errors.Is(err, db.ErrNotFound) {
		return gen.GetUserRunRow{}, connect.NewError(connect.CodeNotFound, errors.New("no such run"))
	}
	if err != nil {
		return gen.GetUserRunRow{}, connect.NewError(connect.CodeInternal, err)
	}
	return row, nil
}

// tree adds the runs above and below run to out, and returns the ids of run
// and of every run below it.
func (s *Server) tree(ctx context.Context, run gen.Run, out *adminrunv1.GetRunResponse) ([]uuid.UUID, error) {
	ancestors, err := s.queries.ListRunAncestors(ctx, run.ID)
	if err != nil {
		return nil, err
	}
	for _, a := range ancestors {
		out.Ancestors = append(out.Ancestors, userRun(a.Run, a.Email, a.OpenFindings, runFetch(a.Datasource, a.Endpoint, a.FetchKind)))
	}
	descendants, err := s.queries.ListRunDescendants(ctx, &run.ID)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]*adminrunv1.UserRun{run.ID: out.Run}
	ids := []uuid.UUID{run.ID}
	for _, d := range descendants {
		ids = append(ids, d.Run.ID)
		msg := userRun(d.Run, d.Email, d.OpenFindings, runFetch(d.Datasource, d.Endpoint, d.FetchKind))
		byID[d.Run.ID] = msg
		if parent := byID[*d.Run.ParentID]; parent != nil {
			parent.Children = append(parent.Children, msg)
		}
	}
	return ids, nil
}

// findings adds the findings of the runs ids names to out, in tree order.
func (s *Server) findings(ctx context.Context, ids []uuid.UUID, out *adminrunv1.GetRunResponse) error {
	rows, err := s.queries.ListRunFindings(ctx, ids)
	if err != nil {
		return err
	}
	order := treeOrder(out.Run)
	slices.SortStableFunc(rows, func(a, b gen.ListRunFindingsRow) int {
		return order[a.Finding.RunID.String()] - order[b.Finding.RunID.String()]
	})
	for _, f := range rows {
		out.Findings = append(out.Findings, finding(f))
	}
	return nil
}

// replay reads what the replay run id re-resolves. A replay whose prepare
// step failed has no replays row, and reads as nil.
func (s *Server) replay(ctx context.Context, id uuid.UUID) (*adminreplayv1.Replay, error) {
	r, err := s.queries.GetReplay(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := &adminreplayv1.Replay{SourceRunId: r.Replay.SourceID.String(), StartedBy: r.StartedByEmail}
	if r.Replay.Datasource != nil {
		out.Scope = &adminreplayv1.Replay_Datasource{Datasource: *r.Replay.Datasource}
	} else {
		out.Scope = &adminreplayv1.Replay_Unavailable{Unavailable: true}
	}
	return out, nil
}

// ListRunItems lists the items a run wrote, in the order it wrote them, a page
// at a time. The page token is the last item's ordinal for an ingest statement
// run, its assertion id for an attest identifier or reassert tickers run, and
// its stated key id for any other kind.
func (s *Server) ListRunItems(ctx context.Context, req *connect.Request[adminrunv1.ListRunItemsRequest]) (*connect.Response[adminrunv1.ListRunItemsResponse], error) {
	m := req.Msg
	row, err := s.userRun(ctx, m.GetRunId())
	if err != nil {
		return nil, err
	}
	p := page.New(m.GetPageSize())
	run, lim := row.Run, p.Lim()
	out := &adminrunv1.ListRunItemsResponse{}
	switch run.Kind {
	case gen.RunKindIngestStmt:
		if run.UserID == nil {
			return nil, connect.NewError(connect.CodeInternal, errors.New("ingest statement run has no user"))
		}
		after, err := page.Ordinal(m.GetPageToken())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		rows, err := s.queries.ListStatementItems(ctx, gen.ListStatementItemsParams{StatementID: run.ID, UserID: *run.UserID, After: after, Lim: &lim})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		rows, more := page.Cut(p, rows)
		for _, it := range rows {
			item, err := statement.ItemToProto(it)
			if err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			out.Items = append(out.Items, &adminrunv1.RunItem{Item: &adminrunv1.RunItem_Statement{Statement: item}})
		}
		if more {
			out.NextPageToken = strconv.Itoa(int(rows[len(rows)-1].Ordinal))
		}
	case gen.RunKindResolveStmtKeys:
		after, err := page.ID(m.GetPageToken())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		rows, err := s.queries.ListResolutionItems(ctx, gen.ListResolutionItemsParams{RunID: run.ID, After: after, Lim: &lim})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		rows, more := page.Cut(p, rows)
		for _, it := range rows {
			item := to.ProtoResolutionItem(it.StatedKey, &it.ResolutionKey)
			out.Items = append(out.Items, &adminrunv1.RunItem{Item: &adminrunv1.RunItem_Resolution{Resolution: item}})
		}
		if more {
			out.NextPageToken = rows[len(rows)-1].StatedKey.ID.String()
		}
	case gen.RunKindDatasourceFetch:
		after, err := page.ID(m.GetPageToken())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		rows, err := s.queries.ListFetchItems(ctx, gen.ListFetchItemsParams{FetchID: run.ID, After: after, Lim: &lim})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		rows, more := page.Cut(p, rows)
		for _, it := range rows {
			out.Items = append(out.Items, &adminrunv1.RunItem{Item: &adminrunv1.RunItem_Fetch{Fetch: to.ProtoFetchItem(it)}})
		}
		if more {
			out.NextPageToken = rows[len(rows)-1].FetchKey.ID.String()
		}
	case gen.RunKindReassertTickers, gen.RunKindAttestIdentifier:
		after, err := page.ID(m.GetPageToken())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		rows, err := s.queries.ListRunAssertions(ctx, gen.ListRunAssertionsParams{RunID: &run.ID, After: after, Lim: &lim})
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		rows, more := page.Cut(p, rows)
		for _, it := range rows {
			out.Items = append(out.Items, &adminrunv1.RunItem{Item: &adminrunv1.RunItem_Assertion{Assertion: to.ProtoAssertionItem(it.IdentifierAssertion, it.Datasource, it.AttestedBy)}})
		}
		if more {
			out.NextPageToken = rows[len(rows)-1].IdentifierAssertion.ID.String()
		}
	}
	return connect.NewResponse(out), nil
}

// ClearFinding clears a finding that reports no block.
func (s *Server) ClearFinding(ctx context.Context, req *connect.Request[adminrunv1.ClearFindingRequest]) (*connect.Response[adminrunv1.ClearFindingResponse], error) {
	id, err := uuid.Parse(req.Msg.GetFindingId())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	block, err := s.queries.ClearFinding(ctx, id)
	if errors.Is(err, db.ErrNotFound) {
		return nil, connect.NewError(connect.CodeNotFound, errors.New("no such finding"))
	}
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	if block != nil {
		return nil, connect.NewError(connect.CodeFailedPrecondition, errors.New("the finding reports a block; clear the block"))
	}
	return connect.NewResponse(&adminrunv1.ClearFindingResponse{}), nil
}

// finding writes row as its message, with what the key concerned states.
func finding(row gen.ListRunFindingsRow) *adminrunv1.Finding {
	f := row.Finding
	out := &adminrunv1.Finding{
		Id:        f.ID.String(),
		RunId:     f.RunID.String(),
		Kind:      types.ToProto[adminrunv1.FindingKind](f.Kind),
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
	if f.Step != nil {
		step := types.ToProto[adminrunv1.DropStep](*f.Step)
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
func treeOrder(root *adminrunv1.UserRun) map[string]int {
	order := map[string]int{}
	var walk func(r *adminrunv1.UserRun)
	walk = func(r *adminrunv1.UserRun) {
		order[r.GetRun().GetId()] = len(order)
		for _, c := range r.GetChildren() {
			walk(c)
		}
	}
	walk(root)
	return order
}

// userRun builds a run as the admin listings return it. A system run has
// neither a user nor an email.
func userRun(r gen.Run, email *string, open int32, fetch *adminrunv1.RunFetch) *adminrunv1.UserRun {
	out := &adminrunv1.UserRun{
		Run: to.ProtoRun(r), UserEmail: email, OpenFindings: open, Matched: true, Fetch: fetch,
	}
	if r.UserID != nil {
		id := r.UserID.String()
		out.UserId = &id
	}
	return out
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

// runFetch returns nil for a run of another kind than fetch, which has no
// fetches row.
func runFetch(datasource, endpoint *string, kind *gen.FetchKind) *adminrunv1.RunFetch {
	if datasource == nil || endpoint == nil || kind == nil {
		return nil
	}
	return &adminrunv1.RunFetch{Datasource: *datasource, Endpoint: *endpoint, Kind: types.ToProto[adminblockv1.FetchKind](*kind)}
}
