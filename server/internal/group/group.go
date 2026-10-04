// Package group gathers a user's unresolved keys into the holdings that sum
// them. Where two keys a transaction names share an identifier, they are one
// holding, transitively, named by the earliest key.
package group

import (
	"bytes"
	"context"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

// Queries is this package's view of the generated queries.
type Queries interface {
	ListGroupableKeys(ctx context.Context, userID uuid.UUID) ([]gen.StatedKey, error)
	ClearStatedKeyGroups(ctx context.Context, userID uuid.UUID) error
	SetStatedKeyGroups(ctx context.Context, arg gen.SetStatedKeyGroupsParams) error
}

var _ Queries = (*gen.Queries)(nil)

// domained holds the identifier types whose value is read with a domain. A
// test holds it equal to the identifier_type_traits table. Where a value of
// one of these types is stated without its domain, it names nothing, so two
// keys stating it are not thereby the same holding.
var domained = map[types.IdentifierType]bool{
	types.IdentifierTypeMicTicker:         true,
	types.IdentifierTypeOpenfigiTicker:    true,
	types.IdentifierTypeDatasourceTicker:  true,
	types.IdentifierTypeBrokerID:          true,
	types.IdentifierTypeBrokerDescription: true,
}

// join unions id with the first key stating k.
func join[K comparable](first map[K]uuid.UUID, parent map[uuid.UUID]uuid.UUID, k K, id uuid.UUID) {
	if held, ok := first[k]; ok {
		union(parent, held, id)
		return
	}
	first[k] = id
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

// Of partitions keys into groups and returns the group of each: the id
// of the earliest key sharing an identifier with it, however many keys the
// chain runs through.
func Of(keys []gen.StatedKey) map[uuid.UUID]uuid.UUID {
	parent := make(map[uuid.UUID]uuid.UUID, len(keys))
	for _, k := range keys {
		parent[k.ID] = k.ID
	}
	// first holds the first key seen stating each identifier.
	first := map[types.Identifier]uuid.UUID{}
	for _, k := range keys {
		for _, i := range k.Identifiers {
			if domained[i.Type] && i.Domain == "" {
				continue
			}
			join(first, parent, i, k.ID)
		}
	}
	out := make(map[uuid.UUID]uuid.UUID, len(keys))
	for _, k := range keys {
		out[k.ID] = find(parent, k.ID)
	}
	return out
}

// Regroup recomputes every group of user in full, over the unresolved keys a
// transaction names. The caller holds the user's key lock; see
// [transactions.sql](../db/queries/transactions/transactions.sql).
func Regroup(ctx context.Context, q Queries, user uuid.UUID) error {
	keys, err := q.ListGroupableKeys(ctx, user)
	if err != nil {
		return fmt.Errorf("list groupable keys: %w", err)
	}
	if err := q.ClearStatedKeyGroups(ctx, user); err != nil {
		return fmt.Errorf("clear groups: %w", err)
	}
	if len(keys) == 0 {
		return nil
	}
	of := Of(keys)
	ids, group := make([]uuid.UUID, 0, len(of)), make([]uuid.UUID, 0, len(of))
	for _, k := range keys {
		ids = append(ids, k.ID)
		group = append(group, of[k.ID])
	}
	if err := q.SetStatedKeyGroups(ctx, gen.SetStatedKeyGroupsParams{UserID: user, Ids: ids, GroupIds: group}); err != nil {
		return fmt.Errorf("set groups: %w", err)
	}
	return nil
}
