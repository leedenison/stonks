package statement

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// domained holds the identifier types whose value is read with a domain. A
// test holds it equal to the identifier_type_traits table. A value of one of
// these types stated without its domain names nothing, so two keys stating
// it are not thereby the same holding.
var domained = map[gen.IdentifierType]bool{
	gen.IdentifierTypeMicTicker:        true,
	gen.IdentifierTypeOpenfigiTicker:   true,
	gen.IdentifierTypeDatasourceTicker: true,
	gen.IdentifierTypeBrokerID:         true,
}

// find returns the root of k's component, compressing the path it walks.
func find(parent map[uuid.UUID]uuid.UUID, k uuid.UUID) uuid.UUID {
	for parent[k] != k {
		parent[k] = parent[parent[k]]
		k = parent[k]
	}
	return k
}

// union joins the components of a and b, keeping the smaller root. A version
// 7 id orders by the time it was minted, so the smaller root is the earliest
// key of the two components.
func union(parent map[uuid.UUID]uuid.UUID, a, b uuid.UUID) {
	ra, rb := find(parent, a), find(parent, b)
	if ra == rb {
		return
	}
	if bytes.Compare(ra[:], rb[:]) > 0 {
		ra, rb = rb, ra
	}
	parent[rb] = ra
}

// groups partitions keys into groups and returns the group of each: the id
// of the earliest key it shares an identifier or a description with, however
// many keys the chain runs through.
func groups(keys []gen.ListGroupableKeysRow) map[uuid.UUID]uuid.UUID {
	parent := make(map[uuid.UUID]uuid.UUID, len(keys))
	for _, r := range keys {
		parent[r.StatedKey.ID] = r.StatedKey.ID
	}
	// first holds, per thing a key can state, the first key seen stating it.
	first := map[string]uuid.UUID{}
	join := func(label string, id uuid.UUID) {
		if held, ok := first[label]; ok {
			union(parent, held, id)
			return
		}
		first[label] = id
	}
	for _, r := range keys {
		k := r.StatedKey
		for _, i := range k.Identifiers {
			if domained[gen.IdentifierType(i.Type)] && i.Domain == "" {
				continue
			}
			join(fmt.Sprintf("i\x00%s\x00%s\x00%s", i.Type, i.Domain, i.Value), k.ID)
		}
		if k.Description != nil {
			join(fmt.Sprintf("d\x00%s\x00%s", r.Broker, *k.Description), k.ID)
		}
	}
	out := make(map[uuid.UUID]uuid.UUID, len(keys))
	for _, r := range keys {
		out[r.StatedKey.ID] = find(parent, r.StatedKey.ID)
	}
	return out
}

// regroup recomputes every group of the user, over the unresolved keys a
// transaction names.
func (g *ingestion) regroup(ctx context.Context, q Queries) error {
	keys, err := q.ListGroupableKeys(ctx, g.user)
	if err != nil {
		return fmt.Errorf("list groupable keys: %w", err)
	}
	if err := q.ClearStatedKeyGroups(ctx, g.user); err != nil {
		return fmt.Errorf("clear groups: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	of := groups(keys)
	ids, group := make([]uuid.UUID, 0, len(of)), make([]uuid.UUID, 0, len(of))
	for _, r := range keys {
		ids = append(ids, r.StatedKey.ID)
		group = append(group, of[r.StatedKey.ID])
	}
	if err := q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: g.user, Ids: ids, GroupIds: group}); err != nil {
		return fmt.Errorf("set groups: %w", err)
	}
	return nil
}
