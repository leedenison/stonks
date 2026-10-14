package resolve

import (
	"context"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/types"
)

// findResolutionArbitratedKey finds, for each key the lookup left
// unassociated, the user's arbitrated key that shares a joining identifier
// with it. The write later copies the arbitrated key's association onto the
// key. The arbitrated keys must agree on one instrument. If two of them name
// different instruments, the key takes neither: a contradiction finding names
// the two, and the key goes to the datasources.
func (r *Resolver) findResolutionArbitratedKey(ctx context.Context, run gen.Run, rs []*resolution) error {
	var open []*resolution
	for _, res := range rs {
		if res.outcome == "" && res.knownInstrument == nil {
			open = append(open, res)
		}
	}
	if len(open) == 0 {
		return nil
	}
	arbitrated, err := r.store.ListUserArbitratedKeys(ctx, run.UserID)
	if err != nil {
		return fmt.Errorf("list arbitrated keys: %w", err)
	}
	if len(arbitrated) == 0 {
		return nil
	}
	byID := map[types.Identifier][]*gen.StatedKey{}
	for i := range arbitrated {
		k := &arbitrated[i]
		for _, id := range k.Identifiers {
			if id.Joining() {
				byID[id] = append(byID[id], k)
			}
		}
	}
	for _, res := range open {
		var first, other *gen.StatedKey
		for _, id := range res.row.Identifiers {
			for _, k := range byID[id] {
				switch {
				case k.ID == res.row.ID:
				case first == nil:
					first = k
				case other == nil && *k.InstrumentID != *first.InstrumentID:
					other = k
				}
			}
		}
		switch {
		case first == nil:
		case other != nil:
			detail := fmt.Sprintf("confirmed keys name different instruments: %s names %s and %s names %s", shared(res.row, *first), *first.InstrumentID, shared(res.row, *other), *other.InstrumentID)
			res.findings = append(res.findings, gen.CreateFindingParams{Kind: gen.FindingKindContradiction, Detail: ptr.To(detail)})
		default:
			res.arbitrated = first
		}
	}
	return nil
}

// shared names the first joining identifier k and arbitrated share.
func shared(k, arbitrated gen.StatedKey) string {
	for _, id := range k.Identifiers {
		for _, other := range arbitrated.Identifiers {
			if id == other && id.Joining() {
				return id.Name()
			}
		}
	}
	return arbitrated.ID.String()
}

// carry returns a as the association of k: the same instrument, via and
// validity, with the listing only where k states the currency family fam.
func carry(a gen.SetStatedKeyAssociationParams, fam string, k gen.StatedKey, cur currencies) gen.SetStatedKeyAssociationParams {
	a.ID = k.ID
	if fam == noFamily || cur.family(k) != fam {
		a.ListingID = nil
	}
	return a
}

// copyArbitratedAssociation gives a key the association of its arbitrated
// key and records the resolution as matched. The write only updates existing
// rows, so it skips the identifier lock.
func copyArbitratedAssociation(ctx context.Context, q Queries, run gen.Run, res *resolution) (gen.ResolutionKey, []gen.FindingKind, error) {
	src := res.arbitrated
	a := gen.SetStatedKeyAssociationParams{UserID: run.UserID, InstrumentID: src.InstrumentID, ListingID: src.ListingID, ViaID: src.ViaID, Validity: src.Validity, Arbiter: gen.ArbiterUser}
	arg := carry(a, res.currencies.family(*src), res.row, res.currencies)
	if _, err := q.SetStatedKeyAssociation(ctx, arg); err != nil {
		return gen.ResolutionKey{}, nil, fmt.Errorf("associate key: %w", err)
	}
	return recordResolution(ctx, q, run, res, gen.ResolutionOutcomeMatched, nil, res.findings, 0)
}
