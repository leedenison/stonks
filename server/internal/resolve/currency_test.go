package resolve

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
	"github.com/leedenison/stonks/server/internal/ptr"
)

func TestFamily(t *testing.T) {
	if got := cur.family(gen.StatedKey{Currency: ptr.To("GBX")}); got != "GBP" {
		t.Errorf("family(GBX) = %q, want GBP", got)
	}
	if got := cur.family(gen.StatedKey{}); got != "" {
		t.Errorf("family(none) = %q, want none", got)
	}
}
