package currency

import (
	"context"
	"testing"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

type rows []gen.Currency

func (r rows) ListCurrencies(context.Context) ([]gen.Currency, error) { return r, nil }

func TestLoad(t *testing.T) {
	f, err := Load(context.Background(), rows{{Code: "GBP", Family: "GBP"}, {Code: "GBX", Family: "GBP"}, {Code: "USD", Family: "USD"}})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tests := []struct {
		code, family string
		known        bool
	}{
		{code: "GBX", family: "GBP", known: true},
		{code: "USD", family: "USD", known: true},
		{code: "ZZZ"},
		{code: ""},
	}
	for _, tc := range tests {
		if got := f.Family(tc.code); got != tc.family {
			t.Errorf("Family(%q) = %q, want %q", tc.code, got, tc.family)
		}
		if got := f.Known(tc.code); got != tc.known {
			t.Errorf("Known(%q) = %v, want %v", tc.code, got, tc.known)
		}
	}
}
