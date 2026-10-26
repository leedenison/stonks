---
title: Identifier events
recorded: 2026-09-14
---

# Identifier events

Identifier events served by date range for a whole domain, assertions made by price and
corporate event fetches, and the unwinding of data fetched under a ticker that moves.

## Why

An entity provider reports only the chain of the entity a ticker names today. A retired
or reused ticker outside that chain stays invisible, and validity before the first
assertion stays provisional. A range provider reports every rename in a domain, so its
coverage confirms a present assertion back to the last rename without an attestation.

A price or a corporate event fetched under a ticker rests on the assumption that the
ticker named the same instrument on the row's date as at the fetch. When an event shows
the assumption false, the row is wrong until it is refetched.

## Model

### Assertions

A price or corporate event fetch whose answer echoes the identifier it served asserts
that the identifier names the instrument on the day of the answer.

### Domain coverage

Where a provider serves changes by date range, it covers the domain over the range.
Where the answer does not mention a ticker, the ticker gains a negative assertion through
the domain row and is never written individually.

An integration maps the provider's spelling of a symbol to the system's before it records
an event. A missed match inside domain coverage turns a known event into a claim that
none occurred.

### Unwinding

An event on a ticker finds every fetch key that sent the ticker to a provider. A data row
produced by such a fetch key is invalidated when the event lies between the row's date
and the fetch's assertion moment. Invalidated rows and the coverage written from their
fetch keys are dropped and refetched.

## Constraints

### Explicit Fetches

A range provider is fetched before a batch is resolved, once per domain and period,
covering the earliest transaction date to the present. Where the fetch fails, resolution
proceeds on provisional validity.

### Domain fetches

The period of a domain fetch follows the rule on `NextPeriod` in
[identifier_events.go](../../server/internal/market/identifier_events.go). A range
provider implements `market.Ranged`, and `datasources.lag_days` overrides its lag.

The lag makes each fetch repeat the most recent part of the previous period. An event
that a provider publishes late is still recorded, and it unwinds what rested on the
claim that no event occurred. Coverage that ended a lag before the present would leave
every recent transaction outside coverage.

An administrator starts a domain fetch from the admin area. The datasources page shows
the lag of each datasource and lets an administrator change it.

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
  identifier events kind.
- A refusal of a `from` before the first date fails only the request.
- Tests run against recorded traffic.
- End to end coverage runs through a range that holds a rename.

## Undecided

- How often domain coverage is extended to the present.
