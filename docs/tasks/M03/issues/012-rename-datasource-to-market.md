---
title: Rename the datasource package to market
type: task
---

## Scope

The package fetches market data from external providers, and a datasource is one such
provider, so the package is named for what it fetches.

In:

- `server/internal/datasource` becomes `server/internal/market`, and its integrations move
  with it, as `market/openfigi`.
- `FetchRequest` and `FetchResponse` become `Request` and `Response`, since `market.Request`
  and `market.Response` read in full without the prefix.
- Comments that use "market" for a venue or a composite are reworded, so the word keeps
  one sense in the code. For example, the terminology's `Venue -- the market a listing
  trades on` and the openfigi package's composite exchange code that "names a market rather
  than a venue".

Out:

- The datasource noun inside the package: `Entry`, `Registry` and the rest keep their
  names, as do the `datasources` and `datasource_blocks` tables, the admin proto and the
  admin Datasources page.
