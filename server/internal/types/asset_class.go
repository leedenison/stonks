package types

import (
	"fmt"
	"slices"
)

// AssetClass is the asset_class enum. A test holds it equal to the database.
type AssetClass string

// AssetClasses are the values the enum names, in the order the database
// declares them.
var AssetClasses = []AssetClass{
	AssetClassUnknown,
	AssetClassCash,
	AssetClassSecurity,
	AssetClassEquity,
	AssetClassStock,
	AssetClassEtf,
	AssetClassMutualFund,
	AssetClassFixedIncome,
	AssetClassDerivative,
	AssetClassOption,
	AssetClassFuture,
}

// The asset classes, spelled as the asset_class enum spells them.
const (
	AssetClassUnknown     AssetClass = "unknown"
	AssetClassCash        AssetClass = "cash"
	AssetClassSecurity    AssetClass = "security"
	AssetClassEquity      AssetClass = "equity"
	AssetClassStock       AssetClass = "stock"
	AssetClassEtf         AssetClass = "etf"
	AssetClassMutualFund  AssetClass = "mutual_fund"
	AssetClassFixedIncome AssetClass = "fixed_income"
	AssetClassDerivative  AssetClass = "derivative"
	AssetClassOption      AssetClass = "option"
	AssetClassFuture      AssetClass = "future"
)

// Scan reads the enum from a text or bytea column.
func (e *AssetClass) Scan(src any) error {
	switch s := src.(type) {
	case []byte:
		*e = AssetClass(s)
	case string:
		*e = AssetClass(s)
	default:
		return fmt.Errorf("unsupported scan type for AssetClass: %T", src)
	}
	return nil
}

// Valid reports whether e is one of the enum's values.
func (e AssetClass) Valid() bool { return slices.Contains(AssetClasses, e) }
