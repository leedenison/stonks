---
title: Identifier events
recorded: 2026-09-14
---

# Identifier events

Identifier events served by date range for a whole market, assertions made by price and
corporate action fetches, and the unwinding of data fetched under a ticker that moves.

## Why

An entity provider reports only the chain of the entity a ticker names today. A retired or
reused ticker outside that chain stays invisible, and ticker validity before the first
assertion stays provisional. A range provider reports every rename in a market, so its
assertion coverage confirms a present assertion back to the last rename without an
identifier attestation. Each market names one datasource as its identifier event source.
A range provider counts only in a market where it is the identifier event source.
Elsewhere its edges and coverage are stored and ignored.

A price or a corporate action fetched under a ticker rests on the assumption that the
ticker named the same instrument on the row's date as at the fetch. When an edge shows
the assumption false, the row is wrong until it is refetched.

## Model

### Assertions

A price or corporate action fetch whose answer echoes the identifier it served asserts
that the identifier names the instrument on the day of the answer.

### Market assertion coverage

Where a provider serves changes by date range, the integration writes assertion coverage
over the market for the range. Where the answer does not mention a ticker, the ticker
gains a negative assertion through the market row and is never written individually.

A range provider states a rename without a stable identifier. The integration records
such a rename as two edge-only assertions, one for each symbol, at the start of the day of
the change. An edge-only assertion holds an empty period whose bound is an edge. It holds
no day and states nothing about the instrument. It only cuts the timeline. The check on
`identifier_assertions` then allows `asserted_from = asserted_before`.

An integration maps the provider's spelling of a symbol to the system's before it records
a change. A missed match inside a period of assertion coverage turns a known change into a
claim that none occurred.

### Unwinding

When the identifier event source records an edge on a ticker, the system finds every
fetch key that sent the ticker to a provider. The system invalidates a data row of such a
fetch key when the edge lies between the row's date and the fetch's assertion moment. The
system drops the invalidated rows and the assertion coverage of their fetch keys. Then it
refetches them.

## Constraints

### Explicit Fetches

A range provider is fetched before a batch is resolved, once per market and period,
covering the earliest transaction date to the present. Where the fetch fails, resolution
proceeds on provisional ticker validity.

### Market fetches

A market fetch starts a lag before the end of the previous fetch. The first fetch starts
on the earliest day needed. A fetch never starts before the provider's window. It ends
tomorrow. The provider states a default lag. Its datasource configuration overrides the
default.

The lag makes each fetch repeat the most recent part of the previous period. A provider
may publish a change late. The next fetch still records it. The recording unwinds what
rested on the claim that the ticker did not change. A period of assertion coverage that
ended a lag before the present would leave every recent transaction outside it.

An administrator starts a market fetch from the admin area. The datasources page shows
the lag of each datasource and lets an administrator change it.

### Market membership

The OTC operating MICs join the US market once the Massive integration lists the OTC
venues and reads its `otc` market. Until then Massive does not cover them, and a market
is covered whole or not at all.

A national code elsewhere becomes a market by a row in `markets`. Japan is one
example: a single committee assigns the securities codes of the Tokyo, Osaka, Nagoya,
Fukuoka and Sapporo exchanges.

## Sketch

EODHD serves symbol changes by date range for the US composite. Its range endpoint is
`GET /api/symbol-change-history?from=&to=&fmt=json`:

- One request costs one call. `fmt` defaults to CSV.
- Each row states `exchange`, `old_symbol`, `new_symbol`, `company_name` and
  `effective`. The rows are renames only, and `exchange` is always `US`.
- Data starts on 2022-07-22.
- Only the All World Extended and All-In-One plans include it.
- The documentation says the data is updated daily. A support reply on EODHD's forum
  says a change usually appears within one to two weeks. The default lag of two weeks
  rests on that reply.
- `GET /api/exchange-symbol-list/US?delisted=1` lists delisted symbols without dates.
- Unverified: the share class spelling in `old_symbol` and `new_symbol`, the status a
  plan without the endpoint receives, and whether a `from` before 2022-07-22 is
  refused. A plan without the endpoint can record the second.

The integration is tested as follows:

- A refusal because the plan lacks the endpoint blocks the datasource for the
  identifier facts kind.
- A refusal of a `from` before the first date fails only the request.
- Tests run against recorded traffic.
- End to end coverage runs through a range that holds a rename.

## Undecided

- How often a market's assertion coverage is extended to the present.
