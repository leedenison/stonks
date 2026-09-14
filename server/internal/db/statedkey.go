package db

// StatedIdentifier is one identifier a source stated, held in the identifiers
// column of a stated key. The column is compared whole by the unique index, so
// a writer sorts the array by type, domain and value, an absent domain sorting
// first, and an absent domain is omitted rather than written as null.
type StatedIdentifier struct {
	Type   string  `json:"type"`
	Domain *string `json:"domain,omitempty"`
	Value  string  `json:"value"`
}
