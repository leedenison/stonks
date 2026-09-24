---
title: The first identity integration
type: task
dependencies: [006]
---

## Scope

One integration answering what a source states about an instrument: the identifiers,
asset class, currency and venue of each listing a stated identifier names.

In:

- The client for the provider, tested against recorded traffic redacted as it is saved.
- The declaration of the identifier types and domains it serves, and the identifier it
  sends for each.
- Conversion of each answer to the canonical candidate shape: the identifiers returned,
  the asset class, the currency, and what the call strictly filtered on. A venue is
  returned as the domain of a MIC_TICKER, normalised to its operating MIC. Candidates
  are not ranked.
- Returning every identifier the provider gave a candidate, the instrument-grain ones
  included. Whether two candidates are listings of one instrument or competing answers
  is read from the identifiers they share, so an integration that drops them makes an
  exact answer at instrument grain look like a choice.
- The classification of the provider's error codes as temporary or permanent, and its
  rate limit.
- Configuration that enables the integration and carries its credential.

Out:

- Prices, corporate events and identifier events, whatever the provider could serve.
- Choosing among candidates; issue [008](008-resolution-against-datasources.md).
