package types

import (
	"testing"

	"github.com/shopspring/decimal"
)

func TestRatioEqual(t *testing.T) {
	r := func(from, to string) Ratio {
		return Ratio{From: decimal.RequireFromString(from), To: decimal.RequireFromString(to)}
	}
	tests := []struct {
		name string
		a, b Ratio
		want bool
	}{
		{name: "same terms", a: r("1", "2"), b: r("1", "2"), want: true},
		{name: "other terms", a: r("1", "2"), b: r("5", "10"), want: true},
		{name: "decimal terms", a: r("1.000000", "10.000000"), b: r("1", "10"), want: true},
		{name: "fractional", a: r("2", "3"), b: r("4", "6"), want: true},
		{name: "inverse", a: r("1", "2"), b: r("2", "1"), want: false},
		{name: "other ratio", a: r("1", "2"), b: r("1", "3"), want: false},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := tc.a.Equal(tc.b); got != tc.want {
				t.Errorf("%v.Equal(%v) = %v, want %v", tc.a, tc.b, got, tc.want)
			}
		})
	}
}
