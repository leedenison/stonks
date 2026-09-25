package openfigi

import (
	"testing"

	"github.com/leedenison/stonks/server/internal/mic"
)

// mics is a MIC table holding the venues the tests name.
var mics = mic.Table{
	"XNYS": "XNYS", "ARCX": "XNYS", "XNAS": "XNAS", "XNGS": "XNAS",
	"XLON": "XLON", "XTAI": "XTAI", "ROCO": "ROCO",
}

func TestVenue(t *testing.T) {
	tests := []struct {
		code   string
		want   string
		wantOK bool
	}{
		{code: "UN", want: "XNYS", wantOK: true},
		{code: "UW", want: "XNAS", wantOK: true},
		{code: "UP", want: "XNYS", wantOK: true},
		{code: "LN", want: "XLON", wantOK: true},
		{code: "US"},
		{code: "TT"},
		{code: "GY"},
		{code: ""},
	}
	for _, tc := range tests {
		got, ok := venue(tc.code, mics)
		if got != tc.want || ok != tc.wantOK {
			t.Errorf("venue(%q) = %q, %v, want %q, %v", tc.code, got, ok, tc.want, tc.wantOK)
		}
	}
}
