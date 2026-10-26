---
title: Key a ticker's facts by its market
type: task
status: unreviewed
---

Key identifier assertions, events, coverage and attestations by market rather than by
operating MIC; see [021](../adr/021-a-tickers-facts-are-keyed-by-its-market.md).

## Scope

- A market for each operating MIC: the US venues Massive lists form one, and every other
  operating MIC is its own.
- The three fact tables and attestations keyed by market, and the validity reads and
  writes mapping a ticker's operating MIC to it.
- Massive stating each fact once for the US market.
- The coverage reader, attestation and the Tickers tab showing one history per market.

Out: anything that changes what a stated key, an identifier row or a listing carries.
