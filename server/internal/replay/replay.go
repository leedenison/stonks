// Package replay re-resolves the keys of one run, the source, as a run of
// the source's user. Its order against that user's uploads does not matter;
// see [resolve.go](../resolve/resolve.go) and [group.go](../group/group.go).
package replay

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/group"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/run"
)

var (
	// ErrEmpty is returned by Start when the scope selects no key.
	ErrEmpty = errors.New("no key to replay")
	// ErrDisabled is returned by Start when the scope names a datasource
	// that is not enabled.
	ErrDisabled = errors.New("the datasource is not enabled")
	// ErrKind is returned by Start when the source is neither a statement
	// nor a resolution run.
	ErrKind = errors.New("the run has no keys to replay")
)

// lane serialises a user's replays, apart from the user's uploads.
const lane = "replay"

// Scope selects the keys of a replay, among the source's keys that a
// transaction names. An empty Datasource selects the keys whose latest
// resolution, over any run, left them unavailable. A named datasource
// selects the keys it serves and has not yet answered: the unresolved keys,
// and the keys on an instrument whose identity it has not covered, reference
// data excluded. A key resolved while no datasource was enabled is
// unrecognised, so a datasource scope is how it is reached. The scope only
// picks the keys; the replay asks every enabled datasource, as a fresh
// resolution does.
type Scope struct {
	Datasource string
}

// Service starts replays.
type Service struct {
	store    Store
	runs     Runner
	resolver Resolver
	sources  Sources
}

// New returns a Service.
func New(store Store, runs Runner, resolver Resolver, sources Sources) *Service {
	return &Service{store: store, runs: runs, resolver: resolver, sources: sources}
}

// Start starts a replay of source's keys as a run of source's user, started
// by admin, and returns the pending row.
func (s *Service) Start(ctx context.Context, admin uuid.UUID, source gen.Run, scope Scope) (gen.Run, error) {
	keys, err := s.selectKeys(ctx, source, scope)
	if err != nil {
		return gen.Run{}, err
	}
	w := &replay{store: s.store, runs: s.runs, resolver: s.resolver, admin: admin, source: source, scope: scope, keys: keys}
	spec := run.Spec{Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator, UserID: source.UserID, Lane: lane, Prepare: w.prepare}
	return s.runs.Start(ctx, spec, w.work)
}

// selectKeys picks the keys once, when the replay starts. A key that becomes
// eligible later waits for another replay.
func (s *Service) selectKeys(ctx context.Context, source gen.Run, scope Scope) ([]gen.StatedKey, error) {
	keys, err := s.sourceKeys(ctx, source)
	if err != nil {
		return nil, err
	}
	ids := make([]uuid.UUID, 0, len(keys))
	for _, k := range keys {
		ids = append(ids, k.ID)
	}
	var out []gen.StatedKey
	if scope.Datasource == "" {
		rows, err := s.store.ListUnavailableKeys(ctx, ids)
		if err != nil {
			return nil, fmt.Errorf("list unavailable keys: %w", err)
		}
		for _, r := range rows {
			out = append(out, r.StatedKey)
		}
	} else {
		entry := s.entry(scope.Datasource)
		if entry == nil {
			return nil, ErrDisabled
		}
		rows, err := s.store.ListKeysUncoveredBy(ctx, gen.ListKeysUncoveredByParams{Ids: ids, Datasource: scope.Datasource})
		if err != nil {
			return nil, fmt.Errorf("list uncovered keys: %w", err)
		}
		for _, r := range rows {
			if entry.Identity == nil {
				break
			}
			if _, err := entry.Identity.Serves(r.StatedKey); err == nil {
				out = append(out, r.StatedKey)
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrEmpty
	}
	return out, nil
}

// sourceKeys reads the keys of source by its kind: a statement run's stated
// keys, or a resolution run's resolved keys. A replay run's keys are reached
// through its resolution child, and any other kind is refused.
func (s *Service) sourceKeys(ctx context.Context, source gen.Run) ([]gen.StatedKey, error) {
	switch source.Kind {
	case gen.RunKindStatement:
		keys, err := s.store.ListStatedKeys(ctx, gen.ListStatedKeysParams{StatementID: source.ID, UserID: source.UserID})
		if err != nil {
			return nil, fmt.Errorf("list stated keys: %w", err)
		}
		return keys, nil
	case gen.RunKindResolution:
		rows, err := s.store.ListResolvedKeys(ctx, gen.ListResolvedKeysParams{RunID: source.ID, UserID: source.UserID})
		if err != nil {
			return nil, fmt.Errorf("list resolved keys: %w", err)
		}
		keys := make([]gen.StatedKey, 0, len(rows))
		for _, r := range rows {
			keys = append(keys, r.StatedKey)
		}
		return keys, nil
	}
	return nil, ErrKind
}

func (s *Service) entry(name string) *market.Entry {
	for _, e := range s.sources.Enabled() {
		if e.Name == name {
			return e
		}
	}
	return nil
}

// replay is one replay's work.
type replay struct {
	store    Store
	runs     Runner
	resolver Resolver
	admin    uuid.UUID
	source   gen.Run
	scope    Scope
	keys     []gen.StatedKey
}

// prepare writes the replays row.
func (r *replay) prepare(ctx context.Context, run gen.Run) error {
	arg := gen.CreateReplayParams{ID: run.ID, UserID: run.UserID, SourceID: r.source.ID, StartedBy: r.admin}
	if r.scope.Datasource != "" {
		arg.Datasource = &r.scope.Datasource
	}
	if err := r.store.CreateReplay(ctx, arg); err != nil {
		return fmt.Errorf("create replay: %w", err)
	}
	return nil
}

// A partial replay, failed or interrupted, leaves each key the resolution
// wrote re-resolved and the groups as they were, less any key that became
// associated.
func (r *replay) work(ctx context.Context, run gen.Run) error {
	if _, err := r.runs.Child(ctx, run, gen.RunKindResolution, r.resolve); err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	return r.store.Tx(ctx, func(q Queries) error {
		if err := q.LockUserKeys(ctx, run.UserID); err != nil {
			return fmt.Errorf("lock keys: %w", err)
		}
		return group.Regroup(ctx, q, run.UserID)
	})
}

func (r *replay) resolve(ctx context.Context, res gen.Run) error {
	_, err := r.resolver.Resolve(ctx, res, r.keys)
	return err
}
