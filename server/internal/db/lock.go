package db

import (
	"context"
	"fmt"
	"slices"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/types"
)

// AdvisoryLocker takes named advisory locks for the transaction. The helpers
// below build every name, each with a prefix of its own, so two kinds of lock
// never share a name.
type AdvisoryLocker interface {
	LockAdvisory(ctx context.Context, names []string) error
}

// LockIdentifiers takes the transaction's advisory lock on each of ids, in one
// order whatever the caller's, so two writers of one identifier serialise.
func LockIdentifiers(ctx context.Context, q AdvisoryLocker, ids []types.Identifier) error {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = fmt.Sprintf("identifier:%s:%s:%s", id.Type, id.Domain, id.Value)
	}
	if err := lockNames(ctx, q, names); err != nil {
		return fmt.Errorf("lock identifiers: %w", err)
	}
	return nil
}

// LockInstrs takes the transaction's advisory lock on each of ids, in one
// order whatever the caller's. A rebuild of the ticker validity and names of
// an instrument takes it, so two rebuilds of one instrument serialise.
func LockInstrs(ctx context.Context, q AdvisoryLocker, ids []uuid.UUID) error {
	names := make([]string, len(ids))
	for i, id := range ids {
		names[i] = "instr:" + id.String()
	}
	if err := lockNames(ctx, q, names); err != nil {
		return fmt.Errorf("lock instruments: %w", err)
	}
	return nil
}

// LockUserKeys takes the user's key lock for the transaction. It serialises
// the writes to one user's groups. A regroup takes it. A resolution
// associates a single key without it.
func LockUserKeys(ctx context.Context, q AdvisoryLocker, user uuid.UUID) error {
	if err := lockNames(ctx, q, []string{"user_keys:" + user.String()}); err != nil {
		return fmt.Errorf("lock keys: %w", err)
	}
	return nil
}

// LockCorpActAnswer takes the transaction's advisory lock on the answer of
// source for ticker. It serialises the writes of one datasource's answer for
// one ticker.
func LockCorpActAnswer(ctx context.Context, q AdvisoryLocker, source string, ticker uuid.UUID) error {
	if err := lockNames(ctx, q, []string{"corpact:" + source + ":" + ticker.String()}); err != nil {
		return fmt.Errorf("lock corporate action answer: %w", err)
	}
	return nil
}

func lockNames(ctx context.Context, q AdvisoryLocker, names []string) error {
	if len(names) == 0 {
		return nil
	}
	names = slices.Clone(names)
	slices.Sort(names)
	return q.LockAdvisory(ctx, slices.Compact(names))
}
