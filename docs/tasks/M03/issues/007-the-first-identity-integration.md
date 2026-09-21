---
title: The first identity integration
type: task
dependencies: [004, 006]
---

## Scope

One integration answering what a source states about an instrument: the identifiers,
asset class, currency and venue of each listing a stated identifier names.

In:

- The client for the provider, tested against recorded traffic redacted as it is saved.
- The declaration of the identifier types and domains it serves, and the identifier it
  sends for each.
- Conversion of each answer to the canonical candidate shape: the identifiers named, the
  asset class, the currency, the venue normalised to its operating MIC, and what the
  call strictly filtered on. Candidates are not ranked.
- The classification of the provider's error codes as temporary or permanent, and its
  rate limit.
- Configuration that enables the integration and carries its credential.

Out:

- Prices, corporate events and identifier events, whatever the provider could serve.
- Choosing among candidates; issue [008](008-resolution-against-datasources.md).
