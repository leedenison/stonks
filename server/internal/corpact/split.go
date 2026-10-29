package corpact

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// window is how many calendar days either side of a stated split the
// matching corporate action may fall. The count is in calendar days because
// the system lacks a calendar of trading days.
const window = 7

// splitGroup holds the stated splits of one key on one day, from one or
// several statements.
type splitGroup struct {
	key   uuid.UUID
	day   time.Time
	instr uuid.UUID
	// ratios holds each distinct ratio the statements state, and is empty
	// when the statements give no ratio.
	ratios []types.Ratio
	// run is the datasource fetch run whose answer covers the instrument on
	// day.
	run gen.Run
}

// unmatchedSplit is a group the calendar lacks.
type unmatchedSplit struct {
	splitGroup
	// ratio is the stated ratio that the actions lack, and nil for a group
	// without a stated ratio.
	ratio *types.Ratio
}

// cover is a period over which a datasource fetch run's answer covers an
// instrument.
type cover struct {
	instr  uuid.UUID
	period types.Period
	run    gen.Run
}

// checkSplits writes an unmatched split finding for each group of stated
// splits that falls inside the coverage of the recorded answers and has no
// match in the calendar. Brokers state splits too loosely to serve as
// corporate actions: IBKR dates a split on the trading day before its
// ex-date, and Schwab omits the ratio. db.CreateStandingFinding skips a split
// that a finding already names. A fetch that acts for a user checks that
// user's splits. A system fetch checks every user's.
func (r *Recorder) checkSplits(ctx context.Context, parent gen.Run, recorded []recordedKey) error {
	if len(recorded) == 0 {
		return nil
	}
	var found int
	err := r.store.Tx(ctx, func(q Queries) error {
		found = 0
		fs, err := r.splitFindings(ctx, q, parent.UserID, recorded)
		if err != nil {
			return err
		}
		for _, f := range fs {
			created, err := db.CreateStandingFinding(ctx, q, f)
			if err != nil {
				return fmt.Errorf("create finding: %w", err)
			}
			if created {
				found++
			}
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("check stated splits: %w", err)
	}
	for range found {
		runs.Found(ctx, gen.FindingKindUnmatchedSplit)
	}
	return nil
}

// splitFindings returns a finding for each unmatched group.
func (r *Recorder) splitFindings(ctx context.Context, q Queries, user *uuid.UUID, recorded []recordedKey) ([]gen.CreateStandingFindingParams, error) {
	ts := make([]types.MarketTicker, len(recorded))
	for i, rec := range recorded {
		ts[i] = rec.ticker
	}
	read, err := tickervalidity.Load(ctx, q, ts)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the fetched tickers: %w", err)
	}
	covers, err := coverage(ctx, q, recorded, read)
	if err != nil {
		return nil, err
	}
	groups, err := r.coveredSplits(ctx, q, user, ts, covers)
	if err != nil || len(groups) == 0 {
		return nil, err
	}
	instrs := make([]uuid.UUID, 0, len(groups))
	for _, g := range groups {
		if !slices.Contains(instrs, g.instr) {
			instrs = append(instrs, g.instr)
		}
	}
	cal, err := calendar(ctx, q, instrs)
	if err != nil {
		return nil, err
	}
	var out []gen.CreateStandingFindingParams
	for _, m := range unmatchedSplits(groups, cal) {
		detail := fmt.Sprintf("a split on %s lacks a corporate action within %d days", m.day.Format(time.DateOnly), window)
		if m.ratio != nil {
			detail = fmt.Sprintf("a split of %s on %s lacks a corporate action of equal ratio within %d days",
				ratioText(*m.ratio), m.day.Format(time.DateOnly), window)
		}
		out = append(out, gen.CreateStandingFindingParams{
			ID: db.NewID(), RunID: m.run.ID, Kind: gen.FindingKindUnmatchedSplit, StatedKeyID: &m.key, EffectiveDate: &m.day, Detail: &detail,
		})
	}
	return out, nil
}

// coverage returns the periods over which the stored answers of the recorded
// keys cover each instrument. A stored answer stands in for the recorded
// key's own answer, because it is that answer or a later one that replaced
// it. A recorded key whose datasource is not its market's corporate
// actions source covers nothing. A recorded key covers only an instrument
// of its ticker's market, because a split is checked against the calendar
// that restates the instrument's holdings.
func coverage(ctx context.Context, q Queries, recorded []recordedKey, read map[types.MarketTicker]tickervalidity.Intervals) ([]cover, error) {
	ts := make([]types.MarketTicker, len(recorded))
	var instrs []uuid.UUID
	for i, rec := range recorded {
		ts[i] = rec.ticker
		for _, iv := range read[rec.ticker] {
			if !slices.Contains(instrs, iv.Instr) {
				instrs = append(instrs, iv.Instr)
			}
		}
	}
	markets, err := Markets(ctx, q, instrs)
	if err != nil {
		return nil, err
	}
	answers, err := Read(ctx, q, ts, always)
	if err != nil {
		return nil, err
	}
	var out []cover
	for _, rec := range recorded {
		a, ok := answers[rec.ticker]
		if !ok || a.Datasource != rec.key.Source {
			continue
		}
		for _, iv := range read[rec.ticker] {
			if markets[iv.Instr] != rec.ticker.Market {
				continue
			}
			for _, c := range a.Coverage {
				if p, ok := iv.Clip(c); ok {
					out = append(out, cover{instr: iv.Instr, period: p, run: rec.key.Run})
				}
			}
		}
	}
	return out, nil
}

// coveredSplits returns the stated splits whose date falls inside one of
// covers, grouped by key and day. It leaves out a split of an instrument
// whose class corporate actions do not restate. A dated key counts
// only when its ticker is one of ts.
func (r *Recorder) coveredSplits(ctx context.Context, q Queries, user *uuid.UUID, ts []types.MarketTicker, covers []cover) ([]splitGroup, error) {
	if len(covers) == 0 {
		return nil, nil
	}
	var instrs []uuid.UUID
	for _, c := range covers {
		if !slices.Contains(instrs, c.instr) {
			instrs = append(instrs, c.instr)
		}
	}
	found, err := tickers.Find(ctx, q, ts)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(found))
	for _, row := range found {
		ids = append(ids, row.ID)
	}
	rows, err := q.ListStatedSplits(ctx, gen.ListStatedSplitsParams{Instrs: instrs, Tickers: ids, UserID: user})
	if err != nil {
		return nil, fmt.Errorf("list stated splits: %w", err)
	}
	var groups []splitGroup
	for _, row := range rows {
		sp, instr := row.StmtSplit, row.InstrID
		i := slices.IndexFunc(covers, func(c cover) bool { return c.instr == instr && c.period.Contains(sp.EffectiveDate) })
		if i < 0 {
			continue
		}
		at := slices.IndexFunc(groups, func(g splitGroup) bool { return g.key == sp.StatedKeyID && g.day.Equal(sp.EffectiveDate) })
		if at < 0 {
			at = len(groups)
			groups = append(groups, splitGroup{key: sp.StatedKeyID, day: sp.EffectiveDate, instr: instr, run: covers[i].run})
		}
		if sp.RatioFrom != nil && sp.RatioTo != nil {
			ratio := types.Ratio{From: *sp.RatioFrom, To: *sp.RatioTo}
			if !slices.ContainsFunc(groups[at].ratios, ratio.Equal) {
				groups[at].ratios = append(groups[at].ratios, ratio)
			}
		}
	}
	return restatedOnly(ctx, q, groups)
}

// restatedOnly returns the groups of instruments whose class corporate
// actions restate.
func restatedOnly(ctx context.Context, q Queries, groups []splitGroup) ([]splitGroup, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	ids := make([]uuid.UUID, len(groups))
	for i, g := range groups {
		ids[i] = g.instr
	}
	rows, err := q.ListInstrsByID(ctx, ids)
	if err != nil {
		return nil, fmt.Errorf("list instruments: %w", err)
	}
	restated := map[uuid.UUID]bool{}
	for _, row := range rows {
		restated[row.ID] = row.AssetClass.Restated()
	}
	return slices.DeleteFunc(groups, func(g splitGroup) bool { return !restated[g.instr] }), nil
}

// calendar returns the corporate actions of each of instruments: those of
// its market, handled or not, under each ticker that named the instrument on
// the action's ex-date.
func calendar(ctx context.Context, q Queries, instrs []uuid.UUID) (map[uuid.UUID][]Act, error) {
	cals, err := Calendars(ctx, q, instrs)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID][]Act, len(cals))
	for id, c := range cals {
		out[id] = c.Acts
	}
	return out, nil
}

// unmatchedSplits returns the groups that the calendar lacks. A group
// matches when, for each ratio it states, an action of equal ratio falls
// within window days of its day. A group that states no ratio matches any
// action within the window. An unhandled action matches, because the calendar
// holds it and its own finding reports it.
func unmatchedSplits(groups []splitGroup, cal map[uuid.UUID][]Act) []unmatchedSplit {
	var out []unmatchedSplit
	for _, g := range groups {
		near := slices.DeleteFunc(slices.Clone(cal[g.instr]), func(e Act) bool { return days(e.ExDate, g.day) > window })
		if len(g.ratios) == 0 {
			if len(near) == 0 {
				out = append(out, unmatchedSplit{splitGroup: g})
			}
			continue
		}
		for _, r := range g.ratios {
			if !slices.ContainsFunc(near, func(e Act) bool { return e.Ratio.Equal(r) }) {
				out = append(out, unmatchedSplit{splitGroup: g, ratio: &r})
				break
			}
		}
	}
	return out
}

// days is how many calendar days apart a and b are.
func days(a, b time.Time) int {
	d := int(a.Sub(b).Hours() / 24)
	if d < 0 {
		return -d
	}
	return d
}
