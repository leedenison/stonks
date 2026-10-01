package to

import (
	"testing"

	"github.com/google/uuid"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/db/types"
)

func TestIdentifier(t *testing.T) {
	row := gen.Identifier{ID: uuid.New(), InstrumentID: uuid.New(), Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	want := types.Identifier{Type: types.IdentifierTypeMicTicker, Domain: "XLON", Value: "VOD"}
	if got := Identifier(row); got != want {
		t.Errorf("Identifier(%+v) = %+v, want %+v", row, got, want)
	}
}
