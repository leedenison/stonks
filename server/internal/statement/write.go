package statement

import (
	"context"
	"fmt"

	"google.golang.org/protobuf/encoding/protojson"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/server/internal/db"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/group"
)

// write replaces the statement's period with the accepted rows and records
// the rejected rows as items, in one transaction under the user's key lock.
// In that transaction it also recomputes the user's groups and completes the
// run. A row is rejected by validation or by its key's rejection.
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
		var items []gen.CreateStatementItemsParams
		var txs []gen.CreateTransactionsParams
		for i := range g.rows {
			r := &g.rows[i]
			reason := r.reason
			if reason == "" && r.key.outcome == gen.ResolutionOutcomeRejected {
				reason = r.key.reason
			}
			if reason != "" {
				stated, err := protojson.Marshal(r.stated)
				if err != nil {
					return fmt.Errorf("encode row %d: %w", r.ordinal, err)
				}
				items = append(items, gen.CreateStatementItemsParams{StatementID: run.ID, UserID: g.user, Ordinal: r.ordinal, Reason: reason, Stated: stated})
				continue
			}
			txs = append(txs, gen.CreateTransactionsParams{
				ID: db.NewID(), UserID: g.user, Broker: g.broker, StatementID: run.ID, StatedKeyID: r.key.id,
				OrderDate: r.order, SettlementDate: r.settlement, AsAt: r.asAt, Quantity: r.quantity,
			})
		}
		var err error
		if len(items) > 0 {
			if rejected, err = q.CreateStatementItems(ctx, items); err != nil {
				return fmt.Errorf("create items: %w", err)
			}
		}
		if len(txs) > 0 {
			if accepted, err = q.CreateTransactions(ctx, txs); err != nil {
				return fmt.Errorf("create transactions: %w", err)
			}
		}
		if err := group.Regroup(ctx, q, g.user); err != nil {
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

// ItemToProto converts a rejected row to its message.
func ItemToProto(it gen.StatementItem) (*statementv1.StatementItem, error) {
	row := &statementv1.Row{}
	if err := protojson.Unmarshal(it.Stated, row); err != nil {
		return nil, fmt.Errorf("read item %d: %w", it.Ordinal, err)
	}
	return &statementv1.StatementItem{Ordinal: it.Ordinal, Reason: it.Reason, Row: row}, nil
}
