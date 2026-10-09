package resolve

import (
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	keygroup "github.com/leedenison/stonks/server/internal/group"
	"github.com/leedenison/stonks/server/internal/ptr"
)

// inherit gives a key the association of the user's arbitrated keys when
// the lookup left the key unassociated and the key shares a joining
// identifier with them. Those keys must name one instrument. If they name
// two, the key inherits only when they agree, a contradiction finding names
// the two, and the key goes to the datasources.
func (r *Resolver) inherit(ctx context.Context, run gen.Run, rs []*resolution) error {
	var open []*resolution
	for _, res := range rs {
		if res.outcome == "" && res.found == nil {
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
			if keygroup.Joining(id) {
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
			res.inherit = first
		}
	}
	return nil
}

// shared names the first joining identifier k and arbitrated share.
func shared(k, arbitrated gen.StatedKey) string {
	for _, id := range k.Identifiers {
		for _, other := range arbitrated.Identifiers {
			if id == other && keygroup.Joining(id) {
				return name(id)
			}
		}
	}
	return arbitrated.ID.String()
}

// inheritKey writes the association res inherits, and a matched resolution
// key. The listing carries over where the key states the same currency
// family. The write only updates existing rows, so it skips the identifier
// lock.
func inheritKey(ctx context.Context, q Queries, run gen.Run, res *resolution, cur currencies) (gen.ResolutionKey, []gen.FindingKind, error) {
	src := res.inherit
	var listing *uuid.UUID
	if res.fam != noFamily && res.fam == cur.family(*src) {
		listing = src.ListingID
	}
	arg := gen.SetStatedKeyAssociationParams{ID: res.row.ID, UserID: run.UserID, InstrumentID: src.InstrumentID, ListingID: listing, ViaID: src.ViaID, Validity: src.Validity, Arbiter: gen.ArbiterUser}
	if _, err := q.SetStatedKeyAssociation(ctx, arg); err != nil {
		return gen.ResolutionKey{}, nil, fmt.Errorf("associate key: %w", err)
	}
	return record(ctx, q, run, res, gen.ResolutionOutcomeMatched, nil, res.findings)
}
