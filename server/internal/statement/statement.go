// Package statement ingests a statement: one batch of transactions a user
// submitted in the neutral format of proto/statement/v1/statement.proto,
// claiming every account they hold at one broker over a half-open range of
// order dates.
//
// Receipt is the RPC. It validates the envelope and every row, deduplicates
// the stated keys, and starts the run, whose Prepare writes the statement
// row, the stated keys and the stated splits in one database transaction
// before the call answers; see [run.go](../run/run.go).
//
// Validation. A row is invalid when a date or its quantity does not parse,
// its asset class or an identifier type is outside the vocabulary, its
// order date lies outside the claimed period or after today in UTC, or it
// states a currency the system does not know. A key names one line, so it
// states at most one identifier per type and domain, and at least one
// identifier resolution admits, a description standing for a broker
// description identifier. An invalid row is rejected on its own, recorded
// as an item, and contributes no stated key; the remaining rows are
// ingested. An invalid envelope or split refuses the whole statement, since
// the marshaller builds them.
//
// Resolution. Each distinct key is resolved once, as a run of kind
// resolution with the statement as parent, and every row carrying the key
// takes its answer. A key resolves through the identifier it states that
// resolution admits, today a currency identifier, which names the currency
// instrument; or else through its broker description, a listing grain
// identifier minted here whose value is the description text and whose
// domain is the broker and the upload channel, as "ibkr/upload". Every
// other stated identifier stays in the stated key. An instrument grain
// identifier reaches the instrument, and the key's currency picks the
// listing among its lines; a listing grain one reaches the listing, and the
// key's currency is checked against it. A key stating no currency names the
// instrument alone, or the listing its description names. A key stating a
// currency the instrument has no line in, or an asset class disjoint from
// the instrument's in the class tree, contradicts it and is rejected with
// its rows; a class above or below is accepted, and the stored class is not
// changed. A broker description nothing holds creates a user owned
// instrument and listing carrying it, which needs a currency. Keys stating
// a currency are resolved before those stating none, and otherwise in the
// order they first appear, so a transfer stated beside the trade that first
// names its listing finds it, and the first of two keys stating one
// description with different currencies is the one that names the listing.
// Two runs can race to create one identifier only when they share a user
// and a broker, which the run order excludes; the unique index on
// identifiers is the backstop, and an insert it refuses is re-read.
//
// The write is one database transaction: delete every transaction of the
// user and broker whose order date lies in the period, insert the accepted
// rows, insert the items, and complete the run. A statement that omits an
// account deletes that account's transactions in the period, as an empty
// range at a boundary deletes. A run in any other terminal state has written
// no transactions and no items, and uploading the statement again is the
// recovery; the payload is not persisted, so nothing is resumed. A failed or
// interrupted statement leaves its run, its statement row, its stated keys,
// its splits, its resolution run and any instruments the resolution created,
// which are kept as canonical whether or not the transactions naming them
// were written.
package statement

import (
	"context"
	"errors"
	"fmt"
	"hash/maphash"
	"slices"
	"sort"
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

// ErrInvalid is returned by Create for a statement it cannot read: a broker
// outside the vocabulary, a period bound that does not parse or an empty
// period, or a split whose key, date, quantity or ratio does not parse.
var ErrInvalid = errors.New("invalid statement")

// Service ingests statements.
type Service struct {
	store Store
	runs  Runner
	clock func() time.Time
}

// New returns a Service. clock supplies today for the order date check.
func New(store Store, runs Runner, clock func() time.Time) *Service {
	return &Service{store: store, runs: runs, clock: clock}
}

// Create validates msg, starts its run and answers with the pending row.
func (s *Service) Create(ctx context.Context, userID uuid.UUID, msg *statementv1.Statement) (gen.Run, error) {
	broker, ok := db.FromProto[gen.Broker](msg.GetBroker())
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
	codes, err := s.store.ListCurrencies(ctx)
	if err != nil {
		return gen.Run{}, fmt.Errorf("list currencies: %w", err)
	}
	currencies := make(map[string]bool, len(codes))
	for _, c := range codes {
		currencies[c] = true
	}
	now := s.clock().UTC()
	today := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	g := &ingestion{store: s.store, runs: s.runs, user: userID, broker: broker, from: from, before: before, keys: map[uint64][]*key{}}
	for i, r := range msg.GetRows() {
		g.rows = append(g.rows, g.validate(int32(i), r, today, currencies))
	}
	for i, sp := range msg.GetSplits() {
		split, err := g.split(int32(i), sp)
		if err != nil {
			return gen.Run{}, fmt.Errorf("%w: split %d: %w", ErrInvalid, i, err)
		}
		g.splits = append(g.splits, split)
	}
	spec := run.Spec{Kind: gen.RunKindStatement, UserID: userID, Lane: string(broker), Prepare: g.prepare}
	return s.runs.Start(ctx, spec, g.work)
}

// ingestion is one statement's work, from receipt to the write.
type ingestion struct {
	store  Store
	runs   Runner
	user   uuid.UUID
	broker gen.Broker
	from   time.Time
	before time.Time
	rows   []row
	splits []split
	// keys holds each distinct stated key under its hash, and order lists
	// them by first appearance.
	keys  map[uint64][]*key
	order []*key
}

// row is one row of the payload. reason is set when validation rejected it,
// and key is set otherwise.
type row struct {
	ordinal    int32
	stated     *statementv1.Row
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

// key is one distinct stated key, and once resolved, its answer: the
// listing it names and the instrument of that listing, or the reason it
// names none.
type key struct {
	id          uuid.UUID
	class       *gen.AssetClass
	currency    *string
	description *string
	identifiers []types.StatedIdentifier

	outcome    gen.ResolutionOutcome
	instrument *uuid.UUID
	listing    *uuid.UUID
	reason     string
}

// seed salts key hashes for the life of the process.
var seed = maphash.MakeSeed()

// hash agrees with equal: it covers the class, currency, description and
// the sorted identifiers, each field led by whether it is set.
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
	part(k.description)
	for _, id := range k.identifiers {
		h.WriteString(id.Type)
		h.WriteByte(0)
		part(id.Domain)
		h.WriteString(id.Value)
		h.WriteByte(0)
	}
	return h.Sum64()
}

// equal reports whether k and o state the same key.
func (k *key) equal(o *key) bool {
	if !ptr.Equal(k.class, o.class) || !ptr.Equal(k.currency, o.currency) || !ptr.Equal(k.description, o.description) {
		return false
	}
	return slices.EqualFunc(k.identifiers, o.identifiers, func(a, b types.StatedIdentifier) bool {
		return a.Type == b.Type && ptr.Equal(a.Domain, b.Domain) && a.Value == b.Value
	})
}

// identifier returns the identifier of type t the key states, if any.
func (k *key) identifier(t gen.IdentifierType) (types.StatedIdentifier, bool) {
	for _, id := range k.identifiers {
		if id.Type == string(t) {
			return id, true
		}
	}
	return types.StatedIdentifier{}, false
}

func (k *key) set(outcome gen.ResolutionOutcome, l gen.Listing) {
	k.outcome = outcome
	k.instrument = &l.InstrumentID
	k.listing = &l.ID
}

func (k *key) reject(format string, args ...any) {
	k.outcome = gen.ResolutionOutcomeRejected
	k.reason = fmt.Sprintf(format, args...)
}

// intern returns the key equal to k the statement already holds, or k with
// an id minted for it.
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

// orderedKeys returns the keys with those stating a currency first, and in
// order of first appearance within each group.
func (g *ingestion) orderedKeys() []*key {
	keys := slices.Clone(g.order)
	sort.SliceStable(keys, func(i, j int) bool {
		return keys[i].currency != nil && keys[j].currency == nil
	})
	return keys
}

// prepare writes the statement row, its stated keys and its splits.
func (g *ingestion) prepare(ctx context.Context, run gen.Run) error {
	return g.store.Tx(ctx, func(q Queries) error {
		arg := gen.CreateStatementParams{ID: run.ID, UserID: g.user, Broker: g.broker, OrderFrom: g.from, OrderBefore: g.before, RowCount: int32(len(g.rows))}
		if _, err := q.CreateStatement(ctx, arg); err != nil {
			return fmt.Errorf("create statement: %w", err)
		}
		for _, k := range g.orderedKeys() {
			arg := gen.CreateStatedKeyParams{ID: k.id, StatementID: run.ID, UserID: g.user, AssetClass: k.class, Currency: k.currency, Description: k.description, Identifiers: k.identifiers}
			if _, err := q.CreateStatedKey(ctx, arg); err != nil {
				return fmt.Errorf("create stated key: %w", err)
			}
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

// work resolves the keys as a child run, then writes.
func (g *ingestion) work(ctx context.Context, run gen.Run) error {
	if _, err := g.runs.Child(ctx, run, gen.RunKindResolution, g.resolve); err != nil {
		return fmt.Errorf("resolution: %w", err)
	}
	return g.write(ctx, run)
}
