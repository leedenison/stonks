---
title: EODHD identifier events
type: task
status: unreviewed
dependencies: [002]
---

## Scope

EODHD serves symbol changes by date range for the US composite. Its answer covers the
domain of every operating MIC in the composite over the range, one row per MIC.

In:

- The client for EODHD's symbol change endpoint, tested against recorded traffic
  redacted as it is saved.
- Conversion of each answer to events and domain coverage. A value that the answer does
  not mention gains its negative assertion through the domain row.
- The period of each fetch, as [001](../adr/001-a-domain-fetch-covers-from-the-last-domain-fetch-to-the-present.md)
  states.
- End to end coverage through vcrproxy of a range that holds a symbol change.
