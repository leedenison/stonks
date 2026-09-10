// Package allowlist matches an email address against glob patterns.
//
// A pattern is a glob over the whole address, such as "*@example.com", in
// which * stands for any run of characters and ? for any one character. An
// address has no separator structure, so * spans all of it, and every other
// character stands for itself. Matching is case-insensitive, and an empty list
// matches nothing.
package allowlist

import "strings"

// List is a set of lowercase glob patterns.
type List []string

// Parse parses a comma-separated list of patterns. Whitespace around a
// pattern is dropped, as is an empty entry, so an empty spec is an empty list.
func Parse(spec string) List {
	var l List
	for _, p := range strings.Split(spec, ",") {
		if p = strings.ToLower(strings.TrimSpace(p)); p != "" {
			l = append(l, p)
		}
	}
	return l
}

// Match reports whether email matches any pattern.
func (l List) Match(email string) bool {
	s := []rune(strings.ToLower(email))
	for _, p := range l {
		if match([]rune(p), s) {
			return true
		}
	}
	return false
}

// match reports whether s matches the glob p. On a mismatch it backtracks to
// the last *, giving it one more character, so a pattern holding several of
// them still matches everything it should.
func match(p, s []rune) bool {
	// star is where the last * of p sits, and mark how far into s it had
	// reached when it was taken.
	star, mark := -1, 0
	i, j := 0, 0
	for i < len(s) {
		switch {
		case j < len(p) && (p[j] == '?' || p[j] == s[i]):
			i, j = i+1, j+1
		case j < len(p) && p[j] == '*':
			star, mark = j, i
			j++
		case star < 0:
			return false
		default:
			mark++
			i, j = mark, star+1
		}
	}
	for j < len(p) && p[j] == '*' {
		j++
	}
	return j == len(p)
}
