// Package attest writes and withdraws an administrator's assertions of what a
// ticker names.
//
// An attestation is an assertion whose source is an administrator rather than
// a fetch key. Datasources supply coverage, so a venue no datasource covers
// is attested ticker by ticker. How an attested assertion ranks against a
// datasource's is in [validity.go](../validity/validity.go). Writing or
// withdrawing an attestation replays the unresolved keys that state the
// ticker beneath the attestation run, and a dated key picks up the change
// when it is next read.
package attest

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

var (
	// ErrRange reports a range whose last day precedes its first.
	ErrRange = errors.New("the range ends before it starts")
	// ErrNotAttested reports a withdrawal of a datasource's assertion.
	ErrNotAttested = errors.New("the assertion is a datasource's, not an attestation")
)

// lane serialises attestation runs.
const lane = "attestation"

// Service writes and withdraws attestations.
type Service struct {
	store   Store
	runs    Runner
	replays Replays
	mics    mic.MICMap
	clock   func() time.Time
}

// New returns a Service.
func New(store Store, runs Runner, replays Replays, mics mic.MICMap, clock func() time.Time) *Service {
	return &Service{store: store, runs: runs, replays: replays, mics: mics, clock: clock}
}

// Start asserts that v names instrument, or nothing where instrument is nil,
// on from and on to where to is given, and returns the pending attestation
// run. The assertions exist when Start returns. Whether the administrator
// verified the ticker, accepts that no event occurred since its last
// assertion, or knows of an event, the row is the same. A day is today or
// any earlier day, and a range is an assertion on each end, since the
// bracket rule confirms the days between. v may name any venue of its
// domain. A to before from is refused with ErrRange.
func (s *Service) Start(ctx context.Context, admin uuid.UUID, v types.Identifier, instrument *uuid.UUID, from time.Time, to *time.Time) (gen.Run, error) {
	if op, ok := s.mics.Operating(v.Domain); ok {
		v.Domain = op
	}
	today := s.clock()
	days := []time.Time{types.Day(from)}
	if to != nil && !types.Day(*to).Equal(days[0]) {
		if to.Before(from) {
			return gen.Run{}, fmt.Errorf("%s to %s: %w", days[0].Format(time.DateOnly), types.Day(*to).Format(time.DateOnly), ErrRange)
		}
		days = append(days, types.Day(*to))
	}
	for _, d := range days {
		if err := validity.CheckAttest(s.mics, v, d, today); err != nil {
			return gen.Run{}, err
		}
	}
	if instrument != nil {
		if _, err := s.store.GetInstrument(ctx, *instrument); errors.Is(err, db.ErrNotFound) {
			return gen.Run{}, fmt.Errorf("instrument %s: %w", instrument, err)
		} else if err != nil {
			return gen.Run{}, fmt.Errorf("get instrument: %w", err)
		}
	}
	spec := run.Spec{
		Kind: gen.RunKindAttestation, Trigger: gen.RunTriggerAdministrator, Lane: lane,
		Prepare: func(ctx context.Context, row gen.Run) error {
			return s.store.Tx(ctx, func(q Queries) error {
				for _, d := range days {
					if err := validity.Attest(ctx, q, s.mics, row, admin, v, instrument, d, today); err != nil {
						return err
					}
				}
				return nil
			})
		},
	}
	return s.runs.Start(ctx, spec, func(ctx context.Context, row gen.Run) error {
		return s.replays.ReplayNewIdentifierFacts(ctx, row, []resolve.WrittenIdentifierFacts{{Run: row, Identifiers: []types.Identifier{v}}})
	})
}

// Withdraw deletes the attested assertion id and replays the unresolved keys
// that state its ticker beneath the run that wrote it, before it returns. A
// datasource's assertion is refused with ErrNotAttested.
func (s *Service) Withdraw(ctx context.Context, id uuid.UUID) error {
	a, err := s.store.GetIdentifierAssertion(ctx, id)
	if err != nil {
		return fmt.Errorf("get assertion %s: %w", id, err)
	}
	if a.RunID == nil {
		return fmt.Errorf("assertion %s: %w", id, ErrNotAttested)
	}
	row, err := s.store.GetRunByID(ctx, *a.RunID)
	if err != nil {
		return fmt.Errorf("get run %s: %w", a.RunID, err)
	}
	n, err := s.store.DeleteAttestedAssertion(ctx, id)
	if err != nil {
		return fmt.Errorf("delete assertion %s: %w", id, err)
	}
	if n == 0 {
		return fmt.Errorf("assertion %s: %w", id, db.ErrNotFound)
	}
	v := types.Identifier{Type: a.Type, Domain: a.Domain, Value: a.Value}
	return s.replays.ReplayNewIdentifierFacts(ctx, row, []resolve.WrittenIdentifierFacts{{Run: row, Identifiers: []types.Identifier{v}}})
}
