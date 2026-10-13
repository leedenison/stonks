package db

import "strings"

var likeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

// LikeEscape escapes the characters LIKE treats as patterns, so a query
// matches them as text.
func LikeEscape(s string) string { return likeEscaper.Replace(s) }
