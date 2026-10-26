package types

import "github.com/shopspring/decimal"

// Ratio is how a corporate event restates a quantity: From units before it
// become To units after it.
type Ratio struct {
	From, To decimal.Decimal
}

// Equal reports whether r and o restate a quantity alike, as 2 for 1 and 10
// for 5 do.
func (r Ratio) Equal(o Ratio) bool {
	return r.From.Mul(o.To).Equal(r.To.Mul(o.From))
}
