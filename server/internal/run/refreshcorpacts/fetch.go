package refreshcorpacts

import (
	"context"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// AfterStmt fetches the corporate actions of the tickers of the held
// instruments of statement's user, beneath statement. A failure is logged,
// and the statement's outcome stands.
func (s *Service) AfterStmt(ctx context.Context, stmt gen.Run) {
	if stmt.UserID == nil {
		return
	}
	user := *stmt.UserID
	hs, err := s.heldInstruments(ctx)
	if err != nil {
		s.log.Warn("corporate actions fetch after a statement failed", "run", stmt.ID, "err", err)
		return
	}
	asks, err := s.requests(ctx, hs, func(_ uuid.UUID, h *held) bool { return slices.Contains(h.users, user) })
	if err != nil {
		s.log.Warn("corporate actions fetch after a statement failed", "run", stmt.ID, "err", err)
		return
	}
	s.beneath(ctx, stmt, asks)
}

// FetchAfterIdentifierFacts fetches the corporate actions of the tickers of
// each held instrument that a ticker of written names on any day, beneath
// parent, the run that wrote the facts or started the runs that did.
// Attribution may move to a ticker that has never been fetched. A failure is
// logged, and parent's outcome stands.
func (s *Service) FetchAfterIdentifierFacts(ctx context.Context, parent gen.Run, written []resolve.WrittenIdentifierFacts) {
	var ts []types.MarketTicker
	for _, w := range written {
		for _, t := range w.Tickers {
			if !slices.Contains(ts, t) {
				ts = append(ts, t)
			}
		}
	}
	if len(ts) == 0 {
		return
	}
	read, err := tickervalidity.ReadTickers(ctx, s.queries, ts)
	if err != nil {
		s.log.Warn("corporate actions fetch after new identifier facts failed", "run", parent.ID, "err", err)
		return
	}
	named := map[uuid.UUID]bool{}
	for _, t := range read {
		for _, iv := range t.Derived.Intervals {
			named[iv.Instr] = true
		}
	}
	if len(named) == 0 {
		return
	}
	hs, err := s.heldInstruments(ctx)
	if err != nil {
		s.log.Warn("corporate actions fetch after new identifier facts failed", "run", parent.ID, "err", err)
		return
	}
	asks, err := s.requests(ctx, hs, func(id uuid.UUID, _ *held) bool { return named[id] })
	if err != nil {
		s.log.Warn("corporate actions fetch after new identifier facts failed", "run", parent.ID, "err", err)
		return
	}
	s.beneath(ctx, parent, asks)
}

// beneath fetches asks beneath parent. Beneath a user's run, a ticker that
// another user also holds goes beneath a system child instead, so the fetch
// checks that user's stated splits too. A failure is logged and the rest go
// on.
func (s *Service) beneath(ctx context.Context, parent gen.Run, asks []ask) {
	var own, others []market.CorpActRequest
	for _, a := range asks {
		if parent.UserID == nil || (len(a.users) == 1 && a.users[0] == *parent.UserID) {
			own = append(own, a.req)
			continue
		}
		others = append(others, a.req)
	}
	if len(own) > 0 {
		if err := s.recorder.Fetch(ctx, parent, own); err != nil {
			s.log.Warn("corporate actions fetch failed", "parent", parent.ID, "err", err)
		}
	}
	if len(others) == 0 {
		return
	}
	_, err := s.runs.ChildAs(ctx, parent, nil, gen.RunKindRefreshCorpActs, func(ctx context.Context, sys gen.Run) error {
		return s.recorder.Fetch(ctx, sys, others)
	})
	if err != nil {
		s.log.Warn("corporate actions fetch for other users failed", "parent", parent.ID, "err", err)
	}
}
