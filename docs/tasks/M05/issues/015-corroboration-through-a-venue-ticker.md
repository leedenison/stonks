---
title: Corroboration through a venue ticker on one day
type: task
---

## Scope

Implement [020](../adr/020-a-venue-ticker-joins-two-answers-made-on-one-day.md).

In scope:

- The corroboration test in `rejects` and `corroborates` in
  [choose.go](../../../../server/internal/resolve/choose.go), and the finding detail,
  which states both tests.
- Unit tests for the join: two answers on one day sharing a venue ticker attach, two on
  different days do not, a claim does not join the database's instrument through a ticker,
  and the precedence step drops a contradicting claim.
- The comments in `choose.go` and the `resolve` package comment, written up from the ADR.
- The expected results in [README.md](../../../../local/test/ingest/standard/README.md).
  Case 01 attaches EODHD's answer and writes PepsiCo's ISIN and CUSIP. Case 07 attaches
  EODHD's answer without a corroboration finding. Case 13 matches the instrument from
  the database without an identity fetch, since 01 wrote the ISIN.
- Any e2e fixture or assertion that records the identifiers of an instrument a venue ticker
  first sighted.

Out of scope:

- Writing the losing claim's assertion where two answers disagree on a day. ADR 002 covers
  assertions that validity reads. A dropped identity claim leaves validity without an
  assertion.
- A second fetch under the winner's identifiers.
