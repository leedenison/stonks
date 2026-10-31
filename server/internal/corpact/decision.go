package corpact

import "github.com/leedenison/stonks/server/internal/db/gen"

// Restates reports whether a decision of type d restates the quantity held.
func Restates(d gen.CorpActDecision) bool {
	switch d {
	case gen.CorpActDecisionSplit, gen.CorpActDecisionReverseSplit, gen.CorpActDecisionStockDividend:
		return true
	}
	return false
}

// Restates reports whether e restates the quantity held. A decision
// overrules the system's judgment.
func (e Act) Restates() bool {
	if e.Decision != "" {
		return Restates(e.Decision)
	}
	return e.Handled
}

// Unknown reports whether the effect of e on the quantity held is unknown:
// the system did not handle it and it is undecided, or it is decided as a
// type the system cannot yet apply. The calendar leaves the ex-date of an
// unknown action out of its coverage.
func (e Act) Unknown() bool {
	return (e.Decision == "" && !e.Handled) || e.Decision == gen.CorpActDecisionOther
}
