package corpact

import (
	"context"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickers"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// recordedKey pairs a served fetch key with the ticker of its answer.
type recordedKey struct {
	key    answerKey
	ticker types.MarketTicker
}

// record writes the answer of k, in one transaction, and an unhandled
// corporate action finding for each unhandled corporate action it adds. Where
// the system cannot spell the ticker, it leaves the answer out with a warning
// and reports false.
func (r *Recorder) record(ctx context.Context, k answerKey) (recordedKey, bool, error) {
	t, ok := r.marketTicker(k.Request)
	if !ok {
		r.log.Warn("corporate actions left out", "ticker", k.Request.Ticker.Name(), "datasource", k.Source)
		return recordedKey{}, false, nil
	}
	rows, err := tickers.Get(ctx, r.store, r.mics, []types.MarketTicker{t})
	if err != nil {
		return recordedKey{}, false, fmt.Errorf("create the ticker %s: %w", t.Name(), err)
	}
	var found int
	err = r.store.Tx(ctx, func(q Queries) error {
		var err error
		found, err = writeAnswer(ctx, q, k, rows[t])
		return err
	})
	if err != nil {
		return recordedKey{}, false, fmt.Errorf("record the corporate actions of %s from %s: %w", t.Name(), k.Source, err)
	}
	for range found {
		runs.Found(ctx, gen.FindingKindUnhandledCorpAct)
	}
	return recordedKey{key: k, ticker: t}, true, nil
}

// writeAnswer replaces the stored answer of k's datasource for t with k's,
// unless the stored one is later. An action the new answer repeats keeps its
// row, and with it the findings that name it, so a cleared finding stays
// cleared. It returns how many findings it wrote.
func writeAnswer(ctx context.Context, q Queries, k answerKey, t gen.Ticker) (int, error) {
	if err := q.LockCorpActAnswer(ctx, fmt.Sprintf("corpact:%s:%s:%s", k.Source, t.Market, t.Value)); err != nil {
		return 0, fmt.Errorf("lock: %w", err)
	}
	from, before := covered(k.Response.Coverage, k.AnsweredAt)
	stored, err := q.GetCorpActAnswer(ctx, gen.GetCorpActAnswerParams{Datasource: k.Source, TickerID: t.ID})
	var old []gen.CorpAct
	switch {
	case errors.Is(err, db.ErrNotFound):
		stored.ID = db.NewID()
		if err := q.CreateCorpActAnswer(ctx, gen.CreateCorpActAnswerParams{
			ID: stored.ID, FetchKeyID: k.ID, Datasource: k.Source, TickerID: t.ID, CoveredFrom: from, CoveredBefore: before,
		}); err != nil {
			return 0, fmt.Errorf("create answer: %w", err)
		}
	case err != nil:
		return 0, fmt.Errorf("get answer: %w", err)
	case stored.AnsweredAt.After(k.AnsweredAt):
		return 0, nil
	default:
		if err := q.UpdateCorpActAnswer(ctx, gen.UpdateCorpActAnswerParams{ID: stored.ID, FetchKeyID: k.ID, CoveredFrom: from, CoveredBefore: before}); err != nil {
			return 0, fmt.Errorf("update answer: %w", err)
		}
		if old, err = q.ListAnswerCorpActs(ctx, stored.ID); err != nil {
			return 0, fmt.Errorf("list answer corporate actions: %w", err)
		}
	}
	gone, added := merge(old, judged(k.Response.Acts))
	if len(gone) > 0 {
		if err := q.DeleteCorpActs(ctx, gone); err != nil {
			return 0, fmt.Errorf("delete corporate actions: %w", err)
		}
	}
	found := 0
	for _, e := range added {
		arg := gen.CreateCorpActParams{
			ID: db.NewID(), AnswerID: stored.ID, ExDate: types.Day(e.ExDate),
			RatioFrom: e.Ratio.From, RatioTo: e.Ratio.To, Handled: e.Handled,
		}
		if e.Type != "" {
			arg.Type = &e.Type
		}
		if err := q.CreateCorpAct(ctx, arg); err != nil {
			return 0, fmt.Errorf("create corporate action: %w", err)
		}
		if e.Handled {
			continue
		}
		detail := fmt.Sprintf("%s on %s: %s reported %s, %s, which the system does not handle",
			tickers.Of(t).Name(), arg.ExDate.Format(time.DateOnly), k.Source, typeOf(e.Type), ratioText(e.Ratio))
		if err := q.CreateFinding(ctx, gen.CreateFindingParams{
			ID: db.NewID(), RunID: k.Run.ID, Kind: gen.FindingKindUnhandledCorpAct, CorpActID: &arg.ID, Detail: &detail,
		}); err != nil {
			return 0, fmt.Errorf("create finding: %w", err)
		}
		found++
	}
	return found, nil
}

// maxShareTerm is the largest that the smaller side of a share count ratio
// can be in lowest terms. Providers state a spin-off as a ratio such as 1281
// for 1000, which is the fall in the parent's price.
const maxShareTerm = 100

// restatesShares reports whether r states a change in share count rather
// than a price factor. Both sides are whole, and in lowest terms the smaller
// is at most maxShareTerm. A provider's type is not enough: Massive types
// GE's spin-offs of 2023 and 2024 as forward splits of 1281 for 1000 and 1253
// for 1000, as the cassette massive_splits_spinoff records.
func restatesShares(r types.Ratio) bool {
	if !r.From.IsInteger() || !r.To.IsInteger() {
		return false
	}
	from, to := r.From.BigInt(), r.To.BigInt()
	if from.Sign() <= 0 || to.Sign() <= 0 {
		return false
	}
	gcd := new(big.Int).GCD(nil, nil, from, to)
	smaller := from
	if to.Cmp(from) < 0 {
		smaller = to
	}
	return new(big.Int).Quo(smaller, gcd).Cmp(big.NewInt(maxShareTerm)) <= 0
}

// judged returns es with Handled cleared on each action whose ratio is a price
// factor.
func judged(es []market.CorpAct) []market.CorpAct {
	out := make([]market.CorpAct, len(es))
	for i, e := range es {
		e.Handled = e.Handled && restatesShares(e.Ratio)
		out[i] = e
	}
	return out
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

// merge pairs each stored action with a reported action equal in ex-date,
// ratio, type and handling, each at most once. It returns the stored actions
// left unpaired, which the answer no longer reports, and the reported actions
// left unpaired, which are new.
func merge(stored []gen.CorpAct, reported []market.CorpAct) ([]uuid.UUID, []market.CorpAct) {
	paired := make([]bool, len(reported))
	var gone []uuid.UUID
	for _, s := range stored {
		i := firstUnpaired(reported, paired, func(e market.CorpAct) bool {
			return types.Day(e.ExDate).Equal(s.ExDate) && e.Ratio.Equal(types.Ratio{From: s.RatioFrom, To: s.RatioTo}) &&
				e.Type == ptr.Deref(s.Type, "") && e.Handled == s.Handled
		})
		if i < 0 {
			gone = append(gone, s.ID)
			continue
		}
		paired[i] = true
	}
	var added []market.CorpAct
	for i, e := range reported {
		if !paired[i] {
			added = append(added, e)
		}
	}
	return gone, added
}

// firstUnpaired returns the index of the first unpaired element of es that
// satisfies match, or -1 when none does.
func firstUnpaired(es []market.CorpAct, paired []bool, match func(market.CorpAct) bool) int {
	for i, e := range es {
		if !paired[i] && match(e) {
			return i
		}
	}
	return -1
}

// typeOf names a datasource's type of corporate action in a finding's detail.
func typeOf(t string) string {
	if t == "" {
		return "a corporate action of no stated type"
	}
	return t
}

// ratioText states r as a finding's detail does, as "10 for 1".
func ratioText(r types.Ratio) string {
	return r.To.String() + " for " + r.From.String()
}
