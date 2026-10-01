package resolve

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/to"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// hit is an instrument the re-read found, with the identifiers that matched
// it.
type hit struct {
	found   *found
	matched []types.Identifier
}

// merge folds every later created instrument of hits into the earliest,
// which is returned reloaded, with a merged finding per instrument.
// fetchKey is the response that brought the instruments together.
//
// Where two of the instruments carry identifiers of one type and domain with
// different values, or disjoint classes, nothing merges: the instrument the
// strongest stated identifier identifies is returned with a contradiction
// finding.
//
// Another user's keys are relinked without that user's key lock, so one
// write of theirs may see the instrument it folded.
func merge(ctx context.Context, q Queries, res *resolution, fetchKey *uuid.UUID, hits []hit) (*found, map[types.Identifier]bool, []gen.CreateFindingParams, error) {
	slices.SortFunc(hits, func(a, b hit) int {
		return a.found.instrument.CreatedAt.Compare(b.found.instrument.CreatedAt)
	})
	if detail, ok := disagree(hits); ok {
		f, taken, finding := refuse(res, fetchKey, hits, detail)
		return f, taken, []gen.CreateFindingParams{finding}, nil
	}
	survivor := hits[0].found
	if err := q.DeferConstraints(ctx); err != nil {
		return nil, nil, nil, fmt.Errorf("defer constraints: %w", err)
	}
	families := map[string]bool{}
	for _, l := range survivor.listings {
		families[l.Currency] = true
	}
	var findings []gen.CreateFindingParams
	for _, h := range hits[1:] {
		finding, err := fold(ctx, q, survivor, h, families, fetchKey)
		if err != nil {
			return nil, nil, nil, err
		}
		findings = append(findings, finding)
	}
	f, err := load(ctx, q, survivor.instrument)
	return f, nil, findings, err
}

// refuse picks the instrument of hits the strongest stated identifier of
// res identifies, the earliest where none does, and returns it with the
// identifiers of the others, which the write leaves alone, and a
// contradiction finding carrying detail.
func refuse(res *resolution, fetchKey *uuid.UUID, hits []hit, detail string) (*found, map[types.Identifier]bool, gen.CreateFindingParams) {
	pick := hits[0]
	for _, id := range res.guids {
		if i := slices.IndexFunc(hits, func(h hit) bool { return slices.Contains(h.matched, id) }); i >= 0 {
			pick = hits[i]
			break
		}
	}
	taken := map[types.Identifier]bool{}
	for _, h := range hits {
		if h.found != pick.found {
			for _, id := range h.found.identifiers {
				taken[to.Identifier(id)] = true
			}
		}
	}
	finding := gen.CreateFindingParams{Kind: gen.FindingKindContradiction, FetchKeyID: fetchKey, Detail: ptr.To(detail + "; not merged")}
	return pick.found, taken, finding
}

// fold moves the loser h into survivor under deferred constraints and
// returns the merged finding. families is the currency families where
// survivor has a listing; a listing of the loser in another moves across
// and joins it, the rest are relinked onto the survivor's and deleted.
func fold(ctx context.Context, q Queries, survivor *found, h hit, families map[string]bool, fetchKey *uuid.UUID) (gen.CreateFindingParams, error) {
	loser := h.found
	for _, l := range loser.listings {
		if families[l.Currency] {
			continue
		}
		if err := q.MoveListing(ctx, gen.MoveListingParams{ID: l.ID, InstrumentID: survivor.instrument.ID}); err != nil {
			return gen.CreateFindingParams{}, fmt.Errorf("move listing: %w", err)
		}
		families[l.Currency] = true
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
	matched := make([]string, len(h.matched))
	for i, id := range h.matched {
		matched[i] = name(id)
	}
	detail := fmt.Sprintf("folded instrument %s into %s, both identified: %s", loser.instrument.ID, survivor.instrument.ID, strings.Join(matched, ", "))
	return gen.CreateFindingParams{Kind: gen.FindingKindMerged, FetchKeyID: fetchKey, Detail: ptr.To(detail)}, nil
}

// disagree returns why the instruments of hits cannot be one, if they
// cannot: two carry identifiers of one type and domain with different
// values, or disjoint classes.
func disagree(hits []hit) (string, bool) {
	type key struct {
		typ    types.IdentifierType
		domain string
	}
	type seen struct {
		id         types.Identifier
		instrument uuid.UUID
	}
	first := map[key]seen{}
	for _, h := range hits {
		for _, row := range h.found.identifiers {
			id := to.Identifier(row)
			if multi[id.Type] {
				continue
			}
			k := key{id.Type, id.Domain}
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
			if Disjoint(a.found.instrument.AssetClass, b.found.instrument.AssetClass) {
				return fmt.Sprintf("class %s of instrument %s contradicts class %s of %s", a.found.instrument.AssetClass, a.found.instrument.ID, b.found.instrument.AssetClass, b.found.instrument.ID), true
			}
		}
	}
	return "", false
}
