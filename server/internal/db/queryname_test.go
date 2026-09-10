package db

import "testing"

func TestQueryName(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want string
	}{
		{
			name: "sqlc query",
			sql:  "-- name: GetUser :one\nSELECT id FROM users WHERE id = $1\n",
			want: "GetUser",
		},
		{
			name: "sqlc query behind a blank line",
			sql:  "\n-- name: BindGoogleSubject :one\nUPDATE users SET google_subject = $2\n",
			want: "BindGoogleSubject",
		},
		{name: "no comment", sql: "SELECT 1", want: "SELECT"},
		{name: "pgx internal statement", sql: "begin", want: "begin"},
		{name: "empty", sql: "", want: ""},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := queryName(tc.sql); got != tc.want {
				t.Errorf("queryName(%q) = %q, want %q", tc.sql, got, tc.want)
			}
		})
	}
}
