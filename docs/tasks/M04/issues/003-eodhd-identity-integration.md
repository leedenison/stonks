---
title: EODHD identity integration
type: task
status: unreviewed
---

## Scope

An integration that answers from EODHD what a source states about an instrument: the
identifiers, asset class, currency and venue of each listing a stated identifier names.

In:

- The client for EODHD, tested against recorded traffic redacted as it is saved.
- The declaration of the identifier types and domains it serves, and the identifier it
  sends for each.
- Answering only for the venues and asset classes EODHD covers. What EODHD covers,
  and how the integration is kept to it, are open questions of [001](001-open-m04.md).
- The mapping between EODHD's exchange codes and operating MICs, in both directions.
- Conversion of each answer to the canonical candidate shape. A venue is returned as the
  domain of a MIC_TICKER, normalised to its operating MIC. Every identifier EODHD gave a
  candidate is returned, and candidates are not ranked.
- The classification of EODHD's errors as temporary or permanent, and its rate limit.
- A datasources row and the credential it carries, so an administrator can enable the
  integration from the admin area.
- End to end coverage through vcrproxy of a key that OpenFIGI and EODHD both answer.

Out:

- Prices, corporate events and identifier events, whatever EODHD could serve.
