---
title: Check stated splits against the calendar
type: task
dependencies: [003]
---

## Scope

The corporate event fetch run compares each stated split it now covers with the
calendar. A stated split that lacks a matching corporate event raises an
`unmatched_split` finding. The finding references the stated key, so the
`findings` check on `stated_key_id` admits the new kind. See [005](../adr/005-a-stated-split-only-raises-a-finding.md).
