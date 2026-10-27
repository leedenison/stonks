package corpact

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

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
// match in the calendar. A fetch that acts for a user checks that user's
// splits. A system fetch checks every user's.
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
			if err := q.CreateFinding(ctx, f); err != nil {
				return fmt.Errorf("create finding: %w", err)
			}
			found++
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

// splitFindings returns a finding for each unmatched group that no finding
// names yet.
func (r *Recorder) splitFindings(ctx context.Context, q Queries, user *uuid.UUID, recorded []recordedKey) ([]gen.CreateFindingParams, error) {
	ts := make([]types.MarketTicker, len(recorded))
	for i, rec := range recorded {
		ts[i] = rec.ticker
	}
	tickers, err := tickervalidity.ReadTickers(ctx, q, ts)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the fetched tickers: %w", err)
	}
	covers, err := coverage(ctx, q, recorded, tickers)
	if err != nil {
		return nil, err
	}
	groups, err := r.coveredSplits(ctx, q, user, ts, tickers, covers)
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
	missing := unmatchedSplits(groups, cal)
	if len(missing) == 0 {
		return nil, nil
	}
	keys := make([]uuid.UUID, len(missing))
	for i, m := range missing {
		keys[i] = m.key
	}
	named, err := q.ListUnmatchedSplits(ctx, keys)
	if err != nil {
		return nil, fmt.Errorf("list unmatched splits: %w", err)
	}
	var out []gen.CreateFindingParams
	for _, m := range missing {
		if slices.ContainsFunc(named, func(n gen.ListUnmatchedSplitsRow) bool {
			return n.StatedKeyID == m.key && n.EffectiveDate.Equal(m.day)
		}) {
			continue
		}
		detail := fmt.Sprintf("a split on %s lacks a corporate action within %d days", m.day.Format(time.DateOnly), window)
		if m.ratio != nil {
			detail = fmt.Sprintf("a split of %s on %s lacks a corporate action of equal ratio within %d days",
				ratioText(*m.ratio), m.day.Format(time.DateOnly), window)
		}
		out = append(out, gen.CreateFindingParams{
			ID: db.NewID(), RunID: m.run.ID, Kind: gen.FindingKindUnmatchedSplit, StatedKeyID: &m.key, EffectiveDate: &m.day, Detail: &detail,
		})
	}
	return out, nil
}

// coverage returns the periods over which the stored answers of the recorded
// keys' datasources cover each instrument. A stored answer stands in for the
// recorded key's own answer, because it is that answer or a later one that
// replaced it.
func coverage(ctx context.Context, q Queries, recorded []recordedKey, tickers map[types.MarketTicker]*tickervalidity.Ticker) ([]cover, error) {
	ts := make([]types.MarketTicker, len(recorded))
	for i, rec := range recorded {
		ts[i] = rec.ticker
	}
	answers, err := Read(ctx, q, ts, always)
	if err != nil {
		return nil, err
	}
	var out []cover
	for _, rec := range recorded {
		t := tickers[rec.ticker]
		if t == nil {
			continue
		}
		for _, a := range answers[rec.ticker] {
			if a.Datasource != rec.key.Source {
				continue
			}
			for _, iv := range t.Derived.Intervals {
				for _, c := range a.Coverage {
					if p, ok := iv.Clip(c); ok {
						out = append(out, cover{instr: iv.Instr, period: p, run: rec.key.Run})
					}
				}
			}
		}
	}
	return out, nil
}

// coveredSplits returns the stated splits whose date falls inside one of
// covers, grouped by key and day, with options removed. A dated key counts
// only when its ticker is one of ts.
func (r *Recorder) coveredSplits(ctx context.Context, q Queries, user *uuid.UUID, ts []types.MarketTicker, tickers map[types.MarketTicker]*tickervalidity.Ticker, covers []cover) ([]splitGroup, error) {
	if len(covers) == 0 {
		return nil, nil
	}
	var instrs []uuid.UUID
	for _, c := range covers {
		if !slices.Contains(instrs, c.instr) {
			instrs = append(instrs, c.instr)
		}
	}
	values := make([]string, len(ts))
	for i, t := range ts {
		values[i] = t.Value
	}
	rows, err := q.ListStatedSplits(ctx, gen.ListStatedSplitsParams{Instrs: instrs, Values: values, UserID: user})
	if err != nil {
		return nil, fmt.Errorf("list stated splits: %w", err)
	}
	var groups []splitGroup
	for _, row := range rows {
		sp := row.StmtSplit
		instr, ok := r.instrOn(row.StatedKey, sp.EffectiveDate, tickers)
		if !ok {
			continue
		}
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
	return withoutOptions(ctx, q, groups)
}

// instrOn returns the instrument that k names on d. A dated key names
// one only through one of tickers.
func (r *Recorder) instrOn(k gen.StatedKey, d time.Time, tickers map[types.MarketTicker]*tickervalidity.Ticker) (uuid.UUID, bool) {
	if k.InstrID != nil {
		return *k.InstrID, true
	}
	via, ok := to.AssociatedTicker(k)
	if !ok {
		return uuid.UUID{}, false
	}
	mt, ok := r.mics.MarketTicker(via)
	if !ok || tickers[mt] == nil {
		return uuid.UUID{}, false
	}
	iv, ok := tickers[mt].On(d)
	return iv.Instr, ok
}

// withoutOptions returns groups with those of options removed.
func withoutOptions(ctx context.Context, q Queries, groups []splitGroup) ([]splitGroup, error) {
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
	options := map[uuid.UUID]bool{}
	for _, row := range rows {
		if row.AssetClass == types.AssetClassOption {
			options[row.ID] = true
		}
	}
	return slices.DeleteFunc(groups, func(g splitGroup) bool { return options[g.instr] }), nil
}

// calendar returns the corporate actions of each of instruments: those of
// every datasource and market, under each ticker that named the instrument
// on the action's ex-date. The check asks whether any datasource knows a
// split. A gap in one datasource raises a disagreement.
func calendar(ctx context.Context, q Queries, instrs []uuid.UUID) (map[uuid.UUID][]Act, error) {
	rows, err := q.ListTickersNaming(ctx, instrs)
	if err != nil {
		return nil, fmt.Errorf("list the tickers of the instruments: %w", err)
	}
	ts := make([]types.MarketTicker, len(rows))
	for i, row := range rows {
		ts[i] = types.MarketTicker{Market: row.Market, Value: row.Value}
	}
	tickers, err := tickervalidity.ReadTickers(ctx, q, ts)
	if err != nil {
		return nil, fmt.Errorf("read the ticker validity of the instruments' tickers: %w", err)
	}
	answers, err := Read(ctx, q, ts, always)
	if err != nil {
		return nil, err
	}
	out := map[uuid.UUID][]Act{}
	for t, as := range answers {
		for _, a := range as {
			for _, e := range a.Acts {
				iv, ok := tickers[t].On(e.ExDate)
				if ok && slices.Contains(instrs, iv.Instr) {
					out[iv.Instr] = append(out[iv.Instr], e)
				}
			}
		}
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
