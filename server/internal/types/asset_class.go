package types

import (
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
func (e *AssetClass) Scan(src any) error { return scan(e, src) }

// Valid reports whether e is one of the enum's values.
func (e AssetClass) Valid() bool { return slices.Contains(AssetClasses, e) }

// parents is the asset class tree, the root mapping to "".
var parents = map[AssetClass]AssetClass{
	AssetClassUnknown:     "",
	AssetClassCash:        AssetClassUnknown,
	AssetClassSecurity:    AssetClassUnknown,
	AssetClassEquity:      AssetClassSecurity,
	AssetClassStock:       AssetClassEquity,
	AssetClassEtf:         AssetClassEquity,
	AssetClassMutualFund:  AssetClassEquity,
	AssetClassFixedIncome: AssetClassSecurity,
	AssetClassDerivative:  AssetClassSecurity,
	AssetClassOption:      AssetClassDerivative,
	AssetClassFuture:      AssetClassDerivative,
}

// Parent returns e's parent in asset_class_tree, "" for the root. A test
// holds the tree equal to the table.
func (e AssetClass) Parent() AssetClass { return parents[e] }

// under reports whether e is o or lies below it.
func (e AssetClass) under(o AssetClass) bool {
	for c := e; c != ""; c = c.Parent() {
		if c == o {
			return true
		}
	}
	return false
}

// Disjoint reports whether no instrument can be of both e and o. An unknown
// class overlaps every class.
func (e AssetClass) Disjoint(o AssetClass) bool { return !e.under(o) && !o.under(e) }

// Restated reports whether corporate actions restate a quantity of e. They
// restate equity and the classes below it. Every other class, unknown among
// them, keeps its quantity as stated.
func (e AssetClass) Restated() bool { return e.under(AssetClassEquity) }
