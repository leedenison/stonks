---
title: Decide whether a corporate action restates shares
type: task
---

## Scope

A corporate action applies to a holding only when it restates the share count. A split,
a reverse split and a stock dividend restate it. A spin-off price factor and a rights
issue leave the share count as it is. The other effects of a spin-off belong to D-CORP.

A datasource answer cannot settle the question alone.

* EODHD states an action as an ex-date and a ratio. Its splits also carry stock
  dividends, spin-off price factors and reorganisations. Every EODHD action is therefore
  unhandled. See `fromSplits` in
  [splits.go](../../../../server/internal/market/eodhd/splits.go).
* Massive types GE's spin-offs of 2023 and 2024 as forward splits of 1281 for 1000 and
  1253 for 1000.
* The ratio test of package corpact reads EODHD's 104 for 100 for GE in 2019 as a 26 for
  25 split. See `restatesShares` in
  [record.go](../../../../server/internal/corpact/record.go).

Every EODHD plan omits the type of an action, as EODHD documented its APIs on 2026-10-30.

* `/api/splits` and the bulk API state a date and a ratio.
* `/api/calendar/splits` states `old_shares` and `new_shares` for each date.
* The fundamentals API states the last split only.
* The ASX corporate actions feed types each action, and it covers ASX listings alone.
  Its types are dividends, splits, bonus issues, rights issues, buybacks, capital returns,
  share purchase plans and other. Spin-off is not one of them.

EODHD staff say that spin-offs stay in the splits data, since adjusting prices needs a
factor.

An administrator records a type for an action, and whether it restates shares follows
from the type. See [006](../adr/006-a-corp-act-decision-is-a-type-on-the-action-row.md)
for what the decision records, what keys it and how it settles the alert, and
[002](../adr/002-a-finding-alert-is-cleared-not-dismissed.md) for why clearing the
finding alone leaves the alert open.

The work has two surfaces.

* A Corporate actions tab on the admin instrument page. It lists the actions of the
  instrument with the ex-date, ratio, datasource, stated type, effective restates-shares
  and decision of each. An administrator can set or change any decision there, including
  one the system made.
* A Decide dialog on one action. The tab opens it, and the alerts page opens it from the
  corporate action alert. See [005](005-alerts-page.md).

The dialog shows the evidence beside the action:

* the type the datasource states, if any
* a split that a statement states near the ex-date
* another datasource's answer for a ticker of the same instrument

A later change may infer a proposed decision from this evidence. The administrator still
makes the decision.

## Design

* A `decision` column on `corp_acts`, nullable, holding the type. The refetch pairing in
  `writeAnswer` carries it over on a paired row.
* An admin proto package `stonks.admin.corpact.v1` with an RPC that lists the actions of
  an instrument and an RPC that records a decision. The decision on the wire is an enum
  whose unspecified value means undecided.
* `Finding` gains `optional string corp_act_id` beside `block_id`, so the alerts page can
  reach the action from its finding. `ClearFinding` refuses the unhandled corporate
  action kind as a failed precondition, as it refuses a block finding.
* The holding calendar reads the effective restates-shares: the decision where one
  exists, otherwise `handled`.

## Tests

* An integration test in `server/internal/corpact` records an answer, decides an action,
  re-records the same answer and reads the decision back. A second re-records a changed
  ratio and asserts the row, its finding and the decision are gone.
* A unit test of the holding calendar covers an action whose decision overrules
  `handled` in each direction.
* The e2e suite decides the unmatched split fixture's action through the tab and reads
  the adjusted holding.
