package statement

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/resolve"
)

// ErrAssociated is returned by Candidates and Confirm for a key that
// already has an association.
var ErrAssociated = errors.New("the key has an association")

// Candidates lists the candidates for one of user's keys, as a synchronous
// resolution run. A key of another user is not found.
func (s *Service) Candidates(ctx context.Context, user, key uuid.UUID) (resolve.Offer, error) {
	row, err := s.key(ctx, user, key)
	if err != nil {
		return resolve.Offer{}, err
	}
	var offer resolve.Offer
	_, err = s.runs.Sync(ctx, user, gen.RunKindResolution, gen.RunTriggerUser, func(ctx context.Context, run gen.Run) error {
		var err error
		offer, err = s.resolver.Candidates(ctx, run, row)
		return err
	})
	return offer, err
}

// Confirm takes the candidate p names as the instrument of one of user's
// keys, as a synchronous resolution run, and returns the key's resolution.
func (s *Service) Confirm(ctx context.Context, user, key uuid.UUID, p resolve.Pick) (gen.ResolutionKey, error) {
	row, err := s.key(ctx, user, key)
	if err != nil {
		return gen.ResolutionKey{}, err
	}
	var rk gen.ResolutionKey
	_, err = s.runs.Sync(ctx, user, gen.RunKindResolution, gen.RunTriggerUser, func(ctx context.Context, run gen.Run) error {
		var err error
		rk, err = s.resolver.Confirm(ctx, run, row, p)
		return err
	})
	return rk, err
}

// key reads one of user's keys that has no association.
func (s *Service) key(ctx context.Context, user, id uuid.UUID) (gen.StatedKey, error) {
	row, err := s.store.GetStatedKey(ctx, gen.GetStatedKeyParams{ID: id, UserID: user})
	if err != nil {
		return gen.StatedKey{}, fmt.Errorf("read key %s: %w", id, err)
	}
	if row.InstrumentID != nil {
		return gen.StatedKey{}, ErrAssociated
	}
	return row, nil
}
