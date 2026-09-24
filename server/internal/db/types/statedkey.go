// Package types holds the Go types of columns sqlc does not map itself,
// named in the overrides in sqlc.yaml. It imports nothing of the server, so
// generated code may depend on it without a cycle.
package types

// StatedIdentifier is one identifier a source stated, held in the identifiers
// column of a stated key. The column is compared whole by the unique index, so
// a writer sorts the array by type, domain and value, and an absent domain is
// empty and omitted rather than written as null. A key stating no identifiers
// holds an empty slice, since nil encodes as null and the column refuses it.
type StatedIdentifier struct {
	Type   string `json:"type"`
	Domain string `json:"domain,omitempty"`
	Value  string `json:"value"`
}
