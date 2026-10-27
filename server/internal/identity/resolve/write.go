package resolve

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	runs "github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// tries is the number of times a key's write is attempted when the database
// refuses it for a concurrent transaction.
const tries = 3

// write records the outcome of res against run in a transaction of its own.
// A key with a winner is written under an advisory lock on the identifiers
// it states and every identifier its responses name, which covers every
// identifier the write may insert. userChoice is the set of consistent
// claims that a confirmation writes, with its head as the winner. It is nil
// in a run. The write leaves an association the user arbitrated in place and
// records the key matched.
func (r *Resolver) write(ctx context.Context, run gen.Run, res *resolution, userChoice consistentClaims) (gen.ResolutionKey, error) {
	var out gen.ResolutionKey
	var written []gen.FindingKind
	for attempt := 1; ; attempt++ {
		err := r.store.Tx(ctx, func(q Queries) error {
			var err error
			out, written, err = r.resolveKey(ctx, q, run, res, userChoice)
			return err
		})
		if err == nil {
			break
		}
		if !db.IsRetryable(err) || attempt >= tries {
			return gen.ResolutionKey{}, fmt.Errorf("resolve key %s: %w", res.row.ID, err)
		}
		r.log.Debug("retrying a resolution write", "key", res.row.ID, "attempt", attempt)
	}
	for _, kind := range written {
		runs.Found(ctx, kind)
	}
	return out, nil
}

// resolveKey writes res's outcome in one transaction, returning the resolution
// key and the kinds of the findings written.
func (r *Resolver) resolveKey(ctx context.Context, q Queries, run gen.Run, res *resolution, userChoice consistentClaims) (gen.ResolutionKey, []gen.FindingKind, error) {
	res.written = nil
	if res.outcome != "" {
		if res.datedFromFacts {
			if err := writeDated(ctx, q, *run.UserID, res, res.via, gen.ArbiterStated); err != nil {
				return gen.ResolutionKey{}, nil, err
			}
		}
		return recordResolution(ctx, q, run, res, res.outcome, reasons(res.reason), res.findings, 0)
	}
	d, err := decide(ctx, q, res, userChoice)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	v := d.verdict
	if d.conflict != "" {
		return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeUnrecognised, []string{d.conflict}, d.target.findings, len(v.offered))
	}
	if v.winner == nil {
		return recordResolution(ctx, q, run, res, unresolved(res), summary(res, v), v.findings, len(v.offered))
	}
	w, err := writeInstr(ctx, q, *run.UserID, res, d)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	ok, err := associateKey(ctx, res, w, v, userChoice)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	if !ok {
		return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeUnrecognised, []string{"no identifier to associate through"}, v.findings, len(v.offered))
	}
	return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeMatched, nil, v.findings, 0)
}

// decision is what a key's write will do.
type decision struct {
	verdict claimVerdict
	target  writeTarget
	// conflict is set when the instrument's class contradicts the key's. The
	// key then ends unrecognised.
	conflict string
}

// decide chooses the winner among res's claims, with the identifiers it may
// write locked. It returns ErrNoCandidate when the user's choice fails a
// check. Unless the decision has a conflict, its verdict carries every
// finding the write records.
func decide(ctx context.Context, q Queries, res *resolution, userChoice consistentClaims) (decision, error) {
	if err := db.LockIdentifiers(ctx, q, identifiersToLock(res)); err != nil {
		return decision{}, err
	}
	if err := recordUnknown(ctx, q, res); err != nil {
		return decision{}, err
	}
	verdict := res.first(userChoice)
	if verdict.refused != "" {
		return decision{}, fmt.Errorf("%w: %s", ErrNoCandidate, verdict.refused)
	}
	t, err := findWriteTarget(ctx, q, res, verdict)
	if err != nil {
		return decision{}, err
	}
	chooser := verdict.winner
	if t.instr != nil {
		if reason, ok := classConflict(res.row, t.instr.Row.AssetClass); ok {
			return decision{verdict: verdict, target: t, conflict: reason}, nil
		}
		verdict = choose(res, &identityClaim{Instr: t.instr.Instr.Instr()}, userChoice)
		if verdict.refused != "" {
			return decision{}, fmt.Errorf("%w: %s", ErrNoCandidate, verdict.refused)
		}
	}
	dropped, err := dropHeld(ctx, q, &verdict, chooser, t.instr)
	if err != nil {
		return decision{}, err
	}
	verdict.findings = append(verdict.findings, dropped...)
	verdict.findings = append(verdict.findings, t.findings...)
	verdict.findings = append(verdict.findings, res.findings...)
	return decision{verdict: verdict, target: t}, nil
}

// writeInstr writes d's winner to the instrument d targets, creating that
// instrument when d has none. It returns the writer for it.
func writeInstr(ctx context.Context, q Queries, user uuid.UUID, res *resolution, d decision) (*writer, error) {
	w := &writer{q: q, user: user, taken: d.target.taken, res: res}
	if d.target.instr != nil {
		w.instr = d.target.instr.Instr
	} else {
		row, err := w.create(ctx, d.verdict.winner)
		if err != nil {
			return nil, err
		}
		w.instr.Row = row
	}
	for _, c := range d.verdict.attached {
		if err := w.attach(ctx, c); err != nil {
			return nil, err
		}
	}
	if err := recordIdentityCoverage(ctx, q, w.instr.Row.ID, d.verdict, res.fetchKeys); err != nil {
		return nil, err
	}
	return w, nil
}

// associateKey associates res with the instrument w wrote, with the arbiter
// that settled it. It reports false when no identifier of the key leads to
// the instrument.
func associateKey(ctx context.Context, res *resolution, w *writer, v claimVerdict, userChoice consistentClaims) (bool, error) {
	arbiter := gen.ArbiterStated
	switch {
	case userChoice != nil:
		arbiter = gen.ArbiterUser
	case v.winner.fetchKey != nil:
		arbiter = gen.ArbiterDatasource
	}
	return w.associate(ctx, res, arbiter)
}

// identifiersToLock returns the global identifiers and broker descriptions res
// states and every identifier a response served for it names.
func identifiersToLock(res *resolution) []types.Identifier {
	ids := slices.Clone(res.globalAndDescriptionIDs)
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			continue
		}
		for _, id := range fk.Response.Filtered {
			ids = appendUnique(ids, id)
		}
		for _, listing := range fk.Response.Listings {
			for _, id := range listing.Identifiers {
				ids = appendUnique(ids, id)
			}
		}
	}
	return ids
}

// recordIdentityCoverage records instrument as covered by every datasource
// whose claims the verdict attached, and by every datasource that served no
// listing. When the verdict drops every claim of a datasource, the next key
// that names the instrument asks that datasource again.
func recordIdentityCoverage(ctx context.Context, q Queries, instr uuid.UUID, verdict claimVerdict, fetchKeys []*fetchKey) error {
	for _, fk := range fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			continue
		}
		attached := slices.ContainsFunc(verdict.attached, func(c *identityClaim) bool { return c.fetchKey == fk })
		if !attached && len(fk.Response.Listings) > 0 {
			continue
		}
		arg := gen.UpsertIdentityCoverageParams{InstrID: instr, Datasource: fk.Source, FetchKeyID: fk.ID}
		if err := q.UpsertIdentityCoverage(ctx, arg); err != nil {
			return fmt.Errorf("cover %s: %w", fk.Source, err)
		}
	}
	return nil
}

// recordUnknown writes a negative assertion of the identifier sent for each
// fetch key of res whose datasource stated that it knows nothing under it.
// The assertion stands whatever the resolution makes of the other answers.
func recordUnknown(ctx context.Context, q Queries, res *resolution) error {
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed || !fk.Response.Unknown || fk.SentID == nil {
			continue
		}
		id := *fk.SentID
		if err := tickervalidity.AssertIdentifier(ctx, q, res.mics, fk.ID, id, nil, fk.AnsweredAt); err != nil {
			return fmt.Errorf("record negative assertion: %w", err)
		}
		res.wrote(fk, id)
	}
	return nil
}

// writeFindings writes findings against run and res's key, returning their
// kinds.
func writeFindings(ctx context.Context, q Queries, run gen.Run, res *resolution, findings []gen.CreateFindingParams) ([]gen.FindingKind, error) {
	var kinds []gen.FindingKind
	for _, f := range findings {
		f.ID, f.RunID, f.StatedKeyID = db.NewID(), run.ID, &res.row.ID
		if err := q.CreateFinding(ctx, f); err != nil {
			return nil, fmt.Errorf("create finding: %w", err)
		}
		kinds = append(kinds, f.Kind)
	}
	return kinds, nil
}

// recordResolution writes res's findings and resolution key. A non-zero offered
// lets the user choose an instrument.
func recordResolution(ctx context.Context, q Queries, run gen.Run, res *resolution, outcome gen.ResolutionOutcome, reasons []string, findings []gen.CreateFindingParams, offered int) (gen.ResolutionKey, []gen.FindingKind, error) {
	kinds, err := writeFindings(ctx, q, run, res, findings)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: *run.UserID, StatedKeyID: res.row.ID, Outcome: outcome, Reasons: reasons, Offered: int32(offered)}
	rk, err := q.CreateResolutionKey(ctx, arg)
	if err != nil {
		return gen.ResolutionKey{}, nil, fmt.Errorf("record resolution: %w", err)
	}
	return rk, kinds, nil
}
