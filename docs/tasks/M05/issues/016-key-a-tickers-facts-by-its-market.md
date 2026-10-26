---
title: Key a ticker's facts by its market
type: task
---

Key identifier assertions, events, coverage and attestations by market rather than by
operating MIC; see [021](../adr/021-a-tickers-facts-are-keyed-by-its-market.md).

## Scope

- A market for each operating MIC, as reference data: the US venues Massive lists form
  one, and every other operating MIC is its own.
- A market ticker type in Go and in the API, separate from a MIC_TICKER.
- The three fact tables and attestations keyed by market, and the validity reads and
  writes mapping a ticker's operating MIC to it.
- Massive stating each fact once for the US market, and refusing to build where its
  venues do not cover one market whole.
- Extending validity matching a held ticker to a listing at any venue of its market.
- The same-day ticker join comparing tickers at the market.
- The coverage reader, attestation and the Tickers tab showing one history per market.

Out:

- Anything that changes what a stated key, an identifier row or a listing carries.
- OTC venues joining the US market.
