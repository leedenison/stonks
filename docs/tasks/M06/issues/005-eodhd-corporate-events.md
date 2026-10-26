---
title: EODHD corporate events
type: task
dependencies: [003]
---

## Scope

EODHD serves splits through `GET /api/splits/{SYMBOL.EXCHANGE}` with `from` and `to`.
Each corporate event states a date and a ratio. It covers LSE, which Massive does not.

## Open questions

Settle each by recording before the plan is written.

- Which plan the account holds, and how far back that plan answers.
- How an LSE ticker is written in the request.
- Which errors are temporary, and how the answer for an unknown ticker reads.

## Design

- The ratio is a string stated new over old. A ten for one split reads
  `"10.000000/1.000000"`.
- An answer applies only when it names the ticker the request asked for.
- EODHD sends only a ratio, so the ratio test alone decides which corporate events apply.
- A recorded request must replay on any later day, even when its query states the day of
  the recording. The recording proxy and `testutil/vcr` match a request on its method, URL
  and body. Each gains a match that normalises the dates in the query. Their package
  comments describe it.
