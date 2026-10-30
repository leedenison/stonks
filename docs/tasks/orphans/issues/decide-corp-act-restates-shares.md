---
title: Decide whether a corporate action restates shares
type: task
status: unreviewed
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

The work is an admin UI. An administrator uses it to decide whether an action restates
shares. It has two surfaces.

* A corporate actions page. It lists the actions of an instrument with the ex-date,
  ratio, datasource, stated type and decision of each. An administrator can change any
  decision there, including one the system made.
* A quick decision on one unhandled action, which an administrator reaches from its
  finding. D-ALERTS lists these findings.

An administrator's decision overrules both the datasource and the ratio test, in either
direction.

The decision shows the evidence beside the action:

* the type a datasource states, if any
* a split that a statement states near the ex-date
* another datasource's answer for a ticker of the same instrument
* the price series around the ex-date

A later change may infer a proposed decision from this evidence. The administrator still
makes the decision.

## Open questions

* Whether an administrator records the decision alone or a type that implies it. A type
  such as spin-off keeps what D-CORP needs to apply the action in full.
* What keys a decision, so that it survives a refetch that rewrites the answer: the
  ticker, ex-date and ratio, or the action row.
* Whether a decision clears the finding on the unhandled action.
* How a changed decision reaches cached adjusted values and provisional holdings.

## Design

Nothing settled.
