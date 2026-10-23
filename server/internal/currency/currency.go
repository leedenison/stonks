// Package currency maps a currency code to its family: the codes that denote
// one currency at different unit scales, such as GBP and GBX. A listing is
// keyed by its family.
package currency

import (
	"context"
	"fmt"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Queries is the database access Load needs.
type Queries interface {
	ListCurrencies(ctx context.Context) ([]gen.Currency, error)
}

// Families maps each currency code the system has to its family code.
type Families map[string]string

// Load reads the currency reference table.
func Load(ctx context.Context, q Queries) (Families, error) {
	rows, err := q.ListCurrencies(ctx)
	if err != nil {
		return nil, fmt.Errorf("list currencies: %w", err)
	}
	f := make(Families, len(rows))
	for _, r := range rows {
		f[r.Code] = r.Family
	}
	return f, nil
}

// Family returns the family of code. It returns the empty family when the
// system has no such currency.
func (f Families) Family(code string) string { return f[code] }

// Known reports whether the system has the currency code.
func (f Families) Known(code string) bool {
	_, ok := f[code]
	return ok
}
