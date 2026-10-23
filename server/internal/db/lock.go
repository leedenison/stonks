package db

import (
	"context"
	"fmt"
	"slices"

	"github.com/leedenison/stonks/server/internal/types"
)

// IdentifierLocker takes advisory locks keyed on identifiers.
type IdentifierLocker interface {
	LockIdentifiers(ctx context.Context, keys []string) error
}

// LockIdentifiers takes the transaction's advisory lock on each of ids, in one
// order whatever the caller's, so two writers of one identifier serialise.
func LockIdentifiers(ctx context.Context, q IdentifierLocker, ids []types.Identifier) error {
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
