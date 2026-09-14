package db

import (
	"testing"

	"github.com/google/uuid"
)

func TestNewID(t *testing.T) {
	a, b := NewID(), NewID()
	if a.Version() != 7 {
		t.Errorf("NewID() version = %d, want 7", a.Version())
	}
	if a.Variant() != uuid.RFC4122 {
		t.Errorf("NewID() variant = %s, want RFC4122", a.Variant())
	}
	if a.String() >= b.String() {
		t.Errorf("NewID() minted %s then %s, want the first to sort earlier", a, b)
	}
}
