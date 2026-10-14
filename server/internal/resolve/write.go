package resolve

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
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
// identifier the write may insert. pick is the agreement that a confirmation
// writes, with its head as the winner, and is nil in a run. The write leaves
// an association the user arbitrated in place and records the key matched.
func (r *Resolver) write(ctx context.Context, run gen.Run, res *resolution, pick agreement) (gen.ResolutionKey, error) {
	var out gen.ResolutionKey
	var written []gen.FindingKind
	for attempt := 1; ; attempt++ {
		err := r.store.Tx(ctx, func(q Queries) error {
			var err error
			out, written, err = r.resolveKey(ctx, q, run, res, pick)
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
func (r *Resolver) resolveKey(ctx context.Context, q Queries, run gen.Run, res *resolution, pick agreement) (gen.ResolutionKey, []gen.FindingKind, error) {
	if res.outcome != "" {
		return recordResolution(ctx, q, run, res, res.outcome, reasons(res.reason), res.findings, 0)
	}
	if res.arbitrated != nil {
		return copyArbitratedAssociation(ctx, q, run, res)
	}
	if err := lockIdentifiers(ctx, q, identifiersToLock(res)); err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	ch := choose(res, nil, pick)
	if ch.refused != "" {
		return gen.ResolutionKey{}, nil, fmt.Errorf("%w: %s", ErrNoCandidate, ch.refused)
	}
	t, err := findWriteTarget(ctx, q, res, ch)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	if t.stored != nil {
		if reason, ok := classConflict(res.row, t.stored.instrument.AssetClass); ok {
			return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeUnrecognised, []string{reason}, t.findings, len(ch.offered))
		}
		ch = choose(res, t.stored.claim(), pick)
		if ch.refused != "" {
			return gen.ResolutionKey{}, nil, fmt.Errorf("%w: %s", ErrNoCandidate, ch.refused)
		}
	}
	ch.findings = append(ch.findings, t.findings...)
	ch.findings = append(ch.findings, res.findings...)
	if ch.winner == nil {
		return recordResolution(ctx, q, run, res, unresolved(res), summary(res, ch), ch.findings, len(ch.offered))
	}
	w := writer{q: q, user: run.UserID, identifiers: map[types.Identifier]gen.Identifier{}, listings: map[string]gen.Listing{}, taken: t.taken}
	if t.stored != nil {
		w.reuse(t.stored)
	} else if w.instrument, err = w.create(ctx, ch.winner); err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	for _, c := range ch.attached {
		if err := w.attach(ctx, c); err != nil {
			return gen.ResolutionKey{}, nil, err
		}
	}
	if err := recordIdentityCoverage(ctx, q, w.instrument.ID, ch, res.fetchKeys); err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	arbiter := gen.ArbiterStated
	switch {
	case pick != nil:
		arbiter = gen.ArbiterUser
	case ch.winner.fetchKey != nil:
		arbiter = gen.ArbiterDatasource
	}
	var mates []gen.StatedKey
	if pick != nil {
		if mates, err = lockGroupMates(ctx, q, run, res); err != nil {
			return gen.ResolutionKey{}, nil, err
		}
	}
	a, ok, err := w.associate(ctx, res, arbiter)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	if !ok {
		return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeUnrecognised, []string{"no identifier to associate through"}, ch.findings, len(ch.offered))
	}
	if a != nil {
		if err := copyGroupAssociation(ctx, q, run, res, *a, mates); err != nil {
			return gen.ResolutionKey{}, nil, err
		}
	}
	return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeMatched, nil, ch.findings, 0)
}

// lockGroupMates takes the user's key lock and returns the keys of res's group,
// read before the association clears the key's group. An upload or a
// replay recomputes the groups under that lock.
func lockGroupMates(ctx context.Context, q Queries, run gen.Run, res *resolution) ([]gen.StatedKey, error) {
	if err := q.LockUserKeys(ctx, run.UserID); err != nil {
		return nil, fmt.Errorf("lock keys: %w", err)
	}
	fresh, err := q.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: res.row.ID, UserID: run.UserID})
	if err != nil {
		return nil, fmt.Errorf("read key: %w", err)
	}
	if fresh.GroupID == nil {
		return nil, nil
	}
	mates, err := q.ListStatedKeysOfGroups(ctx, gen.ListStatedKeysOfGroupsParams{UserID: run.UserID, GroupIds: []uuid.UUID{*fresh.GroupID}})
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
		if _, err := q.SetStatedKeyAssociation(ctx, carry(a, res.fam, k, res.cur)); err != nil {
			return fmt.Errorf("associate key %s: %w", k.ID, err)
		}
		arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: run.UserID, StatedKeyID: k.ID, Outcome: gen.ResolutionOutcomeMatched}
		if _, err := q.CreateResolutionKey(ctx, arg); err != nil {
			return fmt.Errorf("record resolution of key %s: %w", k.ID, err)
		}
	}
	return nil
}

// identifiersToLock returns the identifiers res trusts and every identifier a
// response served for it names.
func identifiersToLock(res *resolution) []types.Identifier {
	ids := slices.Clone(res.trusted)
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			continue
		}
		for _, id := range fk.Response.Filtered {
			ids = appendUnique(ids, id)
		}
		for _, cand := range fk.Response.Candidates {
			for _, id := range cand.Identifiers {
				ids = appendUnique(ids, id)
			}
		}
	}
	return ids
}

// target is where a key's write lands.
type target struct {
	// stored is nil where the write creates an instrument.
	stored *stored
	// taken is the identifiers of the instruments the write leaves apart
	// from stored.
	taken    map[types.Identifier]bool
	findings []gen.CreateFindingParams
}

// findWriteTarget decides which instrument in the database a key takes: the
// instrument identified by the key's identifiers and the claims chosen from the
// identifier datasources. When it finds none, the write creates one. If the
// identifiers identify several instruments, findWriteTarget merges them.
func findWriteTarget(ctx context.Context, q Queries, res *resolution, ch choice) (target, error) {
	ids := slices.Clone(res.trusted)
	for _, c := range ch.attached {
		for id := range c.AllIDs() {
			ids = appendUnique(ids, id)
		}
	}
	hits, err := findInstrumentsByIdentifier(ctx, q, ids)
	if err != nil {
		return target{}, err
	}
	switch len(hits) {
	case 0:
		return target{}, nil
	case 1:
		return target{stored: hits[0].stored}, nil
	}
	var fetchKeyID *uuid.UUID
	if ch.winner != nil && ch.winner.fetchKey != nil {
		fetchKeyID = &ch.winner.fetchKey.ID
	}
	return merge(ctx, q, res, fetchKeyID, hits)
}

// recordIdentityCoverage records instrument as covered by every datasource
// whose claim ch attached, and by every datasource that served no candidate.
// When ch drops every candidate of a datasource, the next key that names the
// instrument asks that datasource again.
func recordIdentityCoverage(ctx context.Context, q Queries, instrument uuid.UUID, ch choice, fetchKeys []*fetchKey) error {
	for _, fk := range fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			continue
		}
		attached := slices.ContainsFunc(ch.attached, func(c *claim) bool { return c.fetchKey == fk })
		if !attached && len(fk.Response.Candidates) > 0 {
			continue
		}
		arg := gen.UpsertIdentityCoverageParams{InstrumentID: instrument, Datasource: fk.Source, FetchKeyID: fk.ID}
		if err := q.UpsertIdentityCoverage(ctx, arg); err != nil {
			return fmt.Errorf("cover %s: %w", fk.Source, err)
		}
	}
	return nil
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
func findInstrumentsByIdentifier(ctx context.Context, q Queries, ids []types.Identifier) ([]hit, error) {
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
	loaded, err := loadInstruments(ctx, q, instruments)
	if err != nil {
		return nil, err
	}
	hits := make([]hit, len(instruments))
	for i, inst := range instruments {
		hits[i] = hit{stored: loaded[inst.ID], matched: matched[inst.ID]}
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return a.stored.instrument.CreatedAt.Compare(b.stored.instrument.CreatedAt)
	})
	return hits, nil
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
	arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: run.UserID, StatedKeyID: res.row.ID, Outcome: outcome, Reasons: reasons, Offered: int32(offered)}
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
func summary(res *resolution, ch choice) []string {
	if len(res.fetchKeys) == 0 {
		return []string{"no datasource enabled"}
	}
	parts := make([]string, 0, len(res.fetchKeys))
	for _, fk := range res.fetchKeys {
		if fk.Outcome != gen.FetchOutcomeServed {
			parts = append(parts, unservedReason(fk))
			continue
		}
		s := fk.Source + ": " + plural(len(fk.Response.Candidates), "candidate")
		if n := ch.counts[fk.Source]; n > 0 {
			s += " in " + sets(n)
		}
		if n := ch.notNaming[fk.Source]; n > 0 {
			s += fmt.Sprintf(", %s not naming %s", sets(n), fk.SentID.Name())
		}
		dropped := 0
		for _, f := range ch.findings {
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

// plural counts n of noun: "1 candidate", "2 candidates".
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
// instrument has, which taken names. identifiers and listings start as the
// instrument's rows and take each row written.
type writer struct {
	q           Queries
	user        uuid.UUID
	instrument  gen.Instrument
	identifiers map[types.Identifier]gen.Identifier
	listings    map[string]gen.Listing
	taken       map[types.Identifier]bool
}

// reuse takes f as the writer's instrument, with its rows.
func (w *writer) reuse(f *stored) {
	w.instrument = f.instrument
	for _, id := range f.identifiers {
		w.identifiers[to.Identifier(id)] = id
	}
	for _, l := range f.listings {
		w.listings[l.Currency] = l
	}
}

// create writes the instrument of the winner, with its class and provenance.
func (w *writer) create(ctx context.Context, winner *claim) (gen.Instrument, error) {
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
// with c's fetch key as provenance, and records the fetch key's response
// against the instrument. A listing takes the primary venue c states only
// while it has none, whichever run or claim created it.
func (w *writer) attach(ctx context.Context, c *claim) error {
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
		l, ok := w.listings[fam]
		var primary *string
		if p := c.Listings[fam].Primary; p != "" {
			primary = &p
		}
		switch {
		case !ok:
			var err error
			l, err = w.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: w.instrument.ID, Currency: fam, FetchKeyID: &fetchKeyID, PrimaryMic: primary})
			if err != nil {
				return fmt.Errorf("create listing in %s: %w", fam, err)
			}
			w.listings[fam] = l
		case l.PrimaryMic == nil && primary != nil:
			if err := w.q.SetListingPrimary(ctx, gen.SetListingPrimaryParams{ID: l.ID, PrimaryMic: primary}); err != nil {
				return fmt.Errorf("set primary venue of listing in %s: %w", fam, err)
			}
			l.PrimaryMic = primary
			w.listings[fam] = l
		}
		for _, id := range c.Listings[fam].Identifiers {
			if err := w.identify(ctx, id, &l.ID, fetchKeyID); err != nil {
				return err
			}
		}
	}
	arg := gen.SetFetchKeyInstrumentParams{ID: fetchKeyID, UserID: w.user, InstrumentID: &w.instrument.ID}
	if err := w.q.SetFetchKeyInstrument(ctx, arg); err != nil {
		return fmt.Errorf("attach fetch key: %w", err)
	}
	for id := range c.AllIDs() {
		arg := gen.CreateFetchIdentifierParams{FetchKeyID: fetchKeyID, Type: id.Type, Domain: id.Domain, Value: id.Value}
		if err := w.q.CreateFetchIdentifier(ctx, arg); err != nil {
			return fmt.Errorf("record fetch identifier: %w", err)
		}
	}
	return nil
}

// identify writes id on the instrument, or on listing where set, unless it
// already identifies the instrument.
func (w *writer) identify(ctx context.Context, id types.Identifier, listing *uuid.UUID, fetchKeyID uuid.UUID) error {
	if _, ok := w.identifiers[id]; ok || w.taken[id] {
		return nil
	}
	arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: w.instrument.ID, ListingID: listing, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &fetchKeyID}
	row, err := w.q.CreateIdentifier(ctx, arg)
	if err != nil {
		return fmt.Errorf("create %s: %w", id.Name(), err)
	}
	w.identifiers[id] = row
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
	if l, ok := w.listings[res.fam]; ok {
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
	arg := gen.SetStatedKeyAssociationParams{ID: res.row.ID, UserID: w.user, InstrumentID: &w.instrument.ID, ListingID: listingID, ViaID: &via.ID, Validity: &validity, Arbiter: arbiter}
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
// strongest trusted identifier of the instrument, with a description ranking
// below every global identifier. Failing that, it is a venue ticker of the
// chosen listing, then a venue ticker of the instrument.
func (w *writer) via(res *resolution, listing *uuid.UUID) (gen.Identifier, bool) {
	for _, id := range res.trusted {
		if row, ok := w.identifiers[id]; ok {
			return row, true
		}
	}
	var tickers []gen.Identifier
	for id, row := range w.identifiers {
		if id.Type == types.IdentifierTypeMicTicker {
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
