package currency

import (
	"context"
	"testing"

	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

type rows []gen.Currency

func (r rows) ListCurrencies(context.Context) ([]gen.Currency, error) { return r, nil }

func TestLoad(t *testing.T) {
	one, pence := decimal.NewFromInt(1), decimal.RequireFromString("0.01")
	f, s, err := Load(context.Background(), rows{{Code: "GBP", Family: "GBP", Scale: one}, {Code: "GBX", Family: "GBP", Scale: pence}, {Code: "USD", Family: "USD", Scale: one}})
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	tests := []struct {
		code, family string
		known        bool
		scale        decimal.Decimal
	}{
		{code: "GBX", family: "GBP", known: true, scale: pence},
		{code: "USD", family: "USD", known: true, scale: one},
		{code: "ZZZ", scale: one},
		{code: "", scale: one},
	}
	for _, tc := range tests {
		if got := f.Family(tc.code); got != tc.family {
			t.Errorf("Family(%q) = %q, want %q", tc.code, got, tc.family)
		}
		if got := f.Known(tc.code); got != tc.known {
			t.Errorf("Known(%q) = %v, want %v", tc.code, got, tc.known)
		}
		if got := s.Scale(tc.code); !got.Equal(tc.scale) {
			t.Errorf("Scale(%q) = %s, want %s", tc.code, got, tc.scale)
		}
	}
}
