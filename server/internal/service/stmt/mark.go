package stmt

import (
	"time"

	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/identity/tickervalidity"
	"github.com/leedenison/stonks/server/internal/types"
)

// mark returns how firmly a transaction's key names an instrument on day on.
// A key the user arbitrated is confirmed whichever identifier it used.
func mark(arbiter *gen.Arbiter, fixedVia *types.IdentifierType, dated bool, ivs tickervalidity.Intervals, on time.Time) stmtv1.TxnMark {
	switch {
	case arbiter != nil && *arbiter == gen.ArbiterUser:
		return stmtv1.TxnMark_TXN_MARK_CONFIRMED
	case fixedVia != nil:
		if fixedVia.Trait().Reassignment == types.IdentifierReassignmentStable {
			return stmtv1.TxnMark_TXN_MARK_CONFIRMED
		}
		return stmtv1.TxnMark_TXN_MARK_PROVISIONAL
	case dated:
		iv, ok := ivs.On(on)
		switch {
		case !ok:
			return stmtv1.TxnMark_TXN_MARK_UNRESOLVED
		case iv.Validity == tickervalidity.Confirmed:
			return stmtv1.TxnMark_TXN_MARK_CONFIRMED
		}
		return stmtv1.TxnMark_TXN_MARK_PROVISIONAL
	}
	return stmtv1.TxnMark_TXN_MARK_UNRESOLVED
}
