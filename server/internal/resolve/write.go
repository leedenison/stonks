package resolve

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/join"
	"github.com/leedenison/stonks/server/internal/db/to"
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
	if res.outcome != "" {
		return recordResolution(ctx, q, run, res, res.outcome, reasons(res.reason), res.findings, 0)
	}
	if res.arbitrated != nil {
		return copyArbitratedAssociation(ctx, q, run, res)
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
	w, err := writeInstrument(ctx, q, *run.UserID, res, d)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	ok, err := associateKey(ctx, q, run, res, w, v, userChoice)
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
	if err := lockIdentifiers(ctx, q, identifiersToLock(res)); err != nil {
		return decision{}, err
	}
	if err := recordUnknown(ctx, q, res.fetchKeys); err != nil {
		return decision{}, err
	}
	verdict := choose(res, nil, userChoice)
	if verdict.refused != "" {
		return decision{}, fmt.Errorf("%w: %s", ErrNoCandidate, verdict.refused)
	}
	t, err := findWriteTarget(ctx, q, res, verdict)
	if err != nil {
		return decision{}, err
	}
	if t.instrument != nil {
		if reason, ok := classConflict(res.row, t.instrument.Row.AssetClass); ok {
			return decision{verdict: verdict, target: t, conflict: reason}, nil
		}
		verdict = choose(res, &identityClaim{Instrument: t.instrument.Instrument.Instrument()}, userChoice)
		if verdict.refused != "" {
			return decision{}, fmt.Errorf("%w: %s", ErrNoCandidate, verdict.refused)
		}
	}
	verdict.findings = append(verdict.findings, t.findings...)
	verdict.findings = append(verdict.findings, res.findings...)
	return decision{verdict: verdict, target: t}, nil
}

// writeInstrument writes d's winner to the instrument d targets, creating that
// instrument when d has none. It returns the writer for it.
func writeInstrument(ctx context.Context, q Queries, user uuid.UUID, res *resolution, d decision) (*writer, error) {
	w := &writer{q: q, user: user, taken: d.target.taken}
	if d.target.instrument != nil {
		w.instr = d.target.instrument.Instrument
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

// associateKey associates res with the instrument w wrote. When the user
// chose, it also gives the key's group the same association. It reports false
// when no identifier of the key leads to the instrument.
func associateKey(ctx context.Context, q Queries, run gen.Run, res *resolution, w *writer, v claimVerdict, userChoice consistentClaims) (bool, error) {
	arbiter := gen.ArbiterStated
	switch {
	case userChoice != nil:
		arbiter = gen.ArbiterUser
	case v.winner.fetchKey != nil:
		arbiter = gen.ArbiterDatasource
	}
	var mates []gen.StatedKey
	if userChoice != nil {
		var err error
		if mates, err = lockGroupMates(ctx, q, run, res); err != nil {
			return false, err
		}
	}
	a, ok, err := w.associate(ctx, res, arbiter)
	if err != nil || !ok {
		return ok, err
	}
	if a != nil {
		if err := copyGroupAssociation(ctx, q, run, res, *a, mates); err != nil {
			return false, err
		}
	}
	return true, nil
}

// lockGroupMates takes the user's key lock and returns the keys of res's group,
// read before the association clears the key's group. An upload or a
// replay recomputes the groups under that lock.
func lockGroupMates(ctx context.Context, q Queries, run gen.Run, res *resolution) ([]gen.StatedKey, error) {
	if err := q.LockUserKeys(ctx, *run.UserID); err != nil {
		return nil, fmt.Errorf("lock keys: %w", err)
	}
	fresh, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: res.row.ID, UserID: *run.UserID})
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	if fresh.GroupID == nil {
		return nil, nil
	}
	mates, err := q.ListStatedKeysOfGroups(ctx, gen.ListStatedKeysOfGroupsParams{UserID: *run.UserID, GroupIds: []uuid.UUID{*fresh.GroupID}})
	if err != nil {
		return nil, fmt.Errorf("list group: %w", err)
	}
	return mates, nil
}

// copyGroupAssociation gives every key of mates but res's own the association
// a, carried to each, with a matched resolution key of run.
func copyGroupAssociation(ctx context.Context, q Queries, run gen.Run, res *resolution, a gen.SetStatedKeyAssociationParams, mates []gen.StatedKey) error {
	for _, k := range mates {
		if k.ID == res.row.ID {
			continue
		}
		if _, err := q.SetStatedKeyAssociation(ctx, carry(a, res.family, k, res.currencies)); err != nil {
			return fmt.Errorf("associate key %s: %w", k.ID, err)
		}
		arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: *run.UserID, StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeMatched}
		if _, err := q.CreateResolutionKey(ctx, arg); err != nil {
			return fmt.Errorf("record resolution of key %s: %w", k.ID, err)
		}
	}
	return nil
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

// writeTarget is where a key's write lands.
type writeTarget struct {
	// instrument is nil where the write creates an instrument.
	instrument *join.InstrumentWithCoverage
	// taken is the identifiers of the instruments the write leaves apart
	// from instrument.
	taken    map[types.Identifier]bool
	findings []gen.CreateFindingParams
}

// findWriteTarget decides which instrument in the database a key takes: the
// instrument identified by the key's identifiers and the claims chosen from the
// identifier datasources. When it finds none, the write creates one. If the
// identifiers identify several instruments, findWriteTarget merges them.
func findWriteTarget(ctx context.Context, q Queries, res *resolution, verdict claimVerdict) (writeTarget, error) {
	ids := slices.Clone(res.globalAndDescriptionIDs)
	for _, c := range verdict.attached {
		for id := range c.AllIDs() {
			ids = appendUnique(ids, id)
		}
	}
	matches, err := findInstrumentsByIdentifier(ctx, q, ids)
	if err != nil {
		return writeTarget{}, err
	}
	switch len(matches) {
	case 0:
		return writeTarget{}, nil
	case 1:
		return writeTarget{instrument: matches[0].instrument}, nil
	}
	var fetchKeyID *uuid.UUID
	if verdict.winner != nil && verdict.winner.fetchKey != nil {
		fetchKeyID = &verdict.winner.fetchKey.ID
	}
	return merge(ctx, q, res, fetchKeyID, matches)
}

// recordIdentityCoverage records instrument as covered by every datasource
// whose claims the verdict attached, and by every datasource that served no
// listing. When the verdict drops every claim of a datasource, the next key
// that names the instrument asks that datasource again.
func recordIdentityCoverage(ctx context.Context, q Queries, instrument uuid.UUID, verdict claimVerdict, fetchKeys []*fetchKey) error {
	for _, fk := range fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			continue
		}
		attached := slices.ContainsFunc(verdict.attached, func(c *identityClaim) bool { return c.fetchKey == fk })
		if !attached && len(fk.Response.Listings) > 0 {
			continue
		}
		arg := gen.UpsertIdentityCoverageParams{InstrumentID: instrument, Datasource: fk.Source, FetchKeyID: fk.ID}
		if err := q.UpsertIdentityCoverage(ctx, arg); err != nil {
			return fmt.Errorf("cover %s: %w", fk.Source, err)
		}
	}
	return nil
}

// recordUnknown writes a negative assertion of the identifier sent for each
// fetch key whose datasource stated that it knows nothing under it. The
// assertion stands whatever the resolution makes of the other answers.
func recordUnknown(ctx context.Context, q Queries, fetchKeys []*fetchKey) error {
	for _, fk := range fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed || !fk.Response.Unknown || fk.SentID == nil {
			continue
		}
		id := *fk.SentID
		arg := gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), FetchKeyID: &fk.ID, Type: id.Type, Domain: id.Domain, Value: id.Value, AssertedOn: day(fk.AnsweredAt),
		}
		if err := q.CreateIdentifierAssertion(ctx, arg); err != nil {
			return fmt.Errorf("record negative assertion: %w", err)
		}
	}
	return nil
}

// day is the UTC day of t, on which an assertion made at t holds.
func day(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, time.UTC)
}

// lockIdentifiers takes the transaction's advisory lock on each of ids, in one
// order whatever the caller's.
func lockIdentifiers(ctx context.Context, q Queries, ids []types.Identifier) error {
	if len(ids) == 0 {
		return nil
	}
	keys := make([]string, len(ids))
	for i, id := range ids {
		keys[i] = fmt.Sprintf("identifier:%s:%s:%s", id.Type, id.Domain, id.Value)
	}
	slices.Sort(keys)
	if err := q.LockIdentifiers(ctx, keys); err != nil {
		return fmt.Errorf("lock identifiers: %w", err)
	}
	return nil
}

// findInstrumentsByIdentifier finds the instruments any of ids identifies,
// each with the identifiers that matched it, the earliest created first.
func findInstrumentsByIdentifier(ctx context.Context, q Queries, ids []types.Identifier) ([]instrumentMatch, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	arg := gen.ListInstrumentsByIdentifiersParams{Types: make([]string, len(ids)), Domains: make([]string, len(ids)), Values: make([]string, len(ids))}
	for i, id := range ids {
		arg.Types[i], arg.Domains[i], arg.Values[i] = string(id.Type), id.Domain, id.Value
	}
	rows, err := q.ListInstrumentsByIdentifiers(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("find instruments: %w", err)
	}
	var instruments []gen.Instrument
	matched := map[uuid.UUID][]types.Identifier{}
	for _, row := range rows {
		if _, ok := matched[row.Instrument.ID]; !ok {
			instruments = append(instruments, row.Instrument)
		}
		matched[row.Instrument.ID] = append(matched[row.Instrument.ID], to.Identifier(row.Identifier))
	}
	loaded, err := join.InstrumentsWithCoverage(ctx, q, instruments)
	if err != nil {
		return nil, err
	}
	matches := make([]instrumentMatch, len(instruments))
	for i, inst := range instruments {
		matches[i] = instrumentMatch{instrument: loaded[inst.ID], matched: matched[inst.ID]}
	}
	slices.SortFunc(matches, func(a, b instrumentMatch) int {
		return a.instrument.Row.CreatedAt.Compare(b.instrument.Row.CreatedAt)
	})
	return matches, nil
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

// unresolved is the outcome of a resolution no claim won:
// unavailable where a datasource failed or was blocked for it, unrecognised
// otherwise.
func unresolved(res *resolution) gen.ResolutionOutcome {
	for _, fk := range res.fetchKeys {
		switch fk.Outcome {
		case gen.FetchOutcomeBlocked, gen.FetchOutcomeFailedTemporary, gen.FetchOutcomeFailedPermanent:
			return gen.ResolutionOutcomeUnavailable
		}
	}
	return gen.ResolutionOutcomeUnrecognised
}

// unserved words each outcome other than served for a reason.
var unserved = map[gen.FetchOutcome]string{
	gen.FetchOutcomeNotServed:       "skipped",
	gen.FetchOutcomeBlocked:         "blocked",
	gen.FetchOutcomeFailedTemporary: "failed",
	gen.FetchOutcomeFailedPermanent: "failed",
}

// unservedReason says why fk served nothing: the datasource, the outcome
// and the reason recorded, as in "a: failed: 503".
func unservedReason(fk *fetchKey) string {
	s := fk.Source + ": " + unserved[fk.Outcome]
	if fk.Reason != "" {
		s += ": " + fk.Reason
	}
	return s
}

// reasons returns reason as a one-entry list, or nil if reason is empty.
func reasons(reason string) []string {
	if reason == "" {
		return nil
	}
	return []string{reason}
}

// summary says, for each datasource, what it served for res and what became
// of it.
func summary(res *resolution, verdict claimVerdict) []string {
	if len(res.fetchKeys) == 0 {
		return []string{"no datasource enabled"}
	}
	parts := make([]string, 0, len(res.fetchKeys))
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			parts = append(parts, unservedReason(fk))
			continue
		}
		s := fk.Source + ": " + plural(len(fk.Response.Listings), "listing")
		if n := verdict.counts[fk.Source]; n > 0 {
			s += " in " + sets(n)
		}
		if n := verdict.notNaming[fk.Source]; n > 0 {
			s += fmt.Sprintf(", %s not naming %s", sets(n), fk.SentID.Name())
		}
		dropped := 0
		for _, f := range verdict.findings {
			if f.Kind == gen.FindingKindDropped && f.FetchKeyID != nil && *f.FetchKeyID == fk.ID {
				dropped++
			}
		}
		if dropped > 0 {
			s += ", " + sets(dropped) + " dropped"
		}
		parts = append(parts, s)
	}
	return parts
}

// plural counts n of noun: "1 listing", "2 listings".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// sets counts n sets of identifiers: "1 set of identifiers", "2 sets of
// identifiers".
func sets(n int) string {
	if n == 1 {
		return "1 set of identifiers"
	}
	return fmt.Sprintf("%d sets of identifiers", n)
}

// writer writes the listings and identifiers of the attached claims onto one
// instrument. It skips each the instrument already has and each another
// instrument has, which taken names. instr starts as the instrument's rows and
// takes each row written.
type writer struct {
	q     Queries
	user  uuid.UUID
	instr join.Instrument
	taken map[types.Identifier]bool
}

// create writes the instrument of the winner, with its class and provenance.
func (w *writer) create(ctx context.Context, winner *identityClaim) (gen.Instrument, error) {
	class := winner.Class
	if class == "" {
		class = types.AssetClassUnknown
	}
	inst, err := w.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: class, FetchKeyID: &winner.fetchKey.ID})
	if err != nil {
		return gen.Instrument{}, fmt.Errorf("create instrument: %w", err)
	}
	return inst, nil
}

// attach writes the listings and identifiers of c the instrument lacks, each
// with c's fetch key as provenance, records the fetch key's response against
// the instrument, and asserts each of c's identifiers on the UTC day of the
// answer. A listing takes the primary venue c states only while it has none,
// whichever run or claim created it.
func (w *writer) attach(ctx context.Context, c *identityClaim) error {
	fetchKeyID := c.fetchKey.ID
	for _, id := range c.Identifiers {
		if err := w.identify(ctx, id, nil, fetchKeyID); err != nil {
			return err
		}
	}
	for _, fam := range slices.Sorted(maps.Keys(c.Listings)) {
		if fam == noFamily {
			continue
		}
		l := w.instr.Listing(fam)
		var primary *string
		if p := c.Listings[fam].Primary; p != "" {
			primary = &p
		}
		switch {
		case l == nil:
			row, err := w.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: w.instr.Row.ID, Currency: fam, FetchKeyID: &fetchKeyID, PrimaryMic: primary})
			if err != nil {
				return fmt.Errorf("create listing in %s: %w", fam, err)
			}
			w.instr.Listings = append(w.instr.Listings, row)
			l = &row
		case l.PrimaryMic == nil && primary != nil:
			if err := w.q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: l.ID, PrimaryMic: primary}); err != nil {
				return fmt.Errorf("set primary venue of listing in %s: %w", fam, err)
			}
			l.PrimaryMic = primary
		}
		listing := l.ID
		for _, id := range c.Listings[fam].Identifiers {
			if err := w.identify(ctx, id, &listing, fetchKeyID); err != nil {
				return err
			}
		}
	}
	arg := gen.SetFetchKeyInstrumentParams{ID: fetchKeyID, UserID: &w.user, InstrumentID: &w.instr.Row.ID}
	if err := w.q.SetFetchKeyInstrument(ctx, arg); err != nil {
		return fmt.Errorf("attach fetch key: %w", err)
	}
	on := day(c.fetchKey.AnsweredAt)
	for id := range c.AllIDs() {
		arg := gen.CreateIdentifierAssertionParams{
			ID: db.NewID(), FetchKeyID: &fetchKeyID, Type: id.Type, Domain: id.Domain, Value: id.Value,
			InstrumentID: &w.instr.Row.ID, AssertedOn: on,
		}
		if err := w.q.CreateIdentifierAssertion(ctx, arg); err != nil {
			return fmt.Errorf("record assertion: %w", err)
		}
	}
	return nil
}

// identify writes id on the instrument, or on listing where set, unless it
// already identifies the instrument.
func (w *writer) identify(ctx context.Context, id types.Identifier, listing *uuid.UUID, fetchKeyID uuid.UUID) error {
	if w.instr.Identifier(id) != nil || w.taken[id] {
		return nil
	}
	arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: w.instr.Row.ID, ListingID: listing, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &fetchKeyID}
	row, err := w.q.CreateIdentifier(ctx, arg)
	if err != nil {
		return fmt.Errorf("create %s: %w", id.Name(), err)
	}
	w.instr.Identifiers = append(w.instr.Identifiers, row)
	return nil
}

// associate writes the association of res with the instrument, through the
// identifier via returns and on the listing of the stated family, with
// arbiter. It reports false when no identifier identifies the instrument.
// It returns the association written. It returns nil when the user already
// arbitrated the key's association; the write leaves that association in
// place.
func (w *writer) associate(ctx context.Context, res *resolution, arbiter gen.Arbiter) (*gen.SetStatedKeyAssociationParams, bool, error) {
	var listingID *uuid.UUID
	if l := w.instr.Listing(res.family); l != nil {
		listingID = &l.ID
	}
	via, ok := w.via(res, listingID)
	if !ok {
		return nil, false, nil
	}
	validity := gen.ValidityProvisional
	if to.Identifier(via).Stable() {
		validity = gen.ValidityConfirmed
	}
	arg := gen.SetStatedKeyAssociationParams{ID: res.row.ID, UserID: w.user, InstrumentID: &w.instr.Row.ID, ListingID: listingID, ViaID: &via.ID, Validity: &validity, Arbiter: arbiter}
	n, err := w.q.SetStatedKeyAssociation(ctx, arg)
	if err != nil {
		return nil, false, fmt.Errorf("associate key: %w", err)
	}
	if n == 0 {
		return nil, true, nil
	}
	return &arg, true, nil
}

// via returns the identifier that carries res's association. That is the
// strongest global identifier or broker description of res that also
// identifies the instrument, with a description ranking below every global
// identifier. Failing that, it is a venue ticker of the chosen listing, then a
// venue ticker of the instrument.
func (w *writer) via(res *resolution, listing *uuid.UUID) (gen.Identifier, bool) {
	for _, id := range res.globalAndDescriptionIDs {
		if row := w.instr.Identifier(id); row != nil {
			return *row, true
		}
	}
	var tickers []gen.Identifier
	for _, row := range w.instr.Identifiers {
		if row.Type == types.IdentifierTypeMicTicker {
			tickers = append(tickers, row)
		}
	}
	slices.SortFunc(tickers, func(a, b gen.Identifier) int {
		return strings.Compare(a.Domain+":"+a.Value, b.Domain+":"+b.Value)
	})
	for _, row := range tickers {
		if listing != nil && row.ListingID != nil && *row.ListingID == *listing {
			return row, true
		}
	}
	if len(tickers) > 0 {
		return tickers[0], true
	}
	return gen.Identifier{}, false
}
