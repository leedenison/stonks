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

func TestDeref(t *testing.T) {
	x := "x"
	if got := Deref(&x, "d"); got != "x" {
		t.Errorf("Deref(&x) = %q, want x", got)
	}
	if got := Deref[string](nil, "d"); got != "d" {
		t.Errorf("Deref(nil) = %q, want d", got)
	}
}
