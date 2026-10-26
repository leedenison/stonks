package statement

import (
	"time"

	statementv1 "github.com/leedenison/stonks/proto/statement/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/types"
	"github.com/leedenison/stonks/server/internal/validity"
)

// mark returns how firmly a transaction's key names an instrument on day on.
// A key has a fixed association or a dated one, so at most one of fixedVia
// and t is set. A key the user arbitrated is confirmed whichever identifier
// it used.
func mark(arbiter *gen.Arbiter, fixedVia *types.IdentifierType, t *validity.Ticker, on time.Time) statementv1.TransactionMark {
	switch {
	case arbiter != nil && *arbiter == gen.ArbiterUser:
		return statementv1.TransactionMark_TRANSACTION_MARK_CONFIRMED
	case fixedVia != nil:
		if fixedVia.Trait().Reassignment == types.IdentifierReassignmentStable {
			return statementv1.TransactionMark_TRANSACTION_MARK_CONFIRMED
		}
		return statementv1.TransactionMark_TRANSACTION_MARK_PROVISIONAL
	case t != nil:
		iv, ok := t.On(on)
		switch {
		case !ok:
			return statementv1.TransactionMark_TRANSACTION_MARK_UNRESOLVED
		case iv.Validity == validity.Confirmed:
			return statementv1.TransactionMark_TRANSACTION_MARK_CONFIRMED
		}
		return statementv1.TransactionMark_TRANSACTION_MARK_PROVISIONAL
	}
	return statementv1.TransactionMark_TRANSACTION_MARK_UNRESOLVED
}
