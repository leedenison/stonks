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
	"github.com/leedenison/stonks/server/internal/db/types"
	runs "github.com/leedenison/stonks/server/internal/run"
)

// tries is the number of times a key's write is attempted when a concurrent
// insert refuses it.
const tries = 3

// write records the outcome of res against run in a transaction of its own,
// retried on conflict. A key with a winner is written under an advisory lock
// on the identifiers it states; a key the lookup decided takes no lock.
func (r *Resolver) write(ctx context.Context, run gen.Run, res *resolution, families func(string) string) (gen.ResolutionKey, error) {
	var out gen.ResolutionKey
	var written []gen.FindingKind
	for attempt := 1; ; attempt++ {
		err := r.store.Tx(ctx, func(q Queries) error {
			var err error
			out, written, err = r.resolveKey(ctx, q, run, res, families)
			return err
		})
		if err == nil {
			break
		}
		if !db.IsConflict(err) || attempt >= tries {
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
func (r *Resolver) resolveKey(ctx context.Context, q Queries, run gen.Run, res *resolution, families func(string) string) (gen.ResolutionKey, []gen.FindingKind, error) {
	if res.outcome != "" {
		return record(ctx, q, run, res, res.outcome, res.reason, res.findings)
	}
	if err := lock(ctx, q, res.guids); err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	c := choose(res.results, res.row, nil, families)
	f, taken, merged, err := find(ctx, q, res, c)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	if f != nil {
		if res.row.AssetClass != nil && Disjoint(*res.row.AssetClass, f.instrument.AssetClass) {
			reason := fmt.Sprintf("asset class %s contradicts the instrument's %s", *res.row.AssetClass, f.instrument.AssetClass)
			return record(ctx, q, run, res, gen.ResolutionOutcomeUnrecognised, reason, append(c.findings, merged...))
		}
		c = choose(res.results, res.row, f.group(), families)
	}
	c.findings = append(c.findings, merged...)
	if c.winner == nil {
		return record(ctx, q, run, res, unresolved(res), summary(res, c), c.findings)
	}
	w := writer{q: q, user: run.UserID, identifiers: map[types.Identifier]gen.Identifier{}, listings: map[string]gen.Listing{}, taken: taken}
	if f != nil {
		w.reuse(f)
	} else if w.instrument, err = w.create(ctx, c.winner); err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	for _, g := range c.attached {
		if err := w.attach(ctx, g); err != nil {
			return gen.ResolutionKey{}, nil, err
		}
	}
	if err := cover(ctx, q, w.instrument.ID, res.results); err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	ok, err := w.associate(ctx, res, families)
	if err != nil {
		return gen.ResolutionKey{}, nil, err
	}
	if !ok {
		return record(ctx, q, run, res, gen.ResolutionOutcomeUnrecognised, "no identifier to associate through", c.findings)
	}
	return record(ctx, q, run, res, gen.ResolutionOutcomeMatched, "", c.findings)
}

// find re-reads the database for the instrument the key and its attached
// groups name, nil where there is none. Several found are merged, and taken
// is the identifiers of any left apart.
func find(ctx context.Context, q Queries, res *resolution, c choice) (*found, map[types.Identifier]bool, []gen.CreateFindingParams, error) {
	ids := slices.Clone(res.guids)
	for _, g := range c.attached {
		for id := range g.all {
			ids = appendUnique(ids, id)
		}
	}
	hits, err := reread(ctx, q, ids)
	if err != nil {
		return nil, nil, nil, err
	}
	switch len(hits) {
	case 0:
		return nil, nil, nil, nil
	case 1:
		return hits[0].found, nil, nil, nil
	}
	var fetchKey *uuid.UUID
	if c.winner != nil && c.winner.r != nil {
		fetchKey = &c.winner.r.ID
	}
	return merge(ctx, q, res, fetchKey, hits)
}

// cover records instrument as covered by every datasource that served a
// result.
func cover(ctx context.Context, q Queries, instrument uuid.UUID, results []*result) error {
	for _, rs := range results {
		if rs.Outcome != gen.FetchOutcomeServed {
			continue
		}
		arg := gen.UpsertIdentityCoverageParams{InstrumentID: instrument, Datasource: rs.Source, FetchKeyID: rs.ID}
		if err := q.UpsertIdentityCoverage(ctx, arg); err != nil {
			return fmt.Errorf("cover %s: %w", rs.Source, err)
		}
	}
	return nil
}

// lock takes the transaction's advisory lock on each of ids, in one order
// whatever the caller's.
func lock(ctx context.Context, q Queries, ids []types.Identifier) error {
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

// reread finds the instruments any of ids identifies, each with the
// identifiers that matched it, the earliest created first.
func reread(ctx context.Context, q Queries, ids []types.Identifier) ([]hit, error) {
	arg := gen.ListInstrumentsByIdentifiersParams{Types: make([]string, len(ids)), Domains: make([]string, len(ids)), Values: make([]string, len(ids))}
	for i, id := range ids {
		arg.Types[i], arg.Domains[i], arg.Values[i] = string(id.Type), id.Domain, id.Value
	}
	rows, err := q.ListInstrumentsByIdentifiers(ctx, arg)
	if err != nil {
		return nil, fmt.Errorf("find instruments: %w", err)
	}
	var hits []hit
	byID := map[uuid.UUID]int{}
	for _, row := range rows {
		i, ok := byID[row.Instrument.ID]
		if !ok {
			f, err := load(ctx, q, row.Instrument)
			if err != nil {
				return nil, err
			}
			i = len(hits)
			byID[row.Instrument.ID] = i
			hits = append(hits, hit{found: f})
		}
		hits[i].matched = append(hits[i].matched, to.Identifier(row.Identifier))
	}
	slices.SortFunc(hits, func(a, b hit) int {
		return a.found.instrument.CreatedAt.Compare(b.found.instrument.CreatedAt)
	})
	return hits, nil
}

// record writes res's findings and resolution key.
func record(ctx context.Context, q Queries, run gen.Run, res *resolution, outcome gen.ResolutionOutcome, reason string, findings []gen.CreateFindingParams) (gen.ResolutionKey, []gen.FindingKind, error) {
	var kinds []gen.FindingKind
	for _, f := range findings {
		f.ID, f.RunID, f.StatedKeyID = db.NewID(), run.ID, &res.row.ID
		if err := q.CreateFinding(ctx, f); err != nil {
			return gen.ResolutionKey{}, nil, fmt.Errorf("create finding: %w", err)
		}
		kinds = append(kinds, f.Kind)
	}
	arg := gen.CreateResolutionKeyParams{RunID: run.ID, UserID: run.UserID, StatedKeyID: res.row.ID, Outcome: outcome}
	if reason != "" {
		arg.Reason = &reason
	}
	if err := q.CreateResolutionKey(ctx, arg); err != nil {
		return gen.ResolutionKey{}, nil, fmt.Errorf("record resolution: %w", err)
	}
	return gen.ResolutionKey{RunID: run.ID, UserID: run.UserID, StatedKeyID: res.row.ID, Outcome: outcome, Reason: arg.Reason}, kinds, nil
}

// unresolved is the outcome of a resolution no group won: unavailable where a
// datasource failed or was blocked for it, unrecognised otherwise.
func unresolved(res *resolution) gen.ResolutionOutcome {
	for _, rs := range res.results {
		switch rs.Outcome {
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

// summary says what each datasource served for res and what became of it.
func summary(res *resolution, c choice) string {
	if len(res.results) == 0 {
		return "no datasource enabled"
	}
	parts := make([]string, 0, len(res.results))
	for _, rs := range res.results {
		if rs.Outcome != gen.FetchOutcomeServed {
			s := rs.Source + ": " + unserved[rs.Outcome]
			if rs.Reason != "" {
				s += ": " + rs.Reason
			}
			parts = append(parts, s)
			continue
		}
		s := rs.Source + ": " + plural(len(rs.Response.Candidates), "candidate")
		if n := c.groups[rs.Source]; n > 0 {
			s += " in " + plural(n, "group")
		}
		if n := c.notNaming[rs.Source]; n > 0 {
			s += fmt.Sprintf(", %s not naming %s", plural(n, "group"), name(*rs.Sent))
		}
		dropped := 0
		for _, f := range c.findings {
			if *f.FetchKeyID == rs.ID {
				dropped++
			}
		}
		if dropped > 0 {
			s += ", " + plural(dropped, "group") + " dropped"
		}
		parts = append(parts, s)
	}
	return strings.Join(parts, "; ")
}

// plural counts n of noun: "1 group", "2 groups".
func plural(n int, noun string) string {
	if n == 1 {
		return "1 " + noun
	}
	return fmt.Sprintf("%d %ss", n, noun)
}

// writer writes the listings and identifiers of the attached groups onto
// one instrument, skipping each the instrument already has and each another
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
func (w *writer) reuse(f *found) {
	w.instrument = f.instrument
	for _, id := range f.identifiers {
		w.identifiers[to.Identifier(id)] = id
	}
	for _, l := range f.listings {
		w.listings[l.Currency] = l
	}
}

// create writes the instrument of the winner, with its class and provenance.
func (w *writer) create(ctx context.Context, winner *group) (gen.Instrument, error) {
	class := winner.class
	if class == "" {
		class = gen.AssetClassUnknown
	}
	inst, err := w.q.CreateInstrument(ctx, gen.CreateInstrumentParams{ID: db.NewID(), AssetClass: class, FetchKeyID: &winner.r.ID})
	if err != nil {
		return gen.Instrument{}, fmt.Errorf("create instrument: %w", err)
	}
	return inst, nil
}

// attach writes the listings and identifiers of g the instrument lacks,
// each with g's fetch key as provenance, and records the fetch key's
// response against the instrument.
func (w *writer) attach(ctx context.Context, g *group) error {
	fetchKey := g.r.ID
	for _, id := range g.instrument {
		if err := w.identify(ctx, id, nil, fetchKey); err != nil {
			return err
		}
	}
	for _, fam := range slices.Sorted(maps.Keys(g.listings)) {
		if fam == "" {
			continue
		}
		l, ok := w.listings[fam]
		if !ok {
			var err error
			l, err = w.q.CreateListing(ctx, gen.CreateListingParams{ID: db.NewID(), InstrumentID: w.instrument.ID, Currency: fam, FetchKeyID: &fetchKey})
			if err != nil {
				return fmt.Errorf("create listing in %s: %w", fam, err)
			}
			w.listings[fam] = l
		}
		for _, id := range g.listings[fam] {
			if err := w.identify(ctx, id, &l.ID, fetchKey); err != nil {
				return err
			}
		}
	}
	arg := gen.SetFetchKeyInstrumentParams{ID: fetchKey, UserID: w.user, InstrumentID: &w.instrument.ID}
	if err := w.q.SetFetchKeyInstrument(ctx, arg); err != nil {
		return fmt.Errorf("attach fetch key: %w", err)
	}
	for id := range g.all {
		arg := gen.CreateFetchIdentifierParams{FetchKeyID: fetchKey, Type: id.Type, Domain: id.Domain, Value: id.Value}
		if err := w.q.CreateFetchIdentifier(ctx, arg); err != nil {
			return fmt.Errorf("record fetch identifier: %w", err)
		}
	}
	return nil
}

// identify writes id on the instrument, or on listing where set, unless it
// already identifies the instrument.
func (w *writer) identify(ctx context.Context, id types.Identifier, listing *uuid.UUID, fetchKey uuid.UUID) error {
	if _, ok := w.identifiers[id]; ok || w.taken[id] {
		return nil
	}
	arg := gen.CreateIdentifierParams{ID: db.NewID(), InstrumentID: w.instrument.ID, ListingID: listing, Type: id.Type, Domain: id.Domain, Value: id.Value, FetchKeyID: &fetchKey}
	row, err := w.q.CreateIdentifier(ctx, arg)
	if err != nil {
		return fmt.Errorf("create %s: %w", name(id), err)
	}
	w.identifiers[id] = row
	return nil
}

// associate writes the association of res with the instrument, through the
// identifier via returns and on the listing of the stated family. It
// reports false when no identifier identifies the instrument.
func (w *writer) associate(ctx context.Context, res *resolution, families func(string) string) (bool, error) {
	var listingID *uuid.UUID
	if l, ok := w.listings[family(res.row, families)]; ok {
		listingID = &l.ID
	}
	via, ok := w.via(res, listingID)
	if !ok {
		return false, nil
	}
	validity := gen.ValidityProvisional
	if stable(to.Identifier(via)) {
		validity = gen.ValidityConfirmed
	}
	arg := gen.SetStatedKeyAssociationParams{ID: res.row.ID, UserID: w.user, InstrumentID: &w.instrument.ID, ListingID: listingID, ViaID: &via.ID, Validity: &validity}
	if err := w.q.SetStatedKeyAssociation(ctx, arg); err != nil {
		return false, fmt.Errorf("associate key: %w", err)
	}
	return true, nil
}

// via returns the identifier carrying res's association: the strongest it
// stated that identifies the instrument, else a venue ticker of the chosen
// listing, else a venue ticker of the instrument.
func (w *writer) via(res *resolution, listing *uuid.UUID) (gen.Identifier, bool) {
	for _, id := range res.guids {
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
