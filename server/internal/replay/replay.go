// Package replay re-resolves the keys of one run, so a later answer moves each
// key's association, its group is recomputed and the holdings derived from
// its transactions follow.
//
// A replay is a run of kind replay over the keys of a source run: a statement
// run's stated keys, or a resolution run's resolved keys. A replay run's keys
// are reached through its resolution child, and any other kind is refused.
// The replay belongs to the source's user, with trigger administrator, and
// its replays row names the source, the scope and the administrator. It runs
// in a lane of its own under that user, beside the user's uploads. Order
// against an upload does not matter: the resolver serialises creation on the
// stated identifiers and retries on conflict, and the regroup is a full
// recompute under the user key lock, which the statement write holds
// throughout; see [resolve.go](../resolve/resolve.go) and
// [group.go](../group/group.go).
//
// Selection. The set is fixed when the replay starts and confined to the
// source's keys that a transaction names. Unavailable selects the keys whose
// latest resolution, over any run, left them unavailable. A datasource
// selects the keys that datasource serves and has not yet answered: the
// unresolved keys, and the keys on an instrument without its identity
// coverage, reference data excluded. A replay asks every enabled datasource,
// as a fresh resolution does; the datasource scope only picks the keys. A key
// resolved while no datasource was enabled is unrecognised, so a datasource
// scope is how it is reached. An empty selection is refused and no run is
// created.
//
// The work is one resolution child over every selected key, then one regroup
// of the user. A partial replay, failed or interrupted, leaves each key the
// resolution wrote re-resolved and the groups as they were, less any key that
// became associated.
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

// lane serialises a user's replays.
const lane = "replay"

// Scope selects the keys of a replay: those the named datasource serves and
// has not yet answered, or when Datasource is empty, those left unavailable.
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

// Start selects the keys scope names among source's and starts their replay
// as a run of source's user started by admin, answering the pending row.
func (s *Service) Start(ctx context.Context, admin uuid.UUID, source gen.Run, scope Scope) (gen.Run, error) {
	keys, err := s.selectKeys(ctx, source, scope)
	if err != nil {
		return gen.Run{}, err
	}
	w := &replay{store: s.store, runs: s.runs, resolver: s.resolver, admin: admin, source: source, scope: scope, keys: keys}
	spec := run.Spec{Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator, UserID: source.UserID, Lane: lane, Prepare: w.prepare}
	return s.runs.Start(ctx, spec, w.work)
}

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

// sourceKeys reads the keys of source by its kind.
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

// work resolves the keys as a child run, then regroups the user.
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
