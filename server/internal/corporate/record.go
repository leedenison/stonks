package corporate

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/ptr"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// record writes the answer of k, in one transaction, and an unhandled event
// finding for each unhandled event it adds. Where the system cannot spell the
// ticker, it leaves the answer out with a warning.
func (r *Recorder) record(ctx context.Context, k answerKey) error {
	t, ok := r.marketTicker(k.Request)
	if !ok {
		r.log.Warn("corporate events left out", "ticker", k.Request.Ticker.Name(), "datasource", k.Source)
		return nil
	}
	var found int
	err := r.store.Tx(ctx, func(q Queries) error {
		var err error
		found, err = writeAnswer(ctx, q, k, t)
		return err
	})
	if err != nil {
		return fmt.Errorf("record the corporate events of %s from %s: %w", t.Name(), k.Source, err)
	}
	for range found {
		runs.Found(ctx, gen.FindingKindUnhandledCorporateEvent)
	}
	return nil
}

// writeAnswer replaces the stored answer of k's datasource for t with k's,
// unless the stored one is later. An event the new answer repeats keeps its
// row, and with it the findings that name it, so a cleared finding stays
// cleared. It returns how many findings it wrote.
func writeAnswer(ctx context.Context, q Queries, k answerKey, t types.MarketTicker) (int, error) {
	if err := q.LockCorporateAnswer(ctx, fmt.Sprintf("corporate:%s:%s:%s", k.Source, t.Market, t.Value)); err != nil {
		return 0, fmt.Errorf("lock: %w", err)
	}
	from, before := covered(k.Response.Coverage, k.AnsweredAt)
	stored, err := q.GetCorporateAnswer(ctx, gen.GetCorporateAnswerParams{Datasource: k.Source, Market: t.Market, Value: t.Value})
	var old []gen.CorporateEvent
	switch {
	case errors.Is(err, db.ErrNotFound):
		stored.ID = db.NewID()
		if err := q.CreateCorporateAnswer(ctx, gen.CreateCorporateAnswerParams{
			ID: stored.ID, FetchKeyID: k.ID, Datasource: k.Source, Market: t.Market, Value: t.Value, CoveredFrom: from, CoveredBefore: before,
		}); err != nil {
			return 0, fmt.Errorf("create answer: %w", err)
		}
	case err != nil:
		return 0, fmt.Errorf("get answer: %w", err)
	case stored.AnsweredAt.After(k.AnsweredAt):
		return 0, nil
	default:
		if err := q.UpdateCorporateAnswer(ctx, gen.UpdateCorporateAnswerParams{ID: stored.ID, FetchKeyID: k.ID, CoveredFrom: from, CoveredBefore: before}); err != nil {
			return 0, fmt.Errorf("update answer: %w", err)
		}
		if old, err = q.ListAnswerEvents(ctx, stored.ID); err != nil {
			return 0, fmt.Errorf("list answer events: %w", err)
		}
	}
	gone, added := merge(old, k.Response.Events)
	if len(gone) > 0 {
		if err := q.DeleteCorporateEvents(ctx, gone); err != nil {
			return 0, fmt.Errorf("delete events: %w", err)
		}
	}
	found := 0
	for _, e := range added {
		arg := gen.CreateCorporateEventParams{
			ID: db.NewID(), AnswerID: stored.ID, ExDate: types.Day(e.ExDate),
			RatioFrom: e.Ratio.From, RatioTo: e.Ratio.To, Handled: e.Handled,
		}
		if e.Type != "" {
			arg.Type = &e.Type
		}
		if err := q.CreateCorporateEvent(ctx, arg); err != nil {
			return 0, fmt.Errorf("create event: %w", err)
		}
		if e.Handled {
			continue
		}
		detail := fmt.Sprintf("%s on %s: %s reported %s, %s, which the system does not handle",
			t.Name(), arg.ExDate.Format(time.DateOnly), k.Source, typeOf(e.Type), ratioText(e.Ratio))
		if err := q.CreateFinding(ctx, gen.CreateFindingParams{
			ID: db.NewID(), RunID: k.Run.ID, Kind: gen.FindingKindUnhandledCorporateEvent, CorporateEventID: &arg.ID, Detail: &detail,
		}); err != nil {
			return 0, fmt.Errorf("create finding: %w", err)
		}
		found++
	}
	return found, nil
}

// covered returns the bounds of c, cut to end on the day of at at the latest.
// Both are nil when c is nil or nothing of it remains after the cut.
func covered(c *types.Period, at time.Time) (*time.Time, *time.Time) {
	if c == nil {
		return nil, nil
	}
	from, before := types.Day(c.From), types.Day(c.Before)
	if end := types.Day(at).AddDate(0, 0, 1); end.Before(before) {
		before = end
	}
	if !from.Before(before) {
		return nil, nil
	}
	return &from, &before
}

// merge pairs each stored event with a reported event equal in ex-date,
// ratio, type and handling, each at most once. It returns the stored events
// left unpaired, which the answer no longer reports, and the reported events
// left unpaired, which are new.
func merge(stored []gen.CorporateEvent, reported []market.CorporateEvent) ([]uuid.UUID, []market.CorporateEvent) {
	paired := make([]bool, len(reported))
	var gone []uuid.UUID
	for _, s := range stored {
		i := firstUnpaired(reported, paired, func(e market.CorporateEvent) bool {
			return types.Day(e.ExDate).Equal(s.ExDate) && e.Ratio.Equal(types.Ratio{From: s.RatioFrom, To: s.RatioTo}) &&
				e.Type == ptr.Deref(s.Type, "") && e.Handled == s.Handled
		})
		if i < 0 {
			gone = append(gone, s.ID)
			continue
		}
		paired[i] = true
	}
	var added []market.CorporateEvent
	for i, e := range reported {
		if !paired[i] {
			added = append(added, e)
		}
	}
	return gone, added
}

// firstUnpaired returns the index of the first unpaired element of es that
// satisfies match, or -1 when none does.
func firstUnpaired(es []market.CorporateEvent, paired []bool, match func(market.CorporateEvent) bool) int {
	for i, e := range es {
		if !paired[i] && match(e) {
			return i
		}
	}
	return -1
}

// typeOf names a datasource's type of event in a finding's detail.
func typeOf(t string) string {
	if t == "" {
		return "an event of no stated type"
	}
	return t
}

// ratioText states r as a finding's detail does, as "10 for 1".
func ratioText(r types.Ratio) string {
	return r.To.String() + " for " + r.From.String()
}
