// Package page cuts a listing to one page. A page is read with one row more
// than it holds, so a full page is known to have a successor.
package page

import (
	"fmt"

	"github.com/google/uuid"
)

// DefaultSize is the size of a page whose request names none.
const DefaultSize = 50

// Page is the size of one page of a listing.
type Page struct {
	Size int
}

// New returns a page of size rows; 0 means DefaultSize.
func New(size int32) Page {
	p := Page{Size: int(size)}
	if p.Size == 0 {
		p.Size = DefaultSize
	}
	return p
}

// Lim is one more row than the page holds.
func (p Page) Lim() int32 { return int32(p.Size + 1) }

// Cut trims rows to the page and reports whether a further page follows.
func Cut[T any](p Page, rows []T) ([]T, bool) {
	if len(rows) <= p.Size {
		return rows, false
	}
	return rows[:p.Size], true
}

// Before reads a page token that is the id of the last row served; "" is the
// first page.
func Before(token string) (*uuid.UUID, error) {
	if token == "" {
		return nil, nil
	}
	id, err := uuid.Parse(token)
	if err != nil {
		return nil, fmt.Errorf("page token %q: %w", token, err)
	}
	return &id, nil
}
