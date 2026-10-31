package corpact

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	admincorpactv1 "github.com/leedenison/stonks/proto/admin/corpact/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/types"
)

// Restates reports whether a decision of type d restates the quantity held.
func Restates(d gen.CorpActDecision) bool {
	switch d {
	case gen.CorpActDecisionSplit, gen.CorpActDecisionReverseSplit, gen.CorpActDecisionStockDividend:
		return true
	}
	return false
}

// Restates reports whether e restates the quantity held. A decision
// overrules the system's judgment.
func (e Act) Restates() bool {
	if e.Decision != "" {
		return Restates(e.Decision)
	}
	return e.Handled
}

// Unknown reports whether the effect of e on the quantity held is unknown:
// the system did not handle it and it is undecided, or it is decided as a
// type the system cannot yet apply. The calendar leaves the ex-date of an
// unknown action out of its coverage.
func (e Act) Unknown() bool {
	return (e.Decision == "" && !e.Handled) || e.Decision == gen.CorpActDecisionOther
}

// DecisionQueries read what an administrator needs to decide an action.
type DecisionQueries interface {
	MarketQueries
	tickervalidity.ValidityQueries
	GetCorpAct(ctx context.Context, id uuid.UUID) (gen.GetCorpActRow, error)
	ListInstrCorpActs(ctx context.Context, instrID uuid.UUID) ([]gen.ListInstrCorpActsRow, error)
	ListStatedSplits(ctx context.Context, arg gen.ListStatedSplitsParams) ([]gen.ListStatedSplitsRow, error)
}

var _ DecisionQueries = (*gen.Queries)(nil)

// InstrAct is a stored action of a ticker that named an instrument on the
// action's ex-date.
type InstrAct struct {
	Act
	Datasource string
	Ticker     types.MarketTicker
	// Applied is whether the instrument's calendar reads the action: its
	// datasource is the corporate actions source of the ticker's market, and
	// that market is the instrument's.
	Applied bool
}

// ToProto converts a.
func (a InstrAct) ToProto() *admincorpactv1.CorpAct {
	out := &admincorpactv1.CorpAct{
		Id:             a.ID.String(),
		Ticker:         a.Ticker.ToProto(),
		ExDate:         a.ExDate.Format(time.DateOnly),
		Ratio:          a.Ratio.ToProto(),
		Datasource:     a.Datasource,
		RestatesShares: a.Restates(),
		Decision:       types.ToProto[admincorpactv1.CorpActDecision](a.Decision),
		Applied:        a.Applied,
	}
	if a.Type != "" {
		out.Type = &a.Type
	}
	return out
}

// ListInstrActs returns every stored action of a ticker that named instr on
// the action's ex-date, from every datasource, latest ex-date first. The
// calendar reads only the applied ones. The rest are listed because an
// administrator decides an action here whatever its datasource.
func ListInstrActs(ctx context.Context, q DecisionQueries, instr uuid.UUID) ([]InstrAct, error) {
	rows, err := q.ListInstrCorpActs(ctx, instr)
	if err != nil {
		return nil, fmt.Errorf("list the corporate actions of the instrument: %w", err)
	}
	markets, err := Markets(ctx, q, []uuid.UUID{instr})
	if err != nil {
		return nil, err
	}
	out := make([]InstrAct, 0, len(rows))
	for _, row := range rows {
		t := types.MarketTicker{Market: row.Market, Value: row.Value}
		out = append(out, InstrAct{Act: toAct(row.CorpAct), Datasource: row.Datasource, Ticker: t, Applied: row.Source && t.Market == markets[instr]})
	}
	return out, nil
}

// UnknownQueries read the actions whose effect on the quantity held is
// unknown.
type UnknownQueries interface {
	MarketQueries
	ListUnknownCorpActs(ctx context.Context, arg gen.ListUnknownCorpActsParams) ([]gen.ListUnknownCorpActsRow, error)
}

var _ UnknownQueries = (*gen.Queries)(nil)

// ListUnknownActs returns up to lim actions whose effect on the quantity
// held is unknown, latest ex-date first: those before the action of
// beforeDay and beforeID, or the latest where both are nil. Applied is
// judged against the instrument the ticker named on the ex-date, and is
// false where it named nothing.
func ListUnknownActs(ctx context.Context, q UnknownQueries, beforeDay *time.Time, beforeID *uuid.UUID, lim int32) ([]InstrAct, error) {
	rows, err := q.ListUnknownCorpActs(ctx, gen.ListUnknownCorpActsParams{BeforeDay: beforeDay, BeforeID: beforeID, Lim: lim})
	if err != nil {
		return nil, fmt.Errorf("list the unknown corporate actions: %w", err)
	}
	var instrs []uuid.UUID
	for _, row := range rows {
		if row.InstrID != nil && !slices.Contains(instrs, *row.InstrID) {
			instrs = append(instrs, *row.InstrID)
		}
	}
	markets, err := Markets(ctx, q, instrs)
	if err != nil {
		return nil, err
	}
	out := make([]InstrAct, 0, len(rows))
	for _, row := range rows {
		t := types.MarketTicker{Market: row.Market, Value: row.Value}
		applied := row.Source && row.InstrID != nil && t.Market == markets[*row.InstrID]
		out = append(out, InstrAct{Act: toAct(row.CorpAct), Datasource: row.Datasource, Ticker: t, Applied: applied})
	}
	return out, nil
}

// StmtSplit is a split that statements state, by its effective date and
// ratio. Ratio is nil where the statements state none.
type StmtSplit struct {
	EffectiveDate time.Time
	Ratio         *types.Ratio
}

// same reports whether s and o state one split.
func (s StmtSplit) same(o StmtSplit) bool {
	if !s.EffectiveDate.Equal(o.EffectiveDate) || (s.Ratio == nil) != (o.Ratio == nil) {
		return false
	}
	return s.Ratio == nil || s.Ratio.Equal(*o.Ratio)
}

// Evidence is what an administrator weighs when deciding an action.
type Evidence struct {
	Act InstrAct
	// StmtSplits are the distinct splits that statements state within window
	// days of the ex-date, for the instrument the action's ticker named on
	// that day.
	StmtSplits []StmtSplit
	// Others are the actions of other datasources for that instrument within
	// window days of the ex-date.
	Others []InstrAct
}

// ReadEvidence reads the action id and its evidence. Where the ticker named
// nothing on the ex-date, the evidence holds the action alone. A missing
// action is db.ErrNotFound.
func ReadEvidence(ctx context.Context, q DecisionQueries, id uuid.UUID) (Evidence, error) {
	row, err := q.GetCorpAct(ctx, id)
	if err != nil {
		return Evidence{}, fmt.Errorf("get the corporate action: %w", err)
	}
	t := types.MarketTicker{Market: row.Market, Value: row.Value}
	ev := Evidence{Act: InstrAct{Act: toAct(row.CorpAct), Datasource: row.Datasource, Ticker: t}}
	read, err := tickervalidity.Load(ctx, q, []types.MarketTicker{t})
	if err != nil {
		return Evidence{}, fmt.Errorf("read the ticker validity of %s: %w", t.Name(), err)
	}
	iv, ok := read[t].On(ev.Act.ExDate)
	if !ok {
		return ev, nil
	}
	acts, err := ListInstrActs(ctx, q, iv.Instr)
	if err != nil {
		return Evidence{}, err
	}
	for _, a := range acts {
		switch {
		case a.ID == id:
			ev.Act.Applied = a.Applied
		case a.Datasource != ev.Act.Datasource && days(a.ExDate, ev.Act.ExDate) <= window:
			ev.Others = append(ev.Others, a)
		}
	}
	rows, err := q.ListStatedSplits(ctx, gen.ListStatedSplitsParams{Instrs: []uuid.UUID{iv.Instr}, Tickers: []uuid.UUID{row.TickerID}})
	if err != nil {
		return Evidence{}, fmt.Errorf("list stated splits: %w", err)
	}
	for _, r := range rows {
		sp := r.StmtSplit
		if days(sp.EffectiveDate, ev.Act.ExDate) > window {
			continue
		}
		s := StmtSplit{EffectiveDate: sp.EffectiveDate}
		if sp.RatioFrom != nil && sp.RatioTo != nil {
			s.Ratio = &types.Ratio{From: *sp.RatioFrom, To: *sp.RatioTo}
		}
		if !slices.ContainsFunc(ev.StmtSplits, s.same) {
			ev.StmtSplits = append(ev.StmtSplits, s)
		}
	}
	return ev, nil
}
