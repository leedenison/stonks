package statement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/resolve"
)

// ErrAssociated is returned by Candidates and Confirm for a key that
// already has an association.
var ErrAssociated = errors.New("the key has an association")

// Candidates lists the candidates for one of user's keys, as a synchronous
// resolve statement keys run. A key of another user is not found.
func (s *Service) Candidates(ctx context.Context, user, key uuid.UUID) (resolve.Offer, error) {
	row, err := s.key(ctx, user, key)
	if err != nil {
		return resolve.Offer{}, err
	}
	var offer resolve.Offer
	_, err = s.runs.Sync(ctx, user, gen.RunKindResolveStmtKeys, gen.RunTriggerUser, func(ctx context.Context, run gen.Run) error {
		var err error
		offer, err = s.resolver.Candidates(ctx, run, row)
		return err
	})
	return offer, err
}

// Confirm takes the candidate p names as the instrument of one of user's keys,
// as a synchronous resolve statement keys run, and returns the key's
// resolution. It then follows the confirmation's new identifier facts, which
// replay every user's unresolved keys that state one of their identifiers. A
// failed replay is logged, and the confirmation stands.
func (s *Service) Confirm(ctx context.Context, user, key uuid.UUID, p resolve.Pick) (gen.ResolutionKey, error) {
	row, err := s.key(ctx, user, key)
	if err != nil {
		return gen.ResolutionKey{}, err
	}
	var rk gen.ResolutionKey
	var written []resolve.WrittenIdentifierFacts
	run, err := s.runs.Sync(ctx, user, gen.RunKindResolveStmtKeys, gen.RunTriggerUser, func(ctx context.Context, run gen.Run) error {
		var err error
		rk, written, err = s.resolver.Confirm(ctx, run, row, p)
		return err
	})
	if err != nil {
		return rk, err
	}
	s.facts.AfterIdentifierFacts(ctx, run, written)
	return rk, nil
}

// key reads one of user's keys that has no association. A dated key refuses
// a choice as a fixed one does: the facts decide what each of its
// transactions names.
func (s *Service) key(ctx context.Context, user, id uuid.UUID) (gen.StatedKey, error) {
	row, err := s.store.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: id, UserID: user})
	if err != nil {
		return gen.StatedKey{}, fmt.Errorf("read key %s: %w", id, err)
	}
	if row.InstrumentID != nil || row.ViaType != nil {
		return gen.StatedKey{}, ErrAssociated
	}
	return row, nil
}
