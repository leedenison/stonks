// Package replay re-resolves stated keys as a run of their user.
//
// An administrator replays the keys of one run, the source, that a scope
// selects. Its order against that user's uploads does not matter; see
// [resolve.go](../resolve/resolve.go) and [group.go](../group/group.go). A
// key whose association the user arbitrated is never selected, so a replay
// never changes it.
//
// A new identifier fact, or a withdrawn attestation, replays the unresolved
// keys that state its ticker beneath the run that wrote it; see
// ReplayNewIdentifierFacts. A key with an association is never replayed for a
// fact. A dated key takes the
// instrument its ticker names on each transaction's date when it is read, so
// a new fact reaches its transactions without a replay.
package replay

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/group"
	"github.com/leedenison/stonks/server/internal/market"
	"github.com/leedenison/stonks/server/internal/mic"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

var (
	// ErrEmpty is returned by Start when the scope selects no key.
	ErrEmpty = errors.New("no key to replay")
	// ErrDisabled is returned by Start when the scope names a datasource
	// that is not enabled.
	ErrDisabled = errors.New("the datasource is not enabled")
	// ErrNoIdentity is returned by Start when the scope names a datasource
	// that serves no identity.
	ErrNoIdentity = errors.New("the datasource serves no identity")
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
// unrecognised, so a datasource scope is how it is reached. No scope selects
// a key that states only a broker description.
// The scope only picks the keys; the replay asks every enabled datasource,
// as a fresh resolution does.
type Scope struct {
	Datasource string
}

// Service starts replays.
type Service struct {
	store    Store
	runs     Runner
	resolver Resolver
	sources  Sources
	mics     mic.MICMap
	log      *slog.Logger
}

// New returns a Service.
func New(store Store, runs Runner, resolver Resolver, sources Sources, mics mic.MICMap, log *slog.Logger) *Service {
	return &Service{store: store, runs: runs, resolver: resolver, sources: sources, mics: mics, log: log}
}

// Start starts a replay of source's keys as a run of source's user, started
// by admin, and returns the pending row. A replay belongs to the source's
// user, so Start refuses a system run.
func (s *Service) Start(ctx context.Context, admin uuid.UUID, source gen.Run, scope Scope) (gen.Run, error) {
	if _, err := run.Owner(source); err != nil {
		return gen.Run{}, err
	}
	keys, err := s.selectKeys(ctx, source, scope)
	if err != nil {
		return gen.Run{}, err
	}
	w := &replay{svc: s, store: s.store, runs: s.runs, resolver: s.resolver, admin: admin, source: source, scope: scope, keys: keys}
	spec := run.Spec{Kind: gen.RunKindReplay, Trigger: gen.RunTriggerAdministrator, UserID: source.UserID, Lane: lane, Prepare: w.prepare}
	return s.runs.Start(ctx, spec, w.work)
}

// selectKeys picks the keys once, when the replay starts. A key that becomes
// eligible later waits for another replay. A replay run's keys are reached
// through its resolution child, so a source of any kind but statement or
// resolution is refused.
func (s *Service) selectKeys(ctx context.Context, source gen.Run, scope Scope) ([]gen.StatedKey, error) {
	if source.Kind != gen.RunKindStatement && source.Kind != gen.RunKindResolution {
		return nil, ErrKind
	}
	var out []gen.StatedKey
	if scope.Datasource == "" {
		rows, err := s.store.ListUnavailableKeys(ctx, gen.ListUnavailableKeysParams{UserID: *source.UserID, SourceID: source.ID})
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
		identity, ok := entry.Integration.(market.Identity)
		if !ok {
			return nil, ErrNoIdentity
		}
		rows, err := s.store.ListKeysUncoveredBy(ctx, gen.ListKeysUncoveredByParams{UserID: *source.UserID, SourceID: source.ID, Datasource: scope.Datasource})
		if err != nil {
			return nil, fmt.Errorf("list uncovered keys: %w", err)
		}
		for _, r := range rows {
			if _, err := identity.Serves(r.StatedKey); err == nil {
				out = append(out, r.StatedKey)
			}
		}
	}
	if len(out) == 0 {
		return nil, ErrEmpty
	}
	return out, nil
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
	svc      *Service
	store    Store
	runs     Runner
	resolver Resolver
	admin    uuid.UUID
	source   gen.Run
	scope    Scope
	keys     []gen.StatedKey
	// written lists the identifiers of the identifier facts the resolution
	// wrote.
	written []resolve.WrittenIdentifierFacts
}

// prepare writes the replays row.
func (r *replay) prepare(ctx context.Context, run gen.Run) error {
	arg := gen.CreateReplayParams{ID: run.ID, UserID: *run.UserID, SourceID: r.source.ID, StartedBy: r.admin}
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
// associated. Afterwards it replays the keys that state an identifier of its
// own new identifier facts. A point in time lookup asks only for a day with
// no assertion of the ticker, so the chain of replays ends.
func (r *replay) work(ctx context.Context, run gen.Run) error {
	res, err := r.runs.Child(ctx, run, gen.RunKindResolution, r.resolve)
	if err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	err = r.store.Tx(ctx, func(q Queries) error {
		if err := q.LockUserKeys(ctx, *run.UserID); err != nil {
			return fmt.Errorf("lock keys: %w", err)
		}
		return group.Regroup(ctx, q, *run.UserID)
	})
	if err != nil {
		return err
	}
	return r.svc.ReplayNewIdentifierFacts(ctx, res, r.written)
}

func (r *replay) resolve(ctx context.Context, res gen.Run) error {
	keys := make([]resolve.KeyOrderDates, len(r.keys))
	for i, k := range r.keys {
		keys[i] = resolve.KeyOrderDates{Row: k}
	}
	_, written, err := r.resolver.Resolve(ctx, res, keys)
	r.written = written
	return err
}

// ReplayNewIdentifierFacts replays every user's unresolved keys that state an
// identifier of written, as children of the run that wrote it. The keys that
// except resolved are left out, and a key is replayed once. A user other than
// the run's replays beneath one system child of the run, or directly beneath
// a system run. A failed replay is logged and the rest go on. The parent has
// completed by then, so the replays run in the goroutine of the run that
// found the keys.
func (s *Service) ReplayNewIdentifierFacts(ctx context.Context, except gen.Run, written []resolve.WrittenIdentifierFacts) error {
	picked := map[uuid.UUID]bool{}
	for _, w := range written {
		probes, err := s.probes(w.Identifiers)
		if err != nil {
			return err
		}
		if len(probes) == 0 {
			continue
		}
		rows, err := s.store.ListUnassociatedKeysStating(ctx, gen.ListUnassociatedKeysStatingParams{Probes: probes, RunID: except.ID})
		if err != nil {
			return fmt.Errorf("list keys stating new identifier facts: %w", err)
		}
		var users []uuid.UUID
		byUser := map[uuid.UUID][]gen.StatedKey{}
		for _, k := range rows {
			if picked[k.ID] {
				continue
			}
			picked[k.ID] = true
			if _, ok := byUser[k.UserID]; !ok {
				users = append(users, k.UserID)
			}
			byUser[k.UserID] = append(byUser[k.UserID], k)
		}
		s.beneath(ctx, w.Run, users, byUser)
	}
	return nil
}

// beneath replays each user's keys beneath parent, the run that wrote the
// facts.
func (s *Service) beneath(ctx context.Context, parent gen.Run, users []uuid.UUID, byUser map[uuid.UUID][]gen.StatedKey) {
	var others []uuid.UUID
	for _, u := range users {
		if parent.UserID != nil && *parent.UserID == u {
			s.replayKeys(ctx, parent, u, byUser[u])
			continue
		}
		others = append(others, u)
	}
	if len(others) == 0 {
		return
	}
	if parent.UserID == nil {
		for _, u := range others {
			s.replayKeys(ctx, parent, u, byUser[u])
		}
		return
	}
	_, err := s.runs.ChildAs(ctx, parent, nil, gen.RunKindReplay, func(ctx context.Context, sys gen.Run) error {
		for _, u := range others {
			s.replayKeys(ctx, sys, u, byUser[u])
		}
		return nil
	})
	if err != nil {
		s.log.Warn("replay of new identifier facts failed", "parent", parent.ID, "err", err)
	}
}

// replayKeys replays user's keys as a child of parent.
func (s *Service) replayKeys(ctx context.Context, parent gen.Run, user uuid.UUID, keys []gen.StatedKey) {
	w := &replay{svc: s, store: s.store, runs: s.runs, resolver: s.resolver, keys: keys}
	if _, err := s.runs.ChildAs(ctx, parent, &user, gen.RunKindReplay, w.work); err != nil {
		s.log.Warn("replay of new identifier facts failed", "parent", parent.ID, "user", user, "err", err)
	}
}

// probes writes the probes that find a key stating each identifier: one per
// venue of its operating MIC, since a statement may name a segment, or one
// under the domain itself where the map lists no venue of it. A domain
// identifier's probe leaves the value out, so it matches every ticker of the
// domain.
func (s *Service) probes(ids []types.Identifier) ([][]byte, error) {
	var out [][]byte
	for _, id := range ids {
		venues := s.mics.Venues(id.Domain)
		if len(venues) == 0 {
			venues = []string{id.Domain}
		}
		for _, v := range venues {
			obj := map[string]string{"type": string(id.Type), "domain": v}
			if !id.IsDomain() {
				obj["value"] = id.Value
			}
			b, err := json.Marshal([]map[string]string{obj})
			if err != nil {
				return nil, fmt.Errorf("probe %s: %w", id.Name(), err)
			}
			out = append(out, b)
		}
	}
	return out, nil
}
