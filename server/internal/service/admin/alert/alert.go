// Package alert serves stonks.admin.alert.v1.
//
// An alert is never stored. Each kind is a query over the rows of the surface
// that owns its cause, or over the coverage derivation. An alert therefore
// cannot drift from the rows it describes, and it needs no lifecycle of its
// own. It disappears when an administrator settles its cause.
//
// The kinds keep a fixed rank, because a score across kinds would be a guess
// until the page has been used on real data.
//
// One kind's full list pages by a keyset cursor, so a page survives rows that
// appear or vanish between reads. A table kind reads one page from its table.
// The two ticker kinds are derived in Go from the stored ticker validity, so
// each read derives both kinds in full and cuts the page in memory.
package alert

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
	"time"

	"connectrpc.com/connect"
	"github.com/google/uuid"

	adminalertv1 "github.com/leedenison/stonks/proto/admin/alert/v1"
	"github.com/leedenison/stonks/proto/admin/alert/v1/adminalertv1connect"
	"github.com/leedenison/stonks/server/internal/corpact"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/coverage"
	"github.com/leedenison/stonks/server/internal/service/admin/page"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListFailedRuns(ctx context.Context, arg gen.ListFailedRunsParams) ([]gen.ListFailedRunsRow, error)
	CountFailedRuns(ctx context.Context) (int32, error)
	ListBlocks(ctx context.Context, arg gen.ListBlocksParams) ([]gen.ListBlocksRow, error)
	CountOpenBlocks(ctx context.Context) (int32, error)
	ListUnknownCorpActs(ctx context.Context, arg gen.ListUnknownCorpActsParams) ([]gen.ListUnknownCorpActsRow, error)
	CountUnknownCorpActs(ctx context.Context) (int32, error)
	ListInstrMarkets(ctx context.Context, instrs []uuid.UUID) ([]gen.ListInstrMarketsRow, error)
	ListOpenFindings(ctx context.Context, arg gen.ListOpenFindingsParams) ([]gen.ListOpenFindingsRow, error)
	CountOpenFindings(ctx context.Context) ([]gen.CountOpenFindingsRow, error)
}

var _ Queries = (*gen.Queries)(nil)

// Reader is this package's view of the coverage package.
type Reader interface {
	Unconfirmed(ctx context.Context, today time.Time) ([]coverage.NamesNothing, []coverage.TickerGap, error)
}

var _ Reader = (*coverage.Reader)(nil)

//go:generate go tool mockgen -source=alert.go -destination=mock/alert_mock.go -package=mock

// Server implements AlertService.
type Server struct {
	queries Queries
	reader  Reader
	clock   func() time.Time
}

var _ adminalertv1connect.AlertServiceHandler = (*Server)(nil)

// New returns a Server.
func New(queries Queries, reader Reader, clock func() time.Time) *Server {
	return &Server{queries: queries, reader: reader, clock: clock}
}

// rank is the fixed order of the kinds, the first ranking highest.
var rank = []adminalertv1.AlertKind{
	adminalertv1.AlertKind_ALERT_KIND_FAILED_RUN,
	adminalertv1.AlertKind_ALERT_KIND_BLOCK,
	adminalertv1.AlertKind_ALERT_KIND_TICKER_NAMES_NOTHING,
	adminalertv1.AlertKind_ALERT_KIND_TICKER_VALIDITY_GAP,
	adminalertv1.AlertKind_ALERT_KIND_CORP_ACT,
	adminalertv1.AlertKind_ALERT_KIND_FINDING,
}

// top is how many alerts of a kind the digest holds.
const top = 5

// findingKinds are the finding kinds that are alerts of kind FINDING. Each
// records a choice the system made between sources, or a mismatch it found,
// for an administrator to check. Dropped and merged findings are routine on
// most runs and stay on the run page. Block and unhandled corporate action
// findings are not FINDING alerts: their block and their action are alerts
// of kinds BLOCK and CORP_ACT. A finding alert is open while its finding is
// uncleared, and clearing the finding is the acknowledgement.
var findingKinds = []string{
	string(gen.FindingKindContradiction), string(gen.FindingKindIdentifierDisagreement),
	string(gen.FindingKindBrokenTickerValidityBracket), string(gen.FindingKindMissedEvent),
	string(gen.FindingKindUnmatchedSplit),
}

// GetAlertDigest reads the count of each kind and its top five.
func (s *Server) GetAlertDigest(ctx context.Context, _ *connect.Request[adminalertv1.GetAlertDigestRequest]) (*connect.Response[adminalertv1.GetAlertDigestResponse], error) {
	nothing, gapped, err := s.reader.Unconfirmed(ctx, s.clock())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	out := &adminalertv1.GetAlertDigestResponse{}
	for _, k := range rank {
		g := &adminalertv1.AlertGroup{Kind: k}
		if derived(k) {
			rows := coverageRows(k, nothing, gapped)
			g.Count = int32(len(rows))
			for _, r := range rows[:min(top, len(rows))] {
				g.Top = append(g.Top, r.alert)
			}
		} else {
			if g.Count, err = s.count(ctx, k); err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
			if g.Top, err = s.list(ctx, k, cursor{}, top); err != nil {
				return nil, connect.NewError(connect.CodeInternal, err)
			}
		}
		out.Groups = append(out.Groups, g)
	}
	return connect.NewResponse(out), nil
}

// ListAlerts lists one kind's alerts in rank order. The page token is the
// cursor of the last row served.
func (s *Server) ListAlerts(ctx context.Context, req *connect.Request[adminalertv1.ListAlertsRequest]) (*connect.Response[adminalertv1.ListAlertsResponse], error) {
	m := req.Msg
	k := m.GetKind()
	p := page.New(m.GetPageSize())
	out := &adminalertv1.ListAlertsResponse{}
	if derived(k) {
		after, err := parseRow(m.GetPageToken())
		if err != nil {
			return nil, connect.NewError(connect.CodeInvalidArgument, err)
		}
		nothing, gapped, err := s.reader.Unconfirmed(ctx, s.clock())
		if err != nil {
			return nil, connect.NewError(connect.CodeInternal, err)
		}
		rows := coverageRows(k, nothing, gapped)
		if after != nil {
			rows = slices.DeleteFunc(rows, func(r row) bool { return compareRows(r, *after) <= 0 })
		}
		rows, more := page.Cut(p, rows)
		for _, r := range rows {
			out.Alerts = append(out.Alerts, r.alert)
		}
		if more {
			out.NextPageToken = rows[len(rows)-1].token()
		}
		return connect.NewResponse(out), nil
	}
	before, err := parseCursor(k, m.GetPageToken())
	if err != nil {
		return nil, connect.NewError(connect.CodeInvalidArgument, err)
	}
	alerts, err := s.list(ctx, k, before, p.Lim())
	if err != nil {
		return nil, connect.NewError(connect.CodeInternal, err)
	}
	var more bool
	out.Alerts, more = page.Cut(p, alerts)
	if more {
		out.NextPageToken = token(out.Alerts[len(out.Alerts)-1])
	}
	return connect.NewResponse(out), nil
}

// derived reports whether the kind is read from the coverage derivation. The
// other kinds, the table kinds, are read from a table each.
func derived(k adminalertv1.AlertKind) bool {
	return k == adminalertv1.AlertKind_ALERT_KIND_TICKER_NAMES_NOTHING || k == adminalertv1.AlertKind_ALERT_KIND_TICKER_VALIDITY_GAP
}

// count reads the count of a table kind.
func (s *Server) count(ctx context.Context, k adminalertv1.AlertKind) (int32, error) {
	switch k {
	case adminalertv1.AlertKind_ALERT_KIND_FAILED_RUN:
		return s.queries.CountFailedRuns(ctx)
	case adminalertv1.AlertKind_ALERT_KIND_BLOCK:
		return s.queries.CountOpenBlocks(ctx)
	case adminalertv1.AlertKind_ALERT_KIND_CORP_ACT:
		return s.queries.CountUnknownCorpActs(ctx)
	case adminalertv1.AlertKind_ALERT_KIND_FINDING:
		counted, err := s.queries.CountOpenFindings(ctx)
		if err != nil {
			return 0, err
		}
		var n int32
		for _, c := range counted {
			if slices.Contains(findingKinds, string(c.Kind)) {
				n += c.Count
			}
		}
		return n, nil
	}
	return 0, fmt.Errorf("kind %v is not read from a table", k)
}

// cursor is the position of a table kind's row within the kind: the row's
// id, and for a corporate action its ex-date too. The zero value is the
// start.
type cursor struct {
	id  *uuid.UUID
	day *time.Time
}

// parseCursor reads the page token of a table kind. A corporate action's
// token is its ex-date, a colon and its id; any other token is the id. An
// empty token is the first page.
func parseCursor(k adminalertv1.AlertKind, token string) (cursor, error) {
	if token == "" {
		return cursor{}, nil
	}
	var c cursor
	if k == adminalertv1.AlertKind_ALERT_KIND_CORP_ACT {
		date, rest, ok := strings.Cut(token, ":")
		if !ok {
			return cursor{}, fmt.Errorf("page token %q: no id", token)
		}
		d, err := time.Parse(time.DateOnly, date)
		if err != nil {
			return cursor{}, fmt.Errorf("page token %q: %w", token, err)
		}
		c.day, token = &d, rest
	}
	id, err := page.ID(token)
	if err != nil {
		return cursor{}, err
	}
	c.id = id
	return c, nil
}

// token returns the page token after the alert, as parseCursor reads it.
func token(a *adminalertv1.Alert) string {
	switch s := a.GetSubject().(type) {
	case *adminalertv1.Alert_Run:
		return s.Run.GetRun().GetId()
	case *adminalertv1.Alert_Block:
		return s.Block.GetId()
	case *adminalertv1.Alert_CorpAct:
		return s.CorpAct.GetExDate() + ":" + s.CorpAct.GetId()
	case *adminalertv1.Alert_Finding:
		return s.Finding.GetId()
	}
	return ""
}

// list reads up to lim alerts of a table kind in rank order, those before
// the cursor.
func (s *Server) list(ctx context.Context, k adminalertv1.AlertKind, before cursor, lim int32) ([]*adminalertv1.Alert, error) {
	var out []*adminalertv1.Alert
	switch k {
	case adminalertv1.AlertKind_ALERT_KIND_FAILED_RUN:
		rows, err := s.queries.ListFailedRuns(ctx, gen.ListFailedRunsParams{Before: before.id, Lim: lim})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			run := to.ProtoUserRun(r.Run, r.RunDetail)
			out = append(out, &adminalertv1.Alert{Kind: k, Subject: &adminalertv1.Alert_Run{Run: run}})
		}
	case adminalertv1.AlertKind_ALERT_KIND_BLOCK:
		rows, err := s.queries.ListBlocks(ctx, gen.ListBlocksParams{IncludeCleared: false, Before: before.id, Lim: lim})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			out = append(out, &adminalertv1.Alert{Kind: k, Subject: &adminalertv1.Alert_Block{Block: to.ProtoBlock(r)}})
		}
	case adminalertv1.AlertKind_ALERT_KIND_CORP_ACT:
		acts, err := corpact.ListUnknownActs(ctx, s.queries, before.day, before.id, lim)
		if err != nil {
			return nil, err
		}
		for _, a := range acts {
			out = append(out, &adminalertv1.Alert{Kind: k, Subject: &adminalertv1.Alert_CorpAct{CorpAct: a.ToProto()}})
		}
	case adminalertv1.AlertKind_ALERT_KIND_FINDING:
		rows, err := s.queries.ListOpenFindings(ctx, gen.ListOpenFindingsParams{Kinds: findingKinds, Before: before.id, Lim: lim})
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			key := to.StatedKeyOf(r.KeyID, r.KeyIdentifiers, r.KeyAssetClass, r.KeyCurrency)
			out = append(out, &adminalertv1.Alert{Kind: k, Subject: &adminalertv1.Alert_Finding{Finding: to.ProtoFinding(r.Finding, key)}})
		}
	default:
		return nil, fmt.Errorf("kind %v is not read from a table", k)
	}
	return out, nil
}

// row is an alert of a derived kind with the tuple that orders it within
// the kind: its transactions, its date and its ticker's name.
type row struct {
	txns  int
	date  time.Time
	name  string
	alert *adminalertv1.Alert
}

// compareRows orders the most transacted first, then the latest date, then
// the name.
func compareRows(a, b row) int {
	if a.txns != b.txns {
		return b.txns - a.txns
	}
	if c := b.date.Compare(a.date); c != 0 {
		return c
	}
	return strings.Compare(a.name, b.name)
}

// coverageRows returns the alerts of a derived kind in rank order.
func coverageRows(k adminalertv1.AlertKind, nothing []coverage.NamesNothing, gapped []coverage.TickerGap) []row {
	var out []row
	if k == adminalertv1.AlertKind_ALERT_KIND_TICKER_NAMES_NOTHING {
		for _, n := range nothing {
			a := &adminalertv1.Alert{Kind: k, Subject: &adminalertv1.Alert_NamesNothing{NamesNothing: n.ToProto()}}
			out = append(out, row{txns: n.Txns, date: n.First, name: n.Ticker.Name(), alert: a})
		}
	} else {
		for _, g := range gapped {
			a := &adminalertv1.Alert{Kind: k, Subject: &adminalertv1.Alert_ValidityGap{ValidityGap: g.ToProto()}}
			out = append(out, row{txns: g.Gap.Txns, date: g.Gap.Period.From, name: g.Ticker.Name(), alert: a})
		}
	}
	slices.SortFunc(out, compareRows)
	return out
}

// token writes the row's tuple as a page token, colon-separated. The name
// comes last, so it may contain colons.
func (r row) token() string {
	return strconv.Itoa(r.txns) + ":" + r.date.Format(time.DateOnly) + ":" + r.name
}

// parseRow reads the page token of a derived kind into the tuple it
// carries; an empty token is the first page.
func parseRow(token string) (*row, error) {
	if token == "" {
		return nil, nil
	}
	txns, rest, ok := strings.Cut(token, ":")
	if !ok {
		return nil, fmt.Errorf("page token %q: no date", token)
	}
	date, name, ok := strings.Cut(rest, ":")
	if !ok {
		return nil, fmt.Errorf("page token %q: no name", token)
	}
	n, err := strconv.Atoi(txns)
	if err != nil {
		return nil, fmt.Errorf("page token %q: %w", token, err)
	}
	d, err := time.Parse(time.DateOnly, date)
	if err != nil {
		return nil, fmt.Errorf("page token %q: %w", token, err)
	}
	return &row{txns: n, date: d, name: name}, nil
}
