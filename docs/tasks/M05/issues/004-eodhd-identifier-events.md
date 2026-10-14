---
title: EODHD identifier events
type: task
status: unreviewed
dependencies: [002]
---

## Scope

EODHD serves symbol changes by date range for the US composite. Its answer covers the
domain of every operating MIC in the composite over the range, one row per MIC. The
provider facts are in [001](001-open-m05.md).

In:

- The client for the symbol change history endpoint.
- Conversion of each answer to events and domain coverage. A value that the answer does
  not mention gains its negative assertion through the domain row.
- The period of each fetch, as
  [001](../adr/001-a-domain-fetch-covers-from-the-last-domain-fetch-to-the-present.md)
  states. The endpoint's first date is 2022-07-22, and the lag defaults to two weeks.
- A refusal because the plan lacks the endpoint blocks the datasource for this fetch
  kind.
- Tests against recorded traffic redacted as it is saved. The recording settles the
  unverified facts in [001](001-open-m05.md).
- End to end coverage through vcrproxy of a range that holds a symbol change.
