---
title: Infer the types of EODHD corporate actions
type: task
status: unreviewed
---

## Scope

EODHD states a corporate action as an ex-date and a ratio, and omits the type. Its
splits also carry stock dividends, spin-off price factors and reorganisations. Every
EODHD action is therefore unhandled, and a holding in a market whose corporate actions
source is EODHD stays provisional. See `fromSplits` in
[splits.go](../../../../server/internal/market/eodhd/splits.go).

The work infers a type for an EODHD action, so that a split restates a holding. The ratio
test of package corpact is not enough on its own. EODHD states GE's spin-off of 2019 as
104 for 100, and the test reads it as a 26 for 25 split. See `restatesShares` in
[record.go](../../../../server/internal/corpact/record.go).

## Open questions

* Which evidence can type an action: a split that a statement states, another
  datasource's answer for a ticker of the same instrument, or a price series around the
  ex-date.
* Whether an inferred type is stored beside the action or derived when it is read.
* How an administrator sees why an action was typed.

## Design

Nothing settled.
