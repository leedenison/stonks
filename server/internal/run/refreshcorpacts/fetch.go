package refreshcorpacts

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/types"
)

// Scope selects instruments for FetchHeld, which keeps only those that are
// held.
type Scope struct {
	// Written selects each instrument that a ticker of the new identifier
	// facts names on any day. Attribution may move to a ticker that has never
	// been fetched.
	Written []resolve.WrittenIdentifierFacts
	// User, when set, selects every instrument that the user holds.
	User *uuid.UUID
}

// FetchHeld fetches the corporate actions of the tickers of the held
// instruments that scope selects, beneath parent. A failure is logged, and
// parent's outcome stands.
func (s *Service) FetchHeld(ctx context.Context, parent gen.Run, scope Scope) {
	kept, err := s.selected(ctx, scope)
	if err != nil {
		s.log.Warn("corporate actions fetch failed", "parent", parent.ID, "err", err)
		return
	}
	asks, err := s.requests(ctx, kept)
	if err != nil {
		s.log.Warn("corporate actions fetch failed", "parent", parent.ID, "err", err)
		return
	}
	s.beneath(ctx, parent, asks)
}

// selected returns the instruments that scope selects. An instrument may
// appear more than once.
func (s *Service) selected(ctx context.Context, scope Scope) ([]uuid.UUID, error) {
	var out []uuid.UUID
	if scope.User != nil {
		ids, err := s.queries.ListHeldInstrIDs(ctx, scope.User)
		if err != nil {
			return nil, fmt.Errorf("list the user's held instruments: %w", err)
		}
		out = ids
	}
	var ts []types.MarketTicker
	for _, w := range scope.Written {
		ts = append(ts, w.Tickers...)
	}
	if len(ts) == 0 {
		return out, nil
	}
	read, err := tickervalidity.Load(ctx, s.queries, ts)
	if err != nil {
		return nil, err
	}
	for _, ivs := range read {
		for _, iv := range ivs {
			out = append(out, iv.Instr)
		}
	}
	return out, nil
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
