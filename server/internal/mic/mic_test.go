package mic

import "testing"

func TestOperating(t *testing.T) {
	tbl := Table{"XNAS": "XNAS", "XNGS": "XNAS"}
	tests := []struct {
		name string
		mic  string
		want string
		ok   bool
	}{
		{name: "operating", mic: "XNAS", want: "XNAS", ok: true},
		{name: "segment", mic: "XNGS", want: "XNAS", ok: true},
		{name: "case and space", mic: " xngs ", want: "XNAS", ok: true},
		{name: "unknown", mic: "ZZZZ"},
		{name: "empty", mic: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := tbl.Operating(tc.mic)
			if got != tc.want || ok != tc.ok {
				t.Errorf("Operating(%q) = %q, %v, want %q, %v", tc.mic, got, ok, tc.want, tc.ok)
			}
		})
	}
}
