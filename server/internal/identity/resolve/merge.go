package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/join"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// merge folds every later created instrument of matches into the earliest,
// which is returned reloaded, with a merged finding per instrument. matches is
// sorted earliest created first. fetchKeyID names the fetch key whose
// response brought the instruments together.
//
// Where mergeConflict finds the instruments cannot be one, nothing merges: the
// instrument the strongest stated identifier identifies is returned with a
// contradiction finding.
//
// The merge relinks another user's keys without holding that user's key
// lock. That user's concurrent write may therefore see an instrument the
// merge has since folded.
func merge(ctx context.Context, q Queries, res *resolution, fetchKeyID *uuid.UUID, matches []instrumentMatch) (writeTarget, error) {
	if detail, ok := mergeConflict(matches); ok {
		return refuse(res, fetchKeyID, matches, detail), nil
	}
	survivor := matches[0].instrument
	if err := q.DeferConstraints(ctx); err != nil {
		return writeTarget{}, fmt.Errorf("defer constraints: %w", err)
	}
	present := map[string]bool{}
	for _, l := range survivor.Listings {
		present[l.Currency] = true
	}
	var findings []gen.CreateFindingParams
	for _, h := range matches[1:] {
		finding, err := fold(ctx, q, survivor, matches[0].matched, h, present, fetchKeyID)
		if err != nil {
			return writeTarget{}, err
		}
		findings = append(findings, finding)
	}
	loaded, err := join.InstrumentsWithCoverage(ctx, q, []gen.Instr{survivor.Row})
	if err != nil {
		return writeTarget{}, err
	}
	return writeTarget{instrument: loaded[survivor.Row.ID], findings: findings}, nil
}

// refuse keeps the instrument, among matches, that the strongest stated
// identifier of res identifies, or the earliest where none does. It leaves
// the others apart with a contradiction finding that carries detail.
func refuse(res *resolution, fetchKeyID *uuid.UUID, matches []instrumentMatch, detail string) writeTarget {
	kept := matches[0]
	for _, id := range lookupIDs(res.globalAndDescriptionIDs) {
		if i := slices.IndexFunc(matches, func(h instrumentMatch) bool { return slices.Contains(h.matched, id) }); i >= 0 {
			kept = matches[i]
			break
		}
	}
	taken := map[types.Identifier]bool{}
	for _, h := range matches {
		if h.instrument != kept.instrument {
			for _, id := range h.instrument.Identifiers {
				taken[to.Identifier(id)] = true
			}
		}
	}
	finding := gen.CreateFindingParams{Kind: gen.FindingKindContradiction, FetchKeyID: fetchKeyID, Detail: ptr.To(detail + "; not merged")}
	return writeTarget{instrument: kept.instrument, taken: taken, findings: []gen.CreateFindingParams{finding}}
}

// fold moves the loser h into survivor under deferred constraints and
// returns the merged finding. A
// listing of the loser in a currency family the survivor lacks moves across
// whole; one in a family the survivor has is relinked onto the survivor's
// listing and deleted.
func fold(ctx context.Context, q Queries, survivor *join.InstrumentWithCoverage, matched []types.Identifier, h instrumentMatch, present map[string]bool, fetchKeyID *uuid.UUID) (gen.CreateFindingParams, error) {
	loser := h.instrument
	for _, l := range loser.Listings {
		if present[l.Currency] {
			continue
		}
		if err := q.MoveListing(ctx, gen.MoveListingParams{ID: l.ID, InstrID: survivor.Row.ID}); err != nil {
			return gen.CreateFindingParams{}, fmt.Errorf("move listing: %w", err)
		}
		present[l.Currency] = true
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"relink identifiers", func() error {
			return q.RelinkIdentifiers(ctx, gen.RelinkIdentifiersParams{Survivor: survivor.Row.ID, Loser: loser.Row.ID})
		}},
		{"relink stated keys", func() error {
			return q.RelinkStatedKeys(ctx, gen.RelinkStatedKeysParams{Survivor: survivor.Row.ID, Loser: loser.Row.ID})
		}},
		{"relink fetch keys", func() error {
			return q.RelinkFetchKeys(ctx, gen.RelinkFetchKeysParams{Survivor: survivor.Row.ID, Loser: loser.Row.ID})
		}},
		{"relink assertions", func() error {
			return q.RelinkIdentifierAssertions(ctx, gen.RelinkIdentifierAssertionsParams{Survivor: survivor.Row.ID, Loser: loser.Row.ID})
		}},
		{"move coverage", func() error {
			return q.MoveIdentityCoverage(ctx, gen.MoveIdentityCoverageParams{Survivor: survivor.Row.ID, Loser: loser.Row.ID})
		}},
		{"delete coverage", func() error { return q.DeleteIdentityCoverage(ctx, loser.Row.ID) }},
		{"delete listings", func() error { return q.DeleteListings(ctx, loser.Row.ID) }},
		{"delete instrument", func() error { return q.DeleteInstr(ctx, loser.Row.ID) }},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			return gen.CreateFindingParams{}, fmt.Errorf("%s: %w", s.name, err)
		}
	}
	detail := fmt.Sprintf("merged the instrument created %s into the instrument created %s; the response identified the first by %s and the second by %s",
		stamp(loser.Row.CreatedAt), stamp(survivor.Row.CreatedAt), names(h.matched), names(matched))
	return gen.CreateFindingParams{Kind: gen.FindingKindMerged, FetchKeyID: fetchKeyID, Detail: ptr.To(detail)}, nil
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }

// names writes ids as a list.
func names(ids []types.Identifier) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = id.Name()
	}
	return strings.Join(out, ", ")
}

// mergeConflict returns why the instruments of matches cannot be one, if they
// cannot: two carry identifiers of one type and domain with different
// values on the instrument or on a listing of one currency family, or their
// classes are disjoint.
func mergeConflict(matches []instrumentMatch) (string, bool) {
	type key struct {
		typ    types.IdentifierType
		domain string
		family string
	}
	type seen struct {
		id         types.Identifier
		instrument uuid.UUID
	}
	first := map[key]seen{}
	for _, h := range matches {
		famOf := map[uuid.UUID]string{}
		for _, l := range h.instrument.Listings {
			famOf[l.ID] = l.Currency
		}
		for _, row := range h.instrument.Identifiers {
			id := to.Identifier(row)
			if !id.Exclusive() {
				continue
			}
			k := key{typ: id.Type, domain: id.Domain}
			if row.ListingID != nil {
				k.family = famOf[*row.ListingID]
			}
			if prior, ok := first[k]; ok && prior.id.Value != id.Value && prior.instrument != row.InstrID {
				return fmt.Sprintf("%s identifies instrument %s and %s identifies %s", prior.id.Name(), prior.instrument, id.Name(), row.InstrID), true
			}
			if _, ok := first[k]; !ok {
				first[k] = seen{id, row.InstrID}
			}
		}
	}
	for i, a := range matches {
		for _, b := range matches[i+1:] {
			if a.instrument.Row.AssetClass.Disjoint(b.instrument.Row.AssetClass) {
				return fmt.Sprintf("class %s of instrument %s contradicts class %s of %s", a.instrument.Row.AssetClass, a.instrument.Row.ID, b.instrument.Row.AssetClass, b.instrument.Row.ID), true
			}
		}
	}
	return "", false
}
