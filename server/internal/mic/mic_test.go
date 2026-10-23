package mic

import (
	"slices"
	"testing"
)

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

func TestVenues(t *testing.T) {
	tbl := Table{"XNAS": "XNAS", "XNGS": "XNAS", "XNCM": "XNAS", "XLON": "XLON"}
	if got, want := tbl.Venues("XNAS"), []string{"XNAS", "XNCM", "XNGS"}; !slices.Equal(got, want) {
		t.Errorf("Venues(XNAS) = %v, want %v", got, want)
	}
	if got := tbl.Venues("XNGS"); len(got) != 0 {
		t.Errorf("Venues(XNGS) = %v, want none, since a segment is no operating MIC", got)
	}
}
