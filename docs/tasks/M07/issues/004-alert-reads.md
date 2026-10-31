---
title: Alert reads
type: task
dependencies: [003]
---

## Scope

The server side of the alerts page: the reads that derive each alert kind and the API
that serves the digest and a kind's list. See
[001](../adr/001-an-alert-is-derived-and-clears-with-its-cause.md),
[002](../adr/002-a-finding-alert-is-cleared-not-dismissed.md) and
[003](../adr/003-alerts-rank-by-kind-then-by-what-rests-on-them.md).

The kinds and their subjects:

* A failed or interrupted run: the run.
* An open block: the block.
* A ticker that names nothing: the market ticker, with the keys that state it, their
  transactions, and the earliest transaction day.
* A ticker validity gap: the market ticker, the instrument the ticker names, and the
  `Gap` of `stonks.admin.coverage.v1` with its period, keys and transactions.
* A corporate action: the unhandled corporate action finding, which names the action.
* A finding: an uncleared finding of the kinds ADR 002 lists.

## Design

* A package `stonks.admin.alert.v1` with two RPCs. `GetAlertDigest` returns the count
  of each kind and its top five. `ListAlerts` requires a kind and takes a page size and
  page token, and returns one kind's alerts in rank order with a next page token.
* An `Alert` carries an `AlertKind` and a oneof subject: run, block, ticker that names
  nothing, gap, finding. The kind is finer than the oneof, since every finding-derived
  kind shares the finding subject. Each subject carries its own counts and dates; the
  envelope holds only the kind.
* New queries: uncleared findings by kind across every run; gaps across every ticker
  with transactions, from the derivation the coverage package already applies per
  ticker; tickers with transactions that name nothing on every day. Blocks and runs
  come from their existing list queries with a state filter.
* The service takes a clock, as the coverage service does, because a gap runs up to
  today.
* The order within a kind is transactions, then the subject's date, then a natural key:
  the ticker and market, or the row id alone, whose UUIDv7 order stands for the date.
  The cursor carries the last row's tuple, or that id.

## Tests

* Unit tests of the service with handcrafted rows and a pinned clock prove the kind
  order, the order within a kind over distinct dates, and the cursor.
* Integration tests prove each kind's query and the natural-key tie-break. They date a
  gap and a ticker that names nothing through the statement rows they insert, and they
  insert runs in the failed and interrupted states. Rows a test creates in one
  transaction share a created_at, so the order over created_at is proved in the unit
  tier.
