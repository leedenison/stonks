package statement

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// write replaces the period with the accepted rows, records the rejected
// ones as items and completes the run, in one transaction.
func (g *ingestion) write(ctx context.Context, run gen.Run) error {
	var accepted, rejected int64
	err := g.store.Tx(ctx, func(q Queries) error {
		if err := q.LockUserKeys(ctx, g.user); err != nil {
			return fmt.Errorf("lock keys: %w", err)
		}
		period := gen.DeleteTransactionsParams{UserID: g.user, Broker: g.broker, OrderFrom: g.from, OrderBefore: g.before}
		if _, err := q.DeleteTransactions(ctx, period); err != nil {
			return fmt.Errorf("delete period: %w", err)
		}
		for i := range g.rows {
			r := &g.rows[i]
			reason := r.reason
			if reason == "" {
				reason = r.key.reason
			}
			if reason != "" {
				stated, err := protojson.Marshal(r.stated)
				if err != nil {
					return fmt.Errorf("encode row %d: %w", r.ordinal, err)
				}
				item := gen.CreateStatementItemParams{StatementID: run.ID, UserID: g.user, Ordinal: r.ordinal, Reason: reason, Stated: stated}
				if err := q.CreateStatementItem(ctx, item); err != nil {
					return fmt.Errorf("create item %d: %w", r.ordinal, err)
				}
				rejected++
				continue
			}
			tx := gen.CreateTransactionParams{
				ID: db.NewID(), UserID: g.user, Broker: g.broker, StatementID: run.ID, StatedKeyID: r.key.id,
				OrderDate: r.order, SettlementDate: r.settlement, AsAt: r.asAt, Quantity: r.quantity,
			}
			if _, err := q.CreateTransaction(ctx, tx); err != nil {
				return fmt.Errorf("create transaction %d: %w", r.ordinal, err)
			}
			accepted++
		}
		for _, k := range g.order {
			if k.outcome != gen.ResolutionOutcomeMatched {
				continue
			}
			arg := gen.SetStatedKeyAssociationParams{ID: k.id, UserID: g.user, InstrumentID: k.instrument, ListingID: k.listing, ViaID: k.via, Validity: k.validity}
			if err := q.SetStatedKeyAssociation(ctx, arg); err != nil {
				return fmt.Errorf("associate key: %w", err)
			}
		}
		if err := g.regroup(ctx, q); err != nil {
			return err
		}
		if err := q.CompleteRun(ctx, run.ID); err != nil {
			return fmt.Errorf("complete run: %w", err)
		}
		return nil
	})
	if err != nil {
		return err
	}
	instr.rows(ctx, outcomeAccepted, accepted)
	instr.rows(ctx, outcomeRejected, rejected)
	return nil
}
