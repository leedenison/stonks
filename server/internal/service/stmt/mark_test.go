package stmt

import (
	"testing"

	stmtv1 "github.com/leedenison/stonks/proto/stmt/v1"
	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/platform/ptr"
)

func TestMark(t *testing.T) {
	tests := []struct {
		name string
		v    *gen.Validity
		want stmtv1.TxnMark
	}{
		{name: "confirmed", v: ptr.To(gen.ValidityConfirmed), want: stmtv1.TxnMark_TXN_MARK_CONFIRMED},
		{name: "provisional", v: ptr.To(gen.ValidityProvisional), want: stmtv1.TxnMark_TXN_MARK_PROVISIONAL},
		{name: "names nothing", want: stmtv1.TxnMark_TXN_MARK_UNRESOLVED},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := mark(tc.v); got != tc.want {
				t.Errorf("mark = %v, want %v", got, tc.want)
			}
		})
	}
}
