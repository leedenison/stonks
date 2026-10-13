package db

import "testing"

func TestLikeEscape(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{`VOD`, `VOD`},
		{`a\b`, `a\\b`},
		{`50%`, `50\%`},
		{`a_b`, `a\_b`},
		{`\%_`, `\\\%\_`},
	}
	for _, tc := range tests {
		if got := LikeEscape(tc.in); got != tc.want {
			t.Errorf("LikeEscape(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
