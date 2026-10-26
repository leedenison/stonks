// Package statement ingests a statement: one batch of transactions a user
// submitted, claiming every account they hold at one broker over a
// half-open range of order dates.
//
// Ingestion is a run: the call responds with the pending row once the
// statement is recorded, and resolution and the write follow; see
// [run.go](../run/run.go). A stated key is the user's: one row per asset
// class, currency and set of identifiers, linked to each statement that
// states it. A statement resolves only the keys it states that lack a fixed
// association, each once, however many rows state it; see
// [resolve.go](../resolve/resolve.go). After a write, every key of the user
// has a transaction or a link to a statement.
//
// The service also lists and confirms candidates for a key of the user's
// that is not yet associated. Each call is a synchronous resolution run;
// see [resolve.go](../resolve/resolve.go).
package statement

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/server/internal/currency"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/resolve"
	"github.com/leedenison/stonks/server/internal/run"
	"github.com/leedenison/stonks/server/internal/types"
)

// ErrInvalid is returned by Create for a statement it cannot read.
var ErrInvalid = errors.New("invalid statement")

// Service ingests statements.
type Service struct {
	store    Store
	runs     Runner
	resolver Resolver
	replays  Replays
	families currency.Families
	clock    func() time.Time
	log      *slog.Logger
}

// New returns a Service. families holds the currencies a statement may
// state, and clock supplies today for the order date check.
func New(store Store, runs Runner, resolver Resolver, replays Replays, families currency.Families, clock func() time.Time, log *slog.Logger) *Service {
	return &Service{store: store, runs: runs, resolver: resolver, replays: replays, families: families, clock: clock, log: log}
}

// Create validates msg, starts its run and responds with the pending row.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, msg *statementv1.Statement) (gen.Run, error) {
	broker, ok := types.FromProto[gen.Broker](msg.GetBroker())
	if !ok || broker == "" {
		return gen.Run{}, fmt.Errorf("%w: broker %v", ErrInvalid, msg.GetBroker())
	}
	from, err := parseDate(msg.GetOrderFrom())
	if err != nil {
		return gen.Run{}, fmt.Errorf("%w: order_from: %w", ErrInvalid, err)
	}
	before, err := parseDate(msg.GetOrderBefore())
	if err != nil {
		return gen.Run{}, fmt.Errorf("%w: order_before: %w", ErrInvalid, err)
	}
	if !from.Before(before) {
		return gen.Run{}, fmt.Errorf("%w: period [%s, %s) is empty", ErrInvalid, msg.GetOrderFrom(), msg.GetOrderBefore())
	}
	today := types.Day(s.clock())
	g := &ingestion{store: s.store, runs: s.runs, resolver: s.resolver, replays: s.replays, user: userID, broker: broker, from: from, before: before, keys: map[string]*key{}}
	for i, r := range msg.GetRows() {
		g.rows = append(g.rows, g.validate(int32(i), r, today, s.families))
	}
	for i, sp := range msg.GetSplits() {
		split, err := g.split(int32(i), sp, s.families)
		if err != nil {
			return gen.Run{}, fmt.Errorf("%w: split %d: %w", ErrInvalid, i, err)
		}
		g.splits = append(g.splits, split)
	}
	spec := run.Spec{Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser, UserID: &userID, Lane: string(broker), Prepare: g.prepare}
	return s.runs.Start(ctx, spec, g.work)
}

// ingestion is one statement's work, from receipt to the write.
type ingestion struct {
	store    Store
	runs     Runner
	resolver Resolver
	replays  Replays
	user     uuid.UUID
	broker   gen.Broker
	from     time.Time
	before   time.Time
	rows     []row
	splits   []split
	// keys holds each distinct stated key under its canonical encoding, and
	// order lists them by first appearance.
	keys  map[string]*key
	order []*key
	// written lists the identifiers of the identifier facts the resolution
	// wrote.
	written []resolve.WrittenIdentifierFacts
}

// row is one row of the payload.
type row struct {
	ordinal int32
	stated  *statementv1.Row
	// key is nil where validation rejected the row; reason then says why.
	key        *key
	order      time.Time
	settlement time.Time
	asAt       time.Time
	quantity   decimal.Decimal
	reason     string
}

// split is one stated split. from and to are set together or not at all.
type split struct {
	ordinal   int32
	key       *key
	effective time.Time
	quantity  decimal.Decimal
	from      *decimal.Decimal
	to        *decimal.Decimal
}

// key is one distinct stated key: once prepared, its row, and once resolved,
// its outcome and the reason it names no instrument.
type key struct {
	class       *types.AssetClass
	currency    *string
	identifiers []types.Identifier

	row     gen.StatedKey
	outcome gen.ResolutionOutcome
	reason  string
}

// statable reports whether k says anything about an instrument at all. A key
// stating no identifier names nothing and its rows are rejected.
func (k *key) statable() bool { return len(k.identifiers) > 0 }

// canon encodes k so that two keys stating the same thing encode alike. Each
// part ends in a NUL, which no stated value carries. An absent field encodes
// as a NUL alone.
func (k *key) canon() string {
	var b strings.Builder
	part := func(s *string) {
		if s != nil {
			b.WriteByte(1)
			b.WriteString(*s)
		}
		b.WriteByte(0)
	}
	part((*string)(k.class))
	part(k.currency)
	for _, id := range k.identifiers {
		for _, s := range []string{string(id.Type), id.Domain, id.Value} {
			b.WriteString(s)
			b.WriteByte(0)
		}
	}
	return b.String()
}

// intern returns the key equal to k the statement already has, or k, now one
// of its keys.
func (g *ingestion) intern(k *key) *key {
	c := k.canon()
	if held, ok := g.keys[c]; ok {
		return held
	}
	g.keys[c] = k
	g.order = append(g.order, k)
	return k
}

// prepare writes the statement row, its stated keys, the links from the
// statement to them, and its splits. When another statement of the user
// stated a key, the upsert returns the row that statement wrote.
func (g *ingestion) prepare(ctx context.Context, run gen.Run) error {
	return g.store.Tx(ctx, func(q Queries) error {
		arg := gen.CreateStatementParams{ID: run.ID, UserID: g.user, Broker: g.broker, OrderFrom: g.from, OrderBefore: g.before, RowCount: int32(len(g.rows))}
		if _, err := q.CreateStatement(ctx, arg); err != nil {
			return fmt.Errorf("create statement: %w", err)
		}
		links := make([]gen.CreateStatementKeysParams, 0, len(g.order))
		for _, k := range g.order {
			arg := gen.UpsertStatedKeyParams{ID: db.NewID(), UserID: g.user, AssetClass: k.class, Currency: k.currency, Identifiers: k.identifiers}
			row, err := q.UpsertStatedKey(ctx, arg)
			if err != nil {
				return fmt.Errorf("upsert stated key: %w", err)
			}
			k.row = row
			links = append(links, gen.CreateStatementKeysParams{StatementID: run.ID, UserID: g.user, StatedKeyID: row.ID})
		}
		if len(links) > 0 {
			if _, err := q.CreateStatementKeys(ctx, links); err != nil {
				return fmt.Errorf("link stated keys: %w", err)
			}
		}
		for _, sp := range g.splits {
			arg := gen.CreateStatementSplitParams{StatementID: run.ID, UserID: g.user, Ordinal: sp.ordinal, StatedKeyID: sp.key.row.ID, EffectiveDate: sp.effective, Quantity: sp.quantity, RatioFrom: sp.from, RatioTo: sp.to}
			if err := q.CreateStatementSplit(ctx, arg); err != nil {
				return fmt.Errorf("create split: %w", err)
			}
		}
		return nil
	})
}

// work resolves the keys, writes the statement, and then replays every
// user's unresolved keys that state an identifier of the resolution's new
// identifier facts. The write completes the statement run in its own
// transaction, before the replays start.
func (g *ingestion) work(ctx context.Context, run gen.Run) error {
	res, err := g.runs.Child(ctx, run, gen.RunKindResolution, g.resolve)
	if err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	if err := g.write(ctx, run); err != nil {
		return err
	}
	g.replays.ReplayNewIdentifierFacts(ctx, res, g.written)
	return nil
}

// resolve resolves the keys that have no instrument as a child run, each with
// the as-at dates of its rows, and takes each outcome onto its key. A dated
// key is resolved again, since its new dates may need facts the system lacks.
// When an earlier statement of the user gave a key a fixed association,
// resolve leaves it as it is and accepts its rows.
func (g *ingestion) resolve(ctx context.Context, res gen.Run) error {
	dates := map[*key][]time.Time{}
	for _, r := range g.rows {
		if r.key != nil && !slices.ContainsFunc(dates[r.key], r.asAt.Equal) {
			dates[r.key] = append(dates[r.key], r.asAt)
		}
	}
	var keys []resolve.KeyDates
	for _, k := range g.order {
		if k.row.InstrumentID == nil {
			keys = append(keys, resolve.KeyDates{Row: k.row, Dates: dates[k]})
		}
	}
	out, written, err := g.resolver.Resolve(ctx, res, keys)
	if err != nil {
		return err
	}
	g.written = written
	byKey := make(map[uuid.UUID]gen.ResolutionKey, len(out))
	for _, rk := range out {
		byKey[rk.StatedKeyID] = rk
	}
	for _, k := range g.order {
		if k.row.InstrumentID != nil {
			continue
		}
		rk, ok := byKey[k.row.ID]
		if !ok {
			return fmt.Errorf("the resolution recorded no outcome for key %s", k.row.ID)
		}
		k.outcome = rk.Outcome
		k.reason = strings.Join(rk.Reasons, "; ")
	}
	return nil
}
