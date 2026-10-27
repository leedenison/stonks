package corpact

import (
	"cmp"
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// window is how many calendar days either side of an action the matching
// action of another datasource may fall. The system holds no trading
// calendar, so the count is in calendar days.
const window = 7

// compare writes a disagreement finding for each action that another
// datasource's answer does not match, where one of the two answers is in
// recorded. An action is reported once, whichever call finds it first.
func (r *Recorder) compare(ctx context.Context, recorded []recordedKey) error {
	if len(recorded) == 0 {
		return nil
	}
	mine := map[uuid.UUID]gen.Run{}
	var ts []types.MarketTicker
	for _, rec := range recorded {
		mine[rec.key.ID] = rec.key.Run
		ts = append(ts, rec.ticker)
	}
	var found int
	err := r.store.Tx(ctx, func(q Queries) error {
		found = 0
		stored, err := readStored(ctx, q, ts, always)
		if err != nil {
			return err
		}
		var fs []gen.CreateFindingParams
		for t, as := range stored {
			fs = append(fs, disagreements(t, as, mine)...)
		}
		if len(fs) == 0 {
			return nil
		}
		ids := make([]uuid.UUID, len(fs))
		for i, f := range fs {
			ids[i] = *f.CorpActID
		}
		named, err := q.ListDisagreedCorpActs(ctx, ids)
		if err != nil {
			return fmt.Errorf("list disagreed corporate actions: %w", err)
		}
		for _, f := range fs {
			if slices.Contains(named, *f.CorpActID) {
				continue
			}
			f.ID = db.NewID()
			if err := q.CreateFinding(ctx, f); err != nil {
				return fmt.Errorf("create finding: %w", err)
			}
			found++
		}
		return nil
	})
	if err != nil {
		return fmt.Errorf("record corporate action disagreements: %w", err)
	}
	for range found {
		runs.Found(ctx, gen.FindingKindCorpActDisagreement)
	}
	return nil
}

// disagreements returns a finding for each action of t that the other answer
// of a pair does not match. It compares only pairs where mine holds the
// fetch key of at least one answer. A finding goes on the run of the action's
// own answer when mine holds its fetch key, and on the other answer's run
// otherwise. An action is reported once, for the first answer that lacks a
// match for it.
func disagreements(t types.MarketTicker, as []storedAnswer, mine map[uuid.UUID]gen.Run) []gen.CreateFindingParams {
	var out []gen.CreateFindingParams
	seen := map[uuid.UUID]bool{}
	report := func(e Act, own, other storedAnswer) {
		if seen[e.ID] {
			return
		}
		seen[e.ID] = true
		run, ok := mine[own.fetchKey]
		if !ok {
			run = mine[other.fetchKey]
		}
		detail := fmt.Sprintf("%s on %s: %s reported %s, and %s, which covers the day, reported no equal ratio within %d days",
			t.Name(), e.ExDate.Format(time.DateOnly), own.Datasource, ratioText(e.Ratio), other.Datasource, window)
		out = append(out, gen.CreateFindingParams{RunID: run.ID, Kind: gen.FindingKindCorpActDisagreement, CorpActID: &e.ID, Detail: &detail})
	}
	for i, a := range as {
		for _, b := range as[i+1:] {
			_, ma := mine[a.fetchKey]
			_, mb := mine[b.fetchKey]
			if !ma && !mb {
				continue
			}
			ua, ub := unmatched(a.Answer, b.Answer)
			for _, e := range ua {
				report(e, a, b)
			}
			for _, e := range ub {
				report(e, b, a)
			}
		}
	}
	return out
}

// unmatched returns the actions of a and of b that remain unpaired, on a day
// both cover. Two actions pair when their ratios are equal and their ex-dates
// are at most window days apart. Each action pairs at most once: the nearest
// pair first, then the pair whose actions are earlier, then the pair whose
// actions have the lower ids. A split that one datasource reports twice
// therefore leaves one of the two unmatched.
func unmatched(a, b Answer) ([]Act, []Act) {
	overlap := overlaps(a.Coverage, b.Coverage)
	if len(overlap) == 0 {
		return nil, nil
	}
	type pair struct {
		i, j int
		gap  int
	}
	var pairs []pair
	for i, ea := range a.Acts {
		for j, eb := range b.Acts {
			gap := days(ea.ExDate, eb.ExDate)
			if gap <= window && ea.Ratio.Equal(eb.Ratio) {
				pairs = append(pairs, pair{i: i, j: j, gap: gap})
			}
		}
	}
	slices.SortFunc(pairs, func(x, y pair) int {
		return cmp.Or(
			cmp.Compare(x.gap, y.gap),
			a.Acts[x.i].ExDate.Compare(a.Acts[y.i].ExDate),
			b.Acts[x.j].ExDate.Compare(b.Acts[y.j].ExDate),
			slices.Compare(a.Acts[x.i].ID[:], a.Acts[y.i].ID[:]),
			slices.Compare(b.Acts[x.j].ID[:], b.Acts[y.j].ID[:]),
		)
	})
	pa, pb := make([]bool, len(a.Acts)), make([]bool, len(b.Acts))
	for _, p := range pairs {
		if !pa[p.i] && !pb[p.j] {
			pa[p.i], pb[p.j] = true, true
		}
	}
	left := func(es []Act, paired []bool) []Act {
		var out []Act
		for i, e := range es {
			if !paired[i] && holds(overlap, e.ExDate) {
				out = append(out, e)
			}
		}
		return out
	}
	return left(a.Acts, pa), left(b.Acts, pb)
}

// overlaps returns the days that both xs and ys hold.
func overlaps(xs, ys []types.Period) []types.Period {
	var out []types.Period
	for _, x := range xs {
		for _, y := range ys {
			if c, ok := clip(x, y); ok {
				out = append(out, c)
			}
		}
	}
	return out
}

// holds reports whether any of ps holds the day d.
func holds(ps []types.Period, d time.Time) bool {
	return slices.ContainsFunc(ps, func(p types.Period) bool { return p.Contains(d) })
}

// days is how many calendar days apart a and b are.
func days(a, b time.Time) int {
	d := int(a.Sub(b).Hours() / 24)
	if d < 0 {
		return -d
	}
	return d
}
