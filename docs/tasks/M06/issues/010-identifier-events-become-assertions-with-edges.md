---
title: Identifier events become assertions with edges
type: task
dependencies: [009]
---

## Scope

An identifier event is recorded as assertions with an edge, and a stronger source
overrules a weaker source's edge. See
[009](../adr/009-an-identifier-event-is-recorded-as-assertions-with-edges.md) and
[010](../adr/010-a-stronger-source-overrules-an-edge.md).

In:

- The nullable `edge` column on `identifier_assertions`.
- `chainFacts` in the Massive integration writes the assertions of a change in place of events.
- `identifier_events` is dropped. `identifier_event_coverage` becomes
  `identifier_period_assertions`.
- `Derive` cuts the timeline at edges and applies the overrule rule.
- The `overruled_edge` finding kind, in SQL, in `FindingKind` in `run.proto` and in the
  client labels.
- The vocabulary moves to identifier facts: the fetch kind `identifier_facts`, the
  integration interface and its result type in `market`, and `tickervalidity.Event` and
  `Facts.Events`.
- Proto:
  - `AssertionItem` gains `optional Edge edge`, with `EDGE_FIRST` and `EDGE_LAST`.
  - `IdentifierEvent`, `IdentifierEventAction` and `Ticker.events` are deleted, and the
    fields of `Ticker` are renumbered.
  - `Coverage` becomes `PeriodAssertion`.
- The admin tickers tab draws edge assertions where it draws events.
- Comments: the `tickervalidity` package comment, the `BrokenBracket` doc, the
  `broken_ticker_validity_bracket` comment in `006_findings.sql`, and the identifier
  events text in `resolve.go` and `market.go`.

Out:

- The edge-only assertion of a range provider, which waits for D-IDENT.

## Tests

- `TestChainFacts` cases: a rename gives `last` on D-1 and `first` on D, and an
  assignment gives `first`.
- `derive_test.go` cases:
  - a `first` cuts at the start of its day, and a `last` at the start of the next;
  - a bracket and a period assertion each overrule a weaker edge;
  - an identifier attestation overrules an edge;
  - an edge across an equal bracket cuts and raises `broken_ticker_validity_bracket`;
  - an overruled edge raises `overruled_edge`.
- The Massive dbtest asserts the edge rows a ticker history writes.
- `timeline.test.tsx` and `page.test.tsx` draw edge assertions.
- The `ticker-change` e2e passes.
