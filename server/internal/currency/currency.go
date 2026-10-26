// Package currency maps a currency code to its family: the codes that denote
// one currency at different unit scales, such as GBP and GBX. A listing is
// keyed by its family.
package currency

import (
	"context"
	"fmt"

	"github.com/shopspring/decimal"

	"github.com/leedenison/stonks/server/internal/db/gen"
)

// Queries is the database access Load needs.
type Queries interface {
	ListCurrencies(ctx context.Context) ([]gen.Currency, error)
}

// Families maps each currency code the system has to its family code.
type Families map[string]string

// Scales maps each currency code the system has to the number of family
// units in one of its units: 0.01 for GBX.
type Scales map[string]decimal.Decimal

// Load reads the currency reference table.
func Load(ctx context.Context, q Queries) (Families, Scales, error) {
	rows, err := q.ListCurrencies(ctx)
	if err != nil {
		return nil, nil, fmt.Errorf("list currencies: %w", err)
	}
	f := make(Families, len(rows))
	s := make(Scales, len(rows))
	for _, r := range rows {
		f[r.Code], s[r.Code] = r.Family, r.Scale
	}
	return f, s, nil
}

// Scale returns the scale of code, one where the system has no such
// currency.
func (s Scales) Scale(code string) decimal.Decimal {
	if v, ok := s[code]; ok {
		return v
	}
	return decimal.NewFromInt(1)
}

// Family returns the family of code. It returns the empty family when the
// system has no such currency.
func (f Families) Family(code string) string { return f[code] }

// Known reports whether the system has the currency code.
func (f Families) Known(code string) bool {
	_, ok := f[code]
	return ok
}
