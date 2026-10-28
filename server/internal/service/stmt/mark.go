package stmt

import (
	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
)

// mark returns the mark for the validity that txn_instrs gives a transaction.
// A nil v is unresolved.
func mark(v *gen.Validity) stmtv1.TxnMark {
	switch {
	case v == nil:
		return stmtv1.TxnMark_TXN_MARK_UNRESOLVED
	case *v == gen.ValidityConfirmed:
		return stmtv1.TxnMark_TXN_MARK_CONFIRMED
	}
	return stmtv1.TxnMark_TXN_MARK_PROVISIONAL
}
