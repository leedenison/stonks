// Package statement ingests a statement: one batch of transactions a user
// submitted, claiming every account they hold at one broker over a
// half-open range of order dates.
//
// Ingestion is a run: the call responds with the pending row once the
// statement is recorded, and resolution and the write follow; see
// [run.go](../run/run.go). Each distinct key is resolved once, however many
// rows state it; see [resolve.go](../resolve/resolve.go).
package statement

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"slices"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
	"github.com/leedenison/stonks/server/internal/ptr"
	"github.com/leedenison/stonks/server/internal/run"
)

// ErrInvalid is returned by Create for a statement it cannot read.
var ErrInvalid = errors.New("invalid statement")

// Service ingests statements.
type Service struct {
	store    Store
	runs     Runner
	resolver Resolver
	clock    func() time.Time
}

// New returns a Service. clock supplies today for the order date check.
func New(store Store, runs Runner, resolver Resolver, clock func() time.Time) *Service {
	return &Service{store: store, runs: runs, resolver: resolver, clock: clock}
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
	rows, err := s.store.ListCurrencies(ctx)
	if err != nil {
		return gen.Run{}, fmt.Errorf("list currencies: %w", err)
	}
	currencies := make(map[string]bool, len(rows))
	families := make(map[string]string, len(rows))
	for _, c := range rows {
		currencies[c.Code] = true
		families[c.Code] = c.Family
	}
	now := s.clock().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	g := &ingestion{store: s.store, runs: s.runs, resolver: s.resolver, user: userID, broker: broker, from: from, before: before, families: families, keys: map[uint64][]*key{}}
	for i, r := range msg.GetRows() {
		g.rows = append(g.rows, g.validate(int32(i), r, today, currencies))
	}
	for i, sp := range msg.GetSplits() {
		split, err := g.split(int32(i), sp, currencies)
		if err != nil {
			return gen.Run{}, fmt.Errorf("%w: split %d: %w", ErrInvalid, i, err)
		}
		g.splits = append(g.splits, split)
	}
	spec := run.Spec{Kind: gen.RunKindStatement, Trigger: gen.RunTriggerUser, UserID: userID, Lane: string(broker), Prepare: g.prepare}
	return s.runs.Start(ctx, spec, g.work)
}

// ingestion is one statement's work, from receipt to the write.
type ingestion struct {
	store    Store
	runs     Runner
	resolver Resolver
	user     uuid.UUID
	broker   gen.Broker
	from     time.Time
	before   time.Time
	rows     []row
	splits   []split
	// families maps each currency code to its family, the key of a listing.
	families map[string]string
	// keys holds each distinct stated key under its hash, and order lists
	// them by first appearance.
	keys  map[uint64][]*key
	order []*key
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
	id          uuid.UUID
	class       *gen.AssetClass
	currency    *string
	identifiers []types.Identifier

	row     gen.StatedKey
	outcome gen.ResolutionOutcome
	reason  string
}

// statable reports whether k says anything about an instrument at all. A key
// stating no identifier names nothing and its rows are rejected.
func (k *key) statable() bool { return len(k.identifiers) > 0 }

// seed salts key hashes for the life of the process.
var seed = maphash.MakeSeed()

// hash agrees with equal.
func (k *key) hash() uint64 {
	var h maphash.Hash
	h.SetSeed(seed)
	part := func(s *string) {
		if s == nil {
			h.WriteByte(0)
			return
		}
		h.WriteByte(1)
		h.WriteString(*s)
		h.WriteByte(0)
	}
	part((*string)(k.class))
	part(k.currency)
	for _, id := range k.identifiers {
		h.WriteString(string(id.Type))
		h.WriteByte(0)
		h.WriteString(id.Domain)
		h.WriteByte(0)
		h.WriteString(id.Value)
		h.WriteByte(0)
	}
	return h.Sum64()
}

// equal reports whether k and o state the same key.
func (k *key) equal(o *key) bool {
	if !ptr.Equal(k.class, o.class) || !ptr.Equal(k.currency, o.currency) {
		return false
	}
	return slices.Equal(k.identifiers, o.identifiers)
}

// intern returns the key equal to k the statement already has, or k with an
// id minted for it.
func (g *ingestion) intern(k *key) *key {
	h := k.hash()
	for _, held := range g.keys[h] {
		if held.equal(k) {
			return held
		}
	}
	k.id = db.NewID()
	g.keys[h] = append(g.keys[h], k)
	g.order = append(g.order, k)
	return k
}

// prepare writes the statement row, its stated keys and its splits.
func (g *ingestion) prepare(ctx context.Context, run gen.Run) error {
	return g.store.Tx(ctx, func(q Queries) error {
		arg := gen.CreateStatementParams{ID: run.ID, UserID: g.user, Broker: g.broker, OrderFrom: g.from, OrderBefore: g.before, RowCount: int32(len(g.rows))}
		if _, err := q.CreateStatement(ctx, arg); err != nil {
			return fmt.Errorf("create statement: %w", err)
		}
		for _, k := range g.order {
			arg := gen.CreateStatedKeyParams{ID: k.id, StatementID: run.ID, UserID: g.user, AssetClass: k.class, Currency: k.currency, Identifiers: k.identifiers}
			row, err := q.CreateStatedKey(ctx, arg)
			if err != nil {
				return fmt.Errorf("create stated key: %w", err)
			}
			k.row = row
		}
		for _, sp := range g.splits {
			arg := gen.CreateStatementSplitParams{StatementID: run.ID, UserID: g.user, Ordinal: sp.ordinal, StatedKeyID: sp.key.id, EffectiveDate: sp.effective, Quantity: sp.quantity, RatioFrom: sp.from, RatioTo: sp.to}
			if err := q.CreateStatementSplit(ctx, arg); err != nil {
				return fmt.Errorf("create split: %w", err)
			}
		}
		return nil
	})
}

func (g *ingestion) work(ctx context.Context, run gen.Run) error {
	if _, err := g.runs.Child(ctx, run, gen.RunKindResolution, g.resolve); err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	return g.write(ctx, run)
}

// resolve resolves the keys as a child run and takes each outcome onto its
// key.
func (g *ingestion) resolve(ctx context.Context, res gen.Run) error {
	rows := make([]gen.StatedKey, len(g.order))
	for i, k := range g.order {
		rows[i] = k.row
	}
	out, err := g.resolver.Resolve(ctx, res, rows)
	if err != nil {
		return err
	}
	for i, k := range g.order {
		k.outcome = out[i].Outcome
		if out[i].Reason != nil {
			k.reason = *out[i].Reason
		}
	}
	return nil
}
