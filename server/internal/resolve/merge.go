package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/mirror"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// hit is an instrument findInstrumentsByIdentifier found, with the
// identifiers that matched it.
type hit struct {
	stored  *stored
	matched []types.Identifier
}

// merge folds every later created instrument of hits into the earliest,
// which is returned reloaded, with a merged finding per instrument. hits is
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
func merge(ctx context.Context, q Queries, res *resolution, fetchKeyID *uuid.UUID, hits []hit) (target, error) {
	if detail, ok := mergeConflict(hits); ok {
		return refuse(res, fetchKeyID, hits, detail), nil
	}
	survivor := hits[0].stored
	if err := q.DeferConstraints(ctx); err != nil {
		return target{}, fmt.Errorf("defer constraints: %w", err)
	}
	present := map[string]bool{}
	for _, l := range survivor.listings {
		present[l.Currency] = true
	}
	var findings []gen.CreateFindingParams
	for _, h := range hits[1:] {
		finding, err := fold(ctx, q, survivor, hits[0].matched, h, present, fetchKeyID)
		if err != nil {
			return target{}, err
		}
		findings = append(findings, finding)
	}
	loaded, err := loadInstruments(ctx, q, []gen.Instrument{survivor.instrument})
	if err != nil {
		return target{}, err
	}
	return target{stored: loaded[survivor.instrument.ID], findings: findings}, nil
}

// refuse targets the instrument of hits the strongest stated identifier of
// res identifies, the earliest where none does, leaving the others apart
// with a contradiction finding carrying detail.
func refuse(res *resolution, fetchKeyID *uuid.UUID, hits []hit, detail string) target {
	pick := hits[0]
	for _, id := range res.trusted {
		if i := slices.IndexFunc(hits, func(h hit) bool { return slices.Contains(h.matched, id) }); i >= 0 {
			pick = hits[i]
			break
		}
	}
	taken := map[types.Identifier]bool{}
	for _, h := range hits {
		if h.stored != pick.stored {
			for _, id := range h.stored.identifiers {
				taken[to.Identifier(id)] = true
			}
		}
	}
	finding := gen.CreateFindingParams{Kind: gen.FindingKindContradiction, FetchKeyID: fetchKeyID, Detail: ptr.To(detail + "; not merged")}
	return target{stored: pick.stored, taken: taken, findings: []gen.CreateFindingParams{finding}}
}

// fold moves the loser h into survivor under deferred constraints and
// returns the merged finding. A
// listing of the loser in a currency family the survivor lacks moves across
// whole; one in a family the survivor has is relinked onto the survivor's
// listing and deleted.
func fold(ctx context.Context, q Queries, survivor *stored, matched []types.Identifier, h hit, present map[string]bool, fetchKeyID *uuid.UUID) (gen.CreateFindingParams, error) {
	loser := h.stored
	for _, l := range loser.listings {
		if present[l.Currency] {
			continue
		}
		if err := q.MoveListing(ctx, gen.MoveListingParams{ID: l.ID, InstrumentID: survivor.instrument.ID}); err != nil {
			return gen.CreateFindingParams{}, fmt.Errorf("move listing: %w", err)
		}
		present[l.Currency] = true
	}
	steps := []struct {
		name string
		run  func() error
	}{
		{"relink identifiers", func() error {
			return q.RelinkIdentifiers(ctx, gen.RelinkIdentifiersParams{Survivor: survivor.instrument.ID, Loser: loser.instrument.ID})
		}},
		{"relink stated keys", func() error {
			return q.RelinkStatedKeys(ctx, gen.RelinkStatedKeysParams{Survivor: survivor.instrument.ID, Loser: loser.instrument.ID})
		}},
		{"relink fetch keys", func() error {
			return q.RelinkFetchKeys(ctx, gen.RelinkFetchKeysParams{Survivor: survivor.instrument.ID, Loser: loser.instrument.ID})
		}},
		{"move coverage", func() error {
			return q.MoveIdentityCoverage(ctx, gen.MoveIdentityCoverageParams{Survivor: survivor.instrument.ID, Loser: loser.instrument.ID})
		}},
		{"delete coverage", func() error { return q.DeleteIdentityCoverage(ctx, loser.instrument.ID) }},
		{"delete listings", func() error { return q.DeleteListings(ctx, loser.instrument.ID) }},
		{"delete instrument", func() error { return q.DeleteInstrument(ctx, loser.instrument.ID) }},
	}
	for _, s := range steps {
		if err := s.run(); err != nil {
			return gen.CreateFindingParams{}, fmt.Errorf("%s: %w", s.name, err)
		}
	}
	detail := fmt.Sprintf("merged the instrument created %s into the instrument created %s; the response identified the first by %s and the second by %s",
		stamp(loser.instrument.CreatedAt), stamp(survivor.instrument.CreatedAt), names(h.matched), names(matched))
	return gen.CreateFindingParams{Kind: gen.FindingKindMerged, FetchKeyID: fetchKeyID, Detail: ptr.To(detail)}, nil
}

func stamp(t time.Time) string { return t.UTC().Format("2006-01-02 15:04:05 UTC") }

// names writes ids as a list.
func names(ids []types.Identifier) string {
	out := make([]string, len(ids))
	for i, id := range ids {
		out[i] = name(id)
	}
	return strings.Join(out, ", ")
}

// mergeConflict returns why the instruments of hits cannot be one, if they
// cannot: two carry identifiers of one type and domain with different
// values naming one subject, the instrument or a listing of one currency
// family, or disjoint classes.
func mergeConflict(hits []hit) (string, bool) {
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
	for _, h := range hits {
		famOf := map[uuid.UUID]string{}
		for _, l := range h.stored.listings {
			famOf[l.ID] = l.Currency
		}
		for _, row := range h.stored.identifiers {
			id := to.Identifier(row)
			if !exclusive(id) {
				continue
			}
			k := key{typ: id.Type, domain: id.Domain}
			if row.ListingID != nil {
				k.family = famOf[*row.ListingID]
			}
			if prior, ok := first[k]; ok && prior.id.Value != id.Value && prior.instrument != row.InstrumentID {
				return fmt.Sprintf("%s identifies instrument %s and %s identifies %s", name(prior.id), prior.instrument, name(id), row.InstrumentID), true
			}
			if _, ok := first[k]; !ok {
				first[k] = seen{id, row.InstrumentID}
			}
		}
	}
	for i, a := range hits {
		for _, b := range hits[i+1:] {
			if mirror.Disjoint(a.stored.instrument.AssetClass, b.stored.instrument.AssetClass) {
				return fmt.Sprintf("class %s of instrument %s contradicts class %s of %s", a.stored.instrument.AssetClass, a.stored.instrument.ID, b.stored.instrument.AssetClass, b.stored.instrument.ID), true
			}
		}
	}
	return "", false
}
