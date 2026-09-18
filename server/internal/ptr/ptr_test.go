package ptr

import "testing"

func TestEqual(t *testing.T) {
	x, y, z := "x", "x", "z"
	cases := []struct {
		name string
		a, b *string
		want bool
	}{
		{"both nil", nil, nil, true},
		{"one nil", &x, nil, false},
		{"equal values", &x, &y, true},
		{"different values", &x, &z, false},
	}
	for _, tc := range cases {
		if got := Equal(tc.a, tc.b); got != tc.want {
			t.Errorf("%s: Equal = %v, want %v", tc.name, got, tc.want)
		}
	}
}
